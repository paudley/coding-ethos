// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/astfacts"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	defaultHookOutputMaxTokens   = 2000
	defaultHookOutputHeadTokens  = 900
	defaultHookOutputTailTokens  = 900
	defaultFileReadChunkLimit    = 512
	defaultHookOutputMaxLines    = 80
	defaultHookOutputHeadLines   = 32
	defaultHookOutputTailLines   = 32
	defaultHookOutputDiagnostics = 12
	defaultAnatomyMapSymbols     = 6
	defaultAnatomyMapTimeout     = 5 * time.Second
	defaultFileReadPageLines     = 100
	defaultFileReadSemanticSlack = 50
	semanticBackupTargetDivisor  = 2
	proxyDecisionAllow           = "allow"
	proxyDecisionInject          = "inject"
	proxyDecisionSummarize       = "summarize"
	proxyDecisionTruncate        = "truncate"
	proxyPolicyDiagnosticSummary = "proxy.diagnostic_summary"
	proxyPolicyDirectoryAnatomy  = "proxy.directory_anatomy"
	proxyPolicyFilePagination    = agentproxy.FileReadPaginationPolicyID
	proxyPolicyTokenBudget       = "proxy.token_budget"
)

type proxiedToolOutput struct {
	Metadata map[string]string
	Text     string
	Records  []agentproxy.TransformRecord
	Events   []agentproxy.ProviderEvent
}

func proxyPostToolOutput(event Event, output string) proxiedToolOutput {
	normalizer := hookOutputNormalizer(event.Cwd)
	normalized := normalizer.preserveLines(output)
	proxied := proxiedToolOutput{Text: normalized}

	proxied = paginateFileReadToolOutput(event, proxied)
	if !hasFileReadTransformChange(proxied.Records) {
		compressed := compressToolOutputWithRecords(event, proxied.Text)
		proxied.Text = compressed.Text
		proxied.Records = append(proxied.Records, compressed.Records...)
		proxied.Metadata = mergeStringMetadata(proxied.Metadata, compressed.Metadata)
	}

	proxied = enrichDirectoryListingToolOutput(event, proxied)
	proxied.Events = proxyToolOutputEvents(event, event.Command(), proxied)

	return proxied
}

func paginateFileReadToolOutput(
	event Event,
	proxied proxiedToolOutput,
) proxiedToolOutput {
	if event.ReturnCode() != 0 {
		return proxied
	}

	invocation, readDetected := fileReadInvocation(event.Command())
	if !readDetected {
		return proxied
	}

	root := gitRootFromPath(event.Cwd)
	if root == "" {
		return proxied
	}

	targetPath, ok := repoRelativeListingPath(root, event.Cwd, invocation.Path)
	if !ok || !isRepoFile(root, targetPath) {
		return proxied
	}

	pageEnd := semanticFileReadPageEnd(root, targetPath, proxied.Text)
	options := loadHookOutputCompressionOptions(event)
	tokenBudget := resolveHookTokenBudget(event, options)

	output, err := agentproxy.NewPipeline(
		nil,
		agentproxy.FileReadPaginationTransform{
			Path:      targetPath,
			PageStart: 1,
			PageEnd:   pageEnd,
		},
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:  tokenBudget.MaxTokens,
			HeadTokens: options.HeadTokens,
			TailTokens: options.TailTokens,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{
			Metadata: tokenBudget.metadata(),
			Text:     proxied.Text,
		},
	)
	if err != nil {
		return proxied
	}

	proxied.Text = output.Text
	proxied.Records = append(proxied.Records, output.Records...)
	proxied.Metadata = mergeStringMetadata(proxied.Metadata, output.Metadata)

	return proxied
}

func fileReadInvocation(command string) (agentproxy.FileReadInvocation, bool) {
	commands, err := shellparse.Commands(command)
	if err != nil || len(commands) != 1 {
		return agentproxy.FileReadInvocation{}, false
	}

	return agentproxy.DetectShellFileReadInvocation(commands[0])
}

func isRepoFile(root, targetPath string) bool {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(targetPath)))
	if err != nil {
		return false
	}

	return !info.IsDir()
}

func semanticFileReadPageEnd(root, targetPath, output string) int {
	totalLines := agentproxy.OutputLineCount(output)
	if totalLines <= 0 {
		return defaultFileReadPageLines
	}

	pageEnd := min(defaultFileReadPageLines, totalLines)
	if totalLines <= pageEnd {
		return pageEnd
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		defaultAnatomyMapTimeout,
	)
	defer cancel()

	store, err := codeintel.Open(ctx, codeintel.DefaultDBPath(root))
	if err != nil {
		return pageEnd
	}
	defer store.Close()

	contentHash := astfacts.ContentHash([]byte(output))

	chunks, cached, err := cachedSemanticChunks(
		ctx,
		store,
		targetPath,
		contentHash,
	)
	if err == nil && cached {
		return semanticPageEnd(pageEnd, totalLines, chunks)
	}

	indexer := codeintel.NewASTIndexer(store)

	_, err = indexer.IndexPaths(ctx, root, []string{targetPath})
	if err != nil {
		return pageEnd
	}

	chunks, err = querySemanticChunks(
		ctx,
		store,
		targetPath,
	)
	if err != nil {
		return pageEnd
	}

	return semanticPageEnd(pageEnd, totalLines, chunks)
}

func cachedSemanticChunks(
	ctx context.Context,
	store *codeintel.Store,
	targetPath string,
	contentHash string,
) ([]codeintel.CodeChunk, bool, error) {
	file, found, err := store.GetCodeFile(ctx, targetPath)
	if err != nil {
		return nil, false, fmt.Errorf("get cached code file: %w", err)
	}

	if !found {
		return nil, false, nil
	}

	if file.ContentHash != contentHash || file.DeletedAtUTC != "" ||
		(file.StaleReason != "" && file.StaleReason != "too_many_chunks") {
		return nil, false, nil
	}

	chunks, err := querySemanticChunks(ctx, store, targetPath)
	if err != nil {
		return nil, false, err
	}

	if len(chunks) == 0 {
		return nil, false, nil
	}

	return chunks, true, nil
}

func querySemanticChunks(
	ctx context.Context,
	store *codeintel.Store,
	targetPath string,
) ([]codeintel.CodeChunk, error) {
	chunks, err := store.CodeChunks(
		ctx,
		codeintel.CodeChunkQuery{
			Path:  targetPath,
			Limit: defaultFileReadChunkLimit,
		},
	)
	if err != nil {
		return nil, fmt.Errorf("query semantic chunks: %w", err)
	}

	return chunks, nil
}

func semanticPageEnd(
	target int,
	totalLines int,
	chunks []codeintel.CodeChunk,
) int {
	candidate := semanticPageCandidate{fallback: target}

	for _, chunk := range chunks {
		if chunk.StartLine > target {
			break
		}

		candidate = candidate.withChunk(target, totalLines, chunk)
	}

	return candidate.pageEnd()
}

type semanticPageCandidate struct {
	bestEnd  int
	fallback int
}

func (candidate semanticPageCandidate) withChunk(
	target int,
	totalLines int,
	chunk codeintel.CodeChunk,
) semanticPageCandidate {
	if !isCrossingSemanticChunk(target, chunk) {
		return candidate
	}

	semanticLimit := min(target+defaultFileReadSemanticSlack, totalLines)
	if chunk.EndLine <= semanticLimit {
		return candidate.withBestEnd(chunk.EndLine)
	}

	semanticStartFloor := max(target-defaultFileReadSemanticSlack, 1)
	backupFloor := semanticBackupFloor(target)
	backupEnd := chunk.StartLine - 1

	if chunk.StartLine >= semanticStartFloor && backupEnd >= backupFloor {
		candidate.fallback = min(candidate.fallback, backupEnd)
	}

	return candidate
}

func semanticBackupFloor(target int) int {
	return max(target/semanticBackupTargetDivisor, 1)
}

func (candidate semanticPageCandidate) withBestEnd(endLine int) semanticPageCandidate {
	if candidate.bestEnd == 0 || endLine < candidate.bestEnd {
		candidate.bestEnd = endLine
	}

	return candidate
}

func (candidate semanticPageCandidate) pageEnd() int {
	if candidate.bestEnd > 0 {
		return candidate.bestEnd
	}

	return candidate.fallback
}

func isCrossingSemanticChunk(target int, chunk codeintel.CodeChunk) bool {
	return chunk.StartLine > 0 &&
		chunk.EndLine > target &&
		chunk.SymbolPath != ""
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
		_, err := indexer.IndexDirectoryTree(
			ctx,
			root,
			targetPath,
			invocation.MaxDepth,
		)
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
	options := loadHookOutputCompressionOptions(event)
	tokenBudget := resolveHookTokenBudget(event, options)

	compressed, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputDiagnosticSummaryTransform{
			Tool:             inferDiagnosticTool(event.Command()),
			MaxFindings:      options.MaxDiagnostics,
			EvidenceMaxAge:   options.TempEvidenceMaxAge,
			EvidenceMaxBytes: options.TempEvidenceMaxBytes,
		},
		agentproxy.ToolOutputCompressionTransform{
			MaxLines:         options.MaxLines,
			Head:             options.HeadLines,
			Tail:             options.TailLines,
			EvidenceMaxAge:   options.TempEvidenceMaxAge,
			EvidenceMaxBytes: options.TempEvidenceMaxBytes,
		},
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:        tokenBudget.MaxTokens,
			HeadTokens:       options.HeadTokens,
			TailTokens:       options.TailTokens,
			EvidenceMaxAge:   options.TempEvidenceMaxAge,
			EvidenceMaxBytes: options.TempEvidenceMaxBytes,
		},
	).Apply(
		context.Background(),
		agentproxy.TransformInput{
			Metadata: tokenBudget.metadata(),
			Text:     output,
		},
	)
	if err != nil {
		return proxiedToolOutput{Text: output}
	}

	return proxiedToolOutput{
		Text:     compressed.Text,
		Records:  compressed.Records,
		Metadata: compressed.Metadata,
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
	tokenizer := agentproxy.ApproximateTokenizer{}
	inputTokens := tokenizer.Count(input)
	outputTokens := tokenizer.Count(proxied.Text)

	return []agentproxy.ProviderEvent{{
		ID:            eventID,
		SessionID:     sessionID,
		Kind:          agentproxy.EventToolOutput,
		Provider:      event.Provider(),
		Model:         strings.TrimSpace(event.Model),
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
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
		Metadata:   cloneStringMetadata(proxied.Metadata),
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

	if hasFilePaginationTransform(records) {
		return proxyPolicyFilePagination
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

func mergeStringMetadata(
	base map[string]string,
	additional map[string]string,
) map[string]string {
	merged := cloneStringMetadata(base)

	for key, value := range additional {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
			continue
		}

		if merged == nil {
			merged = map[string]string{}
		}

		merged[key] = value
	}

	return merged
}

func cloneStringMetadata(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}

	cloned := make(map[string]string, len(values))
	maps.Copy(cloned, values)

	return cloned
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
	MaxTokensSource      string
	MaxLines             int
	HeadLines            int
	TailLines            int
	MaxTokens            int
	HeadTokens           int
	TailTokens           int
	MaxDiagnostics       int
	TempEvidenceMaxAge   time.Duration
	TempEvidenceMaxBytes int64
}

func loadHookOutputCompressionOptions(event Event) hookOutputCompressionOptions {
	options := defaultHookOutputCompressionOptions()

	if strings.TrimSpace(event.Cwd) != "" {
		root := gitRootFromPath(event.Cwd)
		if root != "" {
			options = options.withRepoConfig(root)
		}
	}

	return options.withEnvOverrides()
}

func defaultHookOutputCompressionOptions() hookOutputCompressionOptions {
	return hookOutputCompressionOptions{
		MaxLines:           defaultHookOutputMaxLines,
		HeadLines:          defaultHookOutputHeadLines,
		TailLines:          defaultHookOutputTailLines,
		MaxTokens:          defaultHookOutputMaxTokens,
		MaxTokensSource:    tokenBudgetSourceFallback,
		HeadTokens:         defaultHookOutputHeadTokens,
		TailTokens:         defaultHookOutputTailTokens,
		MaxDiagnostics:     defaultHookOutputDiagnostics,
		TempEvidenceMaxAge: outputsurface.DefaultTempEvidenceMaxAge,
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

	if maxTokens := positiveConfigInt(settings, "max_tokens", 0); maxTokens > 0 {
		options.MaxTokens = maxTokens
		options.MaxTokensSource = tokenBudgetSourceRepoConfig
	}

	options.HeadTokens = positiveConfigInt(settings, "head_tokens", options.HeadTokens)
	options.TailTokens = positiveConfigInt(settings, "tail_tokens", options.TailTokens)
	options.MaxDiagnostics = positiveConfigInt(
		settings,
		"max_diagnostics",
		options.MaxDiagnostics,
	)

	lifecycle, err := outputsurface.LoadSettings(root)
	if err == nil {
		policy := lifecycle.Prune.Surfaces["proxy_temp_evidence"]
		if policy.MaxAge > 0 {
			options.TempEvidenceMaxAge = policy.MaxAge
		}

		if policy.MaxBytes > 0 {
			options.TempEvidenceMaxBytes = policy.MaxBytes
		}
	} else {
		debuglog.Debug(
			"proxy.output_compression.lifecycle_config.warn",
			zap.String("root", root),
			zap.Error(err),
		)
	}

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
	if maxTokens := hookOutputPositiveIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS",
	); maxTokens > 0 {
		options.MaxTokens = maxTokens
		options.MaxTokensSource = tokenBudgetSourceEnv
	}

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

func hookOutputPositiveIntEnv(name string) int {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return 0
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0
	}

	return parsed
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
