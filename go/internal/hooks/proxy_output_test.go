// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestProxyToolOutputPolicyIDPrefersDirectoryAnatomy(t *testing.T) {
	t.Parallel()

	records := []agentproxy.TransformRecord{
		{
			Name:     "token-budget",
			Decision: proxyDecisionTruncate,
		},
		{
			Name:     codeintel.DirectoryAnatomyTransformName,
			Decision: proxyDecisionInject,
		},
	}

	if got := proxyToolOutputPolicyID(records); got != proxyPolicyDirectoryAnatomy {
		t.Fatalf("policy ID mismatch: got %q, want %q", got, proxyPolicyDirectoryAnatomy)
	}

	if got := proxyToolOutputDecision(records); got != proxyDecisionTruncate {
		t.Fatalf("decision mismatch: got %q, want %q", got, proxyDecisionTruncate)
	}
}

func TestProxyToolOutputPolicyIDPrefersFilePaginationOverTokenBudget(t *testing.T) {
	t.Parallel()

	records := []agentproxy.TransformRecord{
		{
			Name:     "token-budget",
			Decision: proxyDecisionTruncate,
		},
		{
			Name:     agentproxy.FileReadPaginationTransformName,
			Decision: proxyDecisionTruncate,
		},
	}

	if got := proxyToolOutputPolicyID(records); got != proxyPolicyFilePagination {
		t.Fatalf("policy ID mismatch: got %q, want %q", got, proxyPolicyFilePagination)
	}
}

func TestProxyPostToolOutputAppliesTokenBudgetAfterFilePagination(t *testing.T) {
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS", "80")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS", "20")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS", "20")

	repo := initProxyOutputRepo(t)
	err := os.MkdirAll(filepath.Join(repo, "docs"), 0o700)
	if err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	line := "alpha beta gamma delta epsilon zeta eta theta iota kappa"
	lines := make([]string, 0, 120)
	for index := 0; index < 120; index++ {
		lines = append(lines, fmt.Sprintf("%s %d", line, index))
	}

	output := strings.Join(lines, "\n") + "\n"
	path := filepath.Join(repo, "docs", "large.md")
	err = os.WriteFile(path, []byte(output), 0o600)
	if err != nil {
		t.Fatalf("write large file: %v", err)
	}

	proxied := proxyPostToolOutput(
		Event{
			SessionID: "session-file-read-token-budget",
			ToolName:  toolBash,
			Cwd:       repo,
			ToolInput: map[string]any{
				"command": "cat docs/large.md",
			},
			ToolResponse: map[string]any{
				"stdout":      output,
				"return_code": 0,
			},
		},
		output,
	)

	if !strings.Contains(proxied.Text, "paginated file read") ||
		!strings.Contains(proxied.Text, "token budget hard stop") {
		t.Fatalf("expected paginated and token-budgeted output: %s", proxied.Text)
	}

	paginationEvidence := ""
	foundTokenBudget := false
	for _, record := range proxied.Records {
		switch record.Name {
		case agentproxy.FileReadPaginationTransformName:
			if record.Decision != proxyDecisionTruncate {
				t.Fatalf("pagination record = %#v", record)
			}
			paginationEvidence = record.EvidencePath
		case "tool-output-token-budget":
			if record.Decision == proxyDecisionTruncate {
				foundTokenBudget = true
				if record.EvidencePath == "" ||
					record.EvidencePath != paginationEvidence {
					t.Fatalf(
						"token budget evidence did not preserve original path: %#v",
						proxied.Records,
					)
				}
			}
		}
	}

	if paginationEvidence == "" || !foundTokenBudget {
		t.Fatalf("missing expected transform records: %#v", proxied.Records)
	}
}

func TestResolveHookTokenBudgetUsesExplicitAndTieredSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		event      Event
		name       string
		wantSource string
		options    hookOutputCompressionOptions
		wantMax    int
	}{
		{
			name:  "repo config wins",
			event: Event{ContextWindowTokens: 1000000, Model: "large-context"},
			options: hookOutputCompressionOptions{
				MaxTokens:       7000,
				MaxTokensSource: tokenBudgetSourceRepoConfig,
			},
			wantSource: tokenBudgetSourceRepoConfig,
			wantMax:    7000,
		},
		{
			name:  "repo config clamps to safety max",
			event: Event{ContextWindowTokens: 1000000, Model: "large-context"},
			options: hookOutputCompressionOptions{
				MaxTokens:       tokenBudgetSafetyMaxTokens + 1,
				MaxTokensSource: tokenBudgetSourceRepoConfig,
			},
			wantSource: tokenBudgetSourceRepoConfig,
			wantMax:    tokenBudgetSafetyMaxTokens,
		},
		{
			name:  "env clamps to safety max",
			event: Event{ContextWindowTokens: 1000000, Model: "large-context"},
			options: hookOutputCompressionOptions{
				MaxTokens:       tokenBudgetSafetyMaxTokens + 1,
				MaxTokensSource: tokenBudgetSourceEnv,
			},
			wantSource: tokenBudgetSourceEnv,
			wantMax:    tokenBudgetSafetyMaxTokens,
		},
		{
			name: "known context uses tier",
			event: Event{
				ContextWindowTokens: 1000000,
				Model:               "gemini-1m",
			},
			options: hookOutputCompressionOptions{
				MaxTokens:       defaultHookOutputMaxTokens,
				MaxTokensSource: tokenBudgetSourceFallback,
			},
			wantSource: tokenBudgetSourceModelContext,
			wantMax:    24000,
		},
		{
			name:  "unknown context falls back",
			event: Event{Model: "unknown"},
			options: hookOutputCompressionOptions{
				MaxTokens:       defaultHookOutputMaxTokens,
				MaxTokensSource: tokenBudgetSourceFallback,
			},
			wantSource: tokenBudgetSourceFallback,
			wantMax:    defaultHookOutputMaxTokens,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := resolveHookTokenBudget(test.event, test.options)
			if got.Source != test.wantSource || got.MaxTokens != test.wantMax {
				t.Fatalf("resolution = %#v, want source=%q max=%d",
					got,
					test.wantSource,
					test.wantMax,
				)
			}
		})
	}
}

func TestProxyPostToolOutputBudgetsDenseGenericOutputAndRecordsLedger(t *testing.T) {
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS", "80")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS", "20")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS", "20")

	repo := initProxyOutputRepo(t)
	output := `{"rows":[` + strings.Repeat(
		`{"id":"abcdef0123456789","value":"payload"},`,
		120,
	) + `]}`

	proxied := proxyPostToolOutput(
		Event{
			ProviderHint: "codex",
			SessionID:    "session-dense-output",
			ToolName:     toolBash,
			Cwd:          repo,
			Model:        "test-model",
			ToolInput: map[string]any{
				"command": "cat dense.json",
			},
			ToolResponse: map[string]any{
				"stdout":      output,
				"return_code": 0,
			},
		},
		output,
	)

	if !strings.Contains(proxied.Text, "[WARNING: Payload exceeded 80 tokens.") ||
		strings.Contains(proxied.Text, output) ||
		len(proxied.Events) != 1 {
		t.Fatalf("unexpected dense proxy output: events=%#v text=%s",
			proxied.Events,
			proxied.Text,
		)
	}

	event := proxied.Events[0]
	if event.Decision != proxyDecisionTruncate ||
		event.PolicyID != proxyPolicyTokenBudget ||
		event.TokenUsage.OutputTokens <= 0 ||
		event.Metadata["coding_ethos.token_budget.source"] != tokenBudgetSourceEnv ||
		event.Metadata["coding_ethos.token_budget.max_tokens"] != "80" ||
		event.Model != "test-model" {
		t.Fatalf("unexpected dense proxy ledger event: %#v", event)
	}
}

func TestProxyPostToolOutputRecordsAllowLedger(t *testing.T) {
	repo := initProxyOutputRepo(t)

	proxied := proxyPostToolOutput(
		Event{
			ProviderHint: "codex",
			SessionID:    "session-allow-output",
			ToolName:     toolBash,
			Cwd:          repo,
			ToolInput: map[string]any{
				"command": "printf ok",
			},
			ToolResponse: map[string]any{
				"stdout":      "ok\n",
				"return_code": 0,
			},
		},
		"ok\n",
	)

	if len(proxied.Events) != 1 {
		t.Fatalf("expected allow ledger event: %#v", proxied.Events)
	}

	event := proxied.Events[0]
	if event.Decision != proxyDecisionAllow ||
		event.TokenUsage.OutputTokens <= 0 ||
		event.TokenUsage.TotalTokens !=
			event.TokenUsage.InputTokens+event.TokenUsage.OutputTokens ||
		event.Metadata["coding_ethos.token_budget.source"] != tokenBudgetSourceFallback ||
		event.Metadata["coding_ethos.token_budget.max_tokens"] !=
			fmt.Sprint(defaultHookOutputMaxTokens) {
		t.Fatalf("unexpected allow ledger event: %#v", event)
	}
}

func TestProxyPostToolOutputRecordsEmptyOutputLedger(t *testing.T) {
	repo := initProxyOutputRepo(t)

	proxied := proxyPostToolOutput(
		Event{
			ProviderHint: "codex",
			SessionID:    "session-empty-output",
			ToolName:     toolBash,
			Cwd:          repo,
			ToolInput: map[string]any{
				"command": "printf ''",
			},
			ToolResponse: map[string]any{
				"stdout":      "",
				"return_code": 0,
			},
		},
		"",
	)

	if len(proxied.Events) != 1 {
		t.Fatalf("expected empty-output allow ledger event: %#v", proxied.Events)
	}

	event := proxied.Events[0]
	if event.Decision != proxyDecisionAllow ||
		event.InputHash != agentproxy.HashText("printf ''") ||
		event.Payload.Bytes != 0 ||
		event.TokenUsage.InputTokens <= 0 ||
		event.TokenUsage.OutputTokens != 0 ||
		event.TokenUsage.TotalTokens != event.TokenUsage.InputTokens {
		t.Fatalf("unexpected empty-output ledger event: %#v", event)
	}
}

func TestProxyPostToolOutputCompressesAllowedFileRead(t *testing.T) {
	repo := initProxyOutputRepo(t)
	err := os.MkdirAll(filepath.Join(repo, "docs"), 0o700)
	if err != nil {
		t.Fatalf("create docs dir: %v", err)
	}

	lines := make([]string, 0, 90)
	for index := 0; index < 90; index++ {
		lines = append(lines, fmt.Sprintf("line %02d", index+1))
	}

	output := strings.Join(lines, "\n") + "\n"
	err = os.WriteFile(filepath.Join(repo, "docs", "small.md"), []byte(output), 0o600)
	if err != nil {
		t.Fatalf("write small file: %v", err)
	}

	proxied := proxyPostToolOutput(
		Event{
			SessionID: "session-file-read-line-compression",
			ToolName:  toolBash,
			Cwd:       repo,
			ToolInput: map[string]any{
				"command": "cat docs/small.md",
			},
			ToolResponse: map[string]any{
				"stdout":      output,
				"return_code": 0,
			},
		},
		output,
	)

	if !strings.Contains(proxied.Text, "compressed tool output") {
		t.Fatalf("expected generic compression after allowed file read: %s", proxied.Text)
	}

	foundPaginationAllow := false
	foundCompressionTruncate := false
	for _, record := range proxied.Records {
		switch record.Name {
		case agentproxy.FileReadPaginationTransformName:
			foundPaginationAllow = record.Decision == proxyDecisionAllow
		case "tool-output-compression":
			foundCompressionTruncate = record.Decision == proxyDecisionTruncate
		}
	}

	if !foundPaginationAllow || !foundCompressionTruncate {
		t.Fatalf("missing expected records: %#v", proxied.Records)
	}
}

func TestSemanticPageEndPrefersNestedBoundaryWithinSlack(t *testing.T) {
	t.Parallel()

	chunks := []codeintel.CodeChunk{
		{
			SymbolPath: "Widget",
			StartLine:  50,
			EndLine:    250,
		},
		{
			SymbolPath:       "Widget.build",
			ParentSymbolPath: "Widget",
			StartLine:        90,
			EndLine:          120,
		},
	}

	if got := semanticPageEnd(100, 260, chunks); got != 120 {
		t.Fatalf("page end = %d, want 120", got)
	}
}

func TestSemanticPageEndDoesNotBackUpToDistantWrapper(t *testing.T) {
	t.Parallel()

	chunks := []codeintel.CodeChunk{
		{
			SymbolPath: "Widget",
			StartLine:  20,
			EndLine:    250,
		},
	}

	if got := semanticPageEnd(100, 260, chunks); got != 100 {
		t.Fatalf("page end = %d, want 100", got)
	}
}

func TestSemanticPageEndDoesNotBackUpBelowHalfTarget(t *testing.T) {
	t.Parallel()

	chunks := []codeintel.CodeChunk{
		{
			SymbolPath: "Widget",
			StartLine:  40,
			EndLine:    250,
		},
	}

	if got := semanticPageEnd(100, 260, chunks); got != 100 {
		t.Fatalf("page end = %d, want 100", got)
	}
}

func TestSemanticPageEndBacksUpToNearbyLargeWrapper(t *testing.T) {
	t.Parallel()

	chunks := []codeintel.CodeChunk{
		{
			SymbolPath: "Widget",
			StartLine:  61,
			EndLine:    250,
		},
	}

	if got := semanticPageEnd(100, 260, chunks); got != 60 {
		t.Fatalf("page end = %d, want 60", got)
	}
}

func initProxyOutputRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	command := exec.Command("git", "init")
	command.Dir = repo

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, string(output))
	}

	return repo
}
