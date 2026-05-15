// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
)

const (
	defaultHookOutputMaxTokens  = 2000
	defaultHookOutputHeadTokens = 900
	defaultHookOutputTailTokens = 900
)

type proxiedToolOutput struct {
	Text    string
	Records []agentproxy.TransformRecord
	Events  []agentproxy.ProviderEvent
}

func proxyPostToolOutput(event Event, output string) proxiedToolOutput {
	normalizer := hookOutputNormalizer(event.Cwd)
	normalized := normalizer.preserveLines(output)
	proxied := compressToolOutputWithRecords(normalized)
	proxied.Events = proxyToolOutputEvents(event, normalized, proxied)

	return proxied
}

func compressToolOutputWithRecords(output string) proxiedToolOutput {
	compressed, err := agentproxy.NewPipeline(
		nil,
		agentproxy.ToolOutputCompressionTransform{},
		agentproxy.ToolOutputTokenBudgetTransform{
			MaxTokens:  hookOutputMaxTokens(),
			HeadTokens: hookOutputHeadTokens(),
			TailTokens: hookOutputTailTokens(),
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
		PolicyID:      "proxy.token_budget",
		Decision:      proxyToolOutputDecision(proxied.Records),
		Policy: agentproxy.PolicyEvidence{
			PolicyID: "proxy.token_budget",
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
	for _, record := range records {
		if record.Decision == "truncate" {
			return "truncate"
		}
	}

	return "allow"
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

func hookOutputMaxTokens() int {
	return hookOutputIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS",
		defaultHookOutputMaxTokens,
	)
}

func hookOutputHeadTokens() int {
	return hookOutputIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS",
		defaultHookOutputHeadTokens,
	)
}

func hookOutputTailTokens() int {
	return hookOutputIntEnv(
		"CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS",
		defaultHookOutputTailTokens,
	)
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
