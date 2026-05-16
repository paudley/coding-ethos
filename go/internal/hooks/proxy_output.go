// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	defaultHookOutputMaxTokens   = 2000
	defaultHookOutputHeadTokens  = 900
	defaultHookOutputTailTokens  = 900
	defaultHookOutputMaxLines    = 80
	defaultHookOutputHeadLines   = 32
	defaultHookOutputTailLines   = 32
	defaultHookOutputDiagnostics = 12
	defaultAnatomyMapSymbols     = 6
	defaultAnatomyMapTimeout     = 5 * time.Second
	proxyDecisionAllow           = "allow"
	proxyDecisionInject          = "inject"
	proxyDecisionSummarize       = "summarize"
	proxyDecisionTruncate        = "truncate"
	proxyPolicyDiagnosticSummary = "proxy.diagnostic_summary"
	proxyPolicyDirectoryAnatomy  = "proxy.directory_anatomy"
	proxyPolicyTokenBudget       = "proxy.token_budget"
)

type proxiedToolOutput struct {
	Text    string
	Records []agentproxy.TransformRecord
	Events  []agentproxy.ProviderEvent
}

func proxyPostToolOutput(event Event, output string) proxiedToolOutput {
	normalizer := hookOutputNormalizer(event.Cwd)
	normalized := normalizer.preserveLines(output)
	proxied := compressToolOutputWithRecords(event, normalized)
	proxied = enrichDirectoryListingToolOutput(event, proxied)
	proxied.Events = proxyToolOutputEvents(event, normalized, proxied)

	return proxied
}

func enrichDirectoryListingToolOutput(
	event Event,
	proxied proxiedToolOutput,
) proxiedToolOutput {
	if event.ReturnCode() != 0 {
		return proxied
	}

	invocation, listingDetected := directoryListingInvocation(event.Command())
	if !listingDetected {
		return proxied
	}

	root := gitRootFromPath(event.Cwd)
	if root == "" {
		return proxied
	}

	targetPath, ok := repoRelativeListingPath(root, event.Cwd, invocation.Path)
	if !ok {
		return proxied
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		defaultAnatomyMapTimeout,
	)
	defer cancel()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return proxied
	}
	defer store.Close()

	indexer := codeintel.NewASTIndexer(store)

	indexErr := indexListingTarget(ctx, indexer, root, targetPath, invocation)
	if indexErr != nil {
		return proxied
	}

	output, err := store.EnrichDirectoryListing(
		ctx,
		codeintel.DirectoryAnatomyQuery{
			Path:           targetPath,
			Root:           root,
			SymbolsPerFile: defaultAnatomyMapSymbols,
			IncludeNested:  invocation.Recursive,
			MaxDepth:       invocation.MaxDepth,
		},
		proxied.Text,
	)
	if err != nil {
		return proxied
	}

	proxied.Text = output.Text
	if output.Record.Name != "" {
		proxied.Records = append(proxied.Records, output.Record)
	}

	return proxied
}

func indexListingTarget(
	ctx context.Context,
	indexer codeintel.ASTIndexer,
	root string,
	targetPath string,
	invocation agentproxy.DirectoryListingInvocation,
) error {
	if invocation.Recursive {
		_, err := indexer.IndexPaths(ctx, root, []string{targetPath})
		if err != nil {
			return fmt.Errorf("index recursive listing target: %w", err)
		}

		return nil
	}

	_, err := indexer.IndexDirectoryChildren(ctx, root, targetPath)
	if err != nil {
		return fmt.Errorf("index listing directory children: %w", err)
	}

	return nil
}

func directoryListingInvocation(
	command string,
) (agentproxy.DirectoryListingInvocation, bool) {
	commands, err := shellparse.Commands(command)
	if err != nil || len(commands) != 1 {
		return agentproxy.DirectoryListingInvocation{}, false
	}

	return agentproxy.DetectShellDirectoryListingInvocation(commands[0])
}

func repoRelativeListingPath(root, cwd, path string) (string, bool) {
	root = strings.TrimSpace(root)
	cwd = strings.TrimSpace(cwd)
	path = strings.TrimSpace(path)

	if root == "" || cwd == "" {
		return "", false
	}

	if path == "" {
		path = "."
	}

	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}

	absolutePath := path
	if !filepath.IsAbs(absolutePath) {
		absolutePath = filepath.Join(cwd, absolutePath)
	}

	absolutePath, err = filepath.Abs(absolutePath)
	if err != nil {
		return "", false
	}

	relativePath, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil {
		return "", false
	}

	if relativePath == ".." ||
		strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", false
	}

	relativePath = filepath.ToSlash(relativePath)
	if relativePath == "." {
		return ".", true
	}

	return relativePath, true
}

func compressToolOutputWithRecords(event Event, output string) proxiedToolOutput {
	options := loadHookOutputCompressionOptions(event.Cwd)

	compressed, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputDiagnosticSummaryTransform{
			Tool:        inferDiagnosticTool(event.Command()),
			MaxFindings: options.MaxDiagnostics,
		},
		agentproxy.ToolOutputCompressionTransform{
			MaxLines: options.MaxLines,
			Head:     options.HeadLines,
			Tail:     options.TailLines,
		},
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:  options.MaxTokens,
			HeadTokens: options.HeadTokens,
			TailTokens: options.TailTokens,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{Text: output},
	)
	if err != nil {
		return proxiedToolOutput{Text: output}
	}

	return proxiedToolOutput{
		Text:    compressed.Text,
		Records: compressed.Records,
	}
}

func proxyToolOutputEvents(
	event Event,
	input string,
	proxied proxiedToolOutput,
) []agentproxy.ProviderEvent {
	sessionID := strings.TrimSpace(event.SessionID)
	if sessionID == "" || strings.TrimSpace(input) == "" {
		return nil
	}

	if strings.TrimSpace(event.Cwd) == "" {
		return nil
	}

	root := gitRootFromPath(event.Cwd)
	if root == "" {
		return nil
	}

	recordedAt := time.Now().UTC()
	outputHash := agentproxy.HashText(proxied.Text)
	inputHash := agentproxy.HashText(input)
	eventID := proxyToolOutputEventID(event, inputHash, recordedAt)

	return []agentproxy.ProviderEvent{{
		ID:            eventID,
		SessionID:     sessionID,
		Kind:          agentproxy.EventToolOutput,
		Provider:      event.Provider(),
		Tool:          event.ToolName,
		RecordedAtUTC: recordedAt,
		RepoRoot:      root,
		Cwd:           strings.TrimSpace(event.Cwd),
		TraceID:       proxyToolOutputTraceID(event, inputHash),
		Direction:     agentproxy.DirectionLocal,
		PayloadKind:   agentproxy.PayloadToolOutput,
		InputHash:     inputHash,
		OutputHash:    outputHash,
		PolicyID:      proxyToolOutputPolicyID(proxied.Records),
		Decision:      proxyToolOutputDecision(proxied.Records),
		Policy: agentproxy.PolicyEvidence{
			PolicyID: proxyToolOutputPolicyID(proxied.Records),
			Decision: proxyToolOutputDecision(proxied.Records),
			Reason:   "live Bash tool output proxy transform",
		},
		Payload: agentproxy.PayloadMeasurement{
			Bytes: len([]byte(proxied.Text)),
			Lines: lineCount(proxied.Text),
		},
		TokenUsage: agentproxy.TokenUsage{
			InputTokens:  agentproxy.WhitespaceTokenizer{}.Count(input),
			OutputTokens: agentproxy.WhitespaceTokenizer{}.Count(proxied.Text),
			TotalTokens:  agentproxy.WhitespaceTokenizer{}.Count(proxied.Text),
		},
		Transforms: proxied.Records,
	}}
}

func proxyToolOutputDecision(records []agentproxy.TransformRecord) string {
	decision := proxyDecisionAllow

	for _, record := range records {
		if record.Decision == proxyDecisionTruncate {
			return proxyDecisionTruncate
		}

		if record.Decision == proxyDecisionSummarize {
			decision = proxyDecisionSummarize
		}

		if record.Decision == proxyDecisionInject &&
			decision == proxyDecisionAllow {
			decision = proxyDecisionInject
		}
	}

	return decision
}

func proxyToolOutputPolicyID(records []agentproxy.TransformRecord) string {
	if hasDirectoryAnatomyTransform(records) {
		return proxyPolicyDirectoryAnatomy
	}

	switch proxyToolOutputDecision(records) {
	case proxyDecisionSummarize:
		return proxyPolicyDiagnosticSummary
	case proxyDecisionInject:
		return proxyPolicyDirectoryAnatomy
	case proxyDecisionTruncate:
		return proxyPolicyTokenBudget
	default:
		return ""
	}
}

func proxyToolOutputEventID(
	event Event,
	inputHash string,
	recordedAt time.Time,
) string {
	return stableHookID(
		"proxy-tool-output",
		event.SessionID,
		event.Provider(),
		event.ToolName,
		inputHash,
		recordedAt.Format(time.RFC3339Nano),
	)
}

func proxyToolOutputTraceID(event Event, inputHash string) string {
	return stableHookID(
		"proxy-tool-output-trace",
		event.SessionID,
		event.Provider(),
		event.ToolName,
		inputHash,
	)
}

func stableHookID(prefix string, values ...string) string {
	hash := sha256.New()
	_, _ = hash.Write([]byte(prefix))

	for _, value := range values {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strings.TrimSpace(value)))
	}

	return prefix + ":" + hex.EncodeToString(hash.Sum(nil))[:24]
}

type hookOutputCompressionOptions struct {
	MaxLines       int
	HeadLines      int
	TailLines      int
	MaxTokens      int
	HeadTokens     int
	TailTokens     int
	MaxDiagnostics int
}

func loadHookOutputCompressionOptions(cwd string) hookOutputCompressionOptions {
	options := defaultHookOutputCompressionOptions()

	if strings.TrimSpace(cwd) != "" {
		root := gitRootFromPath(cwd)
		if root != "" {
			options = options.withRepoConfig(root)
		}
	}

	return options.withEnvOverrides()
}

func defaultHookOutputCompressionOptions() hookOutputCompressionOptions {
	return hookOutputCompressionOptions{
		MaxLines:       defaultHookOutputMaxLines,
		HeadLines:      defaultHookOutputHeadLines,
		TailLines:      defaultHookOutputTailLines,
		MaxTokens:      defaultHookOutputMaxTokens,
		HeadTokens:     defaultHookOutputHeadTokens,
		TailTokens:     defaultHookOutputTailTokens,
		MaxDiagnostics: defaultHookOutputDiagnostics,
	}
}

func (options hookOutputCompressionOptions) withRepoConfig(
	root string,
) hookOutputCompressionOptions {
	config, err := loadHookRepoConfig(root)
	if err != nil {
		return options
	}

	settings := configdata.MapValue(
		configdata.GetPath(config, "proxy.output_compression", map[string]any{}),
	)
	if settings == nil {
		return options
	}

	options.MaxLines = positiveConfigInt(settings, "max_lines", options.MaxLines)
	options.HeadLines = positiveConfigInt(settings, "head_lines", options.HeadLines)
	options.TailLines = positiveConfigInt(settings, "tail_lines", options.TailLines)
	options.MaxTokens = positiveConfigInt(settings, "max_tokens", options.MaxTokens)
	options.HeadTokens = positiveConfigInt(settings, "head_tokens", options.HeadTokens)
	options.TailTokens = positiveConfigInt(settings, "tail_tokens", options.TailTokens)
	options.MaxDiagnostics = positiveConfigInt(
		settings,
		"max_diagnostics",
		options.MaxDiagnostics,
	)

	return options
}

func loadHookRepoConfig(root string) (configdata.Map, error) {
	for _, name := range configdata.RepoConfigCandidates() {
		path := filepath.Join(root, name)

		config, err := configdata.LoadYAMLMap(path)
		if err == nil {
			return config, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load hook repo config %s: %w", path, err)
		}
	}

	return configdata.Map{}, nil
}

func positiveConfigInt(values configdata.Map, key string, fallback int) int {
	value := configdata.IntAt(values, key)
	if value <= 0 {
		return fallback
	}

	return value
}

func (
	options hookOutputCompressionOptions,
) withEnvOverrides() hookOutputCompressionOptions {
	options.MaxTokens = hookOutputIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS",
		options.MaxTokens,
	)
	options.HeadTokens = hookOutputIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS",
		options.HeadTokens,
	)
	options.TailTokens = hookOutputIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS",
		options.TailTokens,
	)

	return options
}

func inferDiagnosticTool(command string) string {
	argv := commandArgv(command)
	tool := inferGoDiagnosticTool(argv)

	if tool != "" {
		return tool
	}

	tool = diagnostics.InferTool(argv)
	if diagnostics.HasParser(tool) {
		return tool
	}

	return ""
}

func inferGoDiagnosticTool(argv []string) string {
	for index, arg := range argv {
		if strings.TrimSpace(arg) != "go" || index+1 >= len(argv) {
			continue
		}

		switch strings.TrimSpace(argv[index+1]) {
		case "test":
			return "go-test"
		case "vet":
			return "go-vet"
		}
	}

	return ""
}

func hookOutputIntEnv(name string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}

	return parsed
}

func lineCount(text string) int {
	count := 0
	for range strings.Lines(text) {
		count++
	}

	return count
}
