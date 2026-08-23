// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

const directAnatomyLargeFixtureBytes = 2 * 1024 * 1024

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

func TestDirectDirectoryAnatomySkipsIgnoredLargeAndSymlinkFiles(t *testing.T) {
	repo := initProxyOutputRepo(t)
	pkgDir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatalf("create package dir: %v", err)
	}

	writeProxyOutputFile(t, filepath.Join(repo, ".gitignore"), "pkg/ignored.go\n")
	writeProxyOutputFile(
		t,
		filepath.Join(pkgDir, "safe.go"),
		"package pkg\n\nfunc Safe() {}\n",
	)
	writeProxyOutputFile(
		t,
		filepath.Join(pkgDir, "ignored.go"),
		"package pkg\n\nfunc Ignored() {}\n",
	)
	writeLargeProxyOutputFile(t, filepath.Join(pkgDir, "large.go"))

	outside := filepath.Join(t.TempDir(), "outside.go")
	writeProxyOutputFile(t, outside, "package outside\n\nfunc Outside() {}\n")
	if err := os.Symlink(outside, filepath.Join(pkgDir, "link.go")); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	files, err := directDirectoryAnatomyFiles(
		context.Background(),
		repo,
		"pkg",
		agentproxy.DirectoryListingInvocation{},
	)
	if err != nil {
		t.Fatalf("build direct anatomy: %v", err)
	}

	if len(files) != 1 || files[0].Path != "pkg/safe.go" {
		t.Fatalf("unexpected direct anatomy files: %#v", files)
	}
}

func TestDirectDirectoryTreeAnatomyLimitsFilesAndSkipsSymlinks(t *testing.T) {
	repo := initProxyOutputRepo(t)
	pkgDir := filepath.Join(repo, "pkg")
	if err := os.MkdirAll(pkgDir, 0o700); err != nil {
		t.Fatalf("create package dir: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.go")
	writeProxyOutputFile(t, outside, "package outside\n\nfunc Outside() {}\n")
	if err := os.Symlink(outside, filepath.Join(pkgDir, "link.go")); err != nil {
		t.Skipf("create symlink: %v", err)
	}

	for index := range maxDirectAnatomyFiles + 5 {
		writeProxyOutputFile(
			t,
			filepath.Join(pkgDir, fmt.Sprintf("file_%03d.go", index)),
			fmt.Sprintf("package pkg\n\nfunc File%d() {}\n", index),
		)
	}

	files, err := directDirectoryAnatomyFiles(
		context.Background(),
		repo,
		"pkg",
		agentproxy.DirectoryListingInvocation{
			Recursive: true,
			MaxDepth:  2,
		},
	)
	if err != nil {
		t.Fatalf("build recursive direct anatomy: %v", err)
	}

	if len(files) != maxDirectAnatomyFiles {
		t.Fatalf(
			"recursive direct anatomy file count = %d, want %d",
			len(files),
			maxDirectAnatomyFiles,
		)
	}

	for _, file := range files {
		if file.Path == "pkg/link.go" {
			t.Fatalf("recursive direct anatomy included symlink: %#v", files)
		}
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

func TestProxyPostToolOutputCollapsesSavedOutputNotice(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		"Error: result (90,668 characters across 1,795 lines) exceeds maximum allowed tokens. Output has been saved to",
		"     /saved-output/tool-results/mcp-gmeow-gmail_get_message-1779695312898.txt.",
		"     Format: Plain text",
		"     Use offset and limit parameters to read specific portions of the file.",
		"     REQUIREMENTS FOR SUMMARIZATION/ANALYSIS/REVIEW:",
		"     - You MUST read the content from the file in sequential chunks until 100%",
		"       of the content has been read.",
	}, "\n")

	proxied := proxyPostToolOutput(
		Event{
			SessionID: "session-saved-output-notice",
			ToolName:  toolBash,
			Cwd:       t.TempDir(),
			ToolInput: map[string]any{
				"command": "gmeow gmail_get_message",
			},
			ToolResponse: map[string]any{
				"stderr":      output,
				"return_code": 1,
			},
		},
		output,
	)

	expected := "Error: result (90,668 characters across 1,795 lines) " +
		"exceeds maximum allowed tokens; full output saved to " +
		"/saved-output/tool-results/mcp-gmeow-gmail_get_message-1779695312898.txt."
	if proxied.Text != expected {
		t.Fatalf("proxied saved-output notice = %q", proxied.Text)
	}

	if strings.Contains(proxied.Text, "REQUIREMENTS FOR SUMMARIZATION") ||
		strings.Contains(proxied.Text, "You MUST read") {
		t.Fatalf("proxied output retained verbose instructions: %s", proxied.Text)
	}

	if len(proxied.Records) == 0 ||
		proxied.Records[0].EvidencePath != "/saved-output/tool-results/mcp-gmeow-gmail_get_message-1779695312898.txt" ||
		proxied.Metadata["coding_ethos.full_output_path"] != proxied.Records[0].EvidencePath {
		t.Fatalf("saved-output evidence missing: records=%#v metadata=%#v",
			proxied.Records,
			proxied.Metadata,
		)
	}
}

func TestProxyPostToolOutputAppliesTokenBudgetAfterFilePagination(t *testing.T) {
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_MAX_TOKENS", "256")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_HEAD_TOKENS", "64")
	t.Setenv("CODE_ETHOS_PROXY_OUTPUT_TAIL_TOKENS", "64")

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
		!strings.Contains(proxied.Text, "token_budget: status=truncated") {
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
					record.EvidencePath != paginationEvidence ||
					record.OutputTokens > 256 {
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

func TestResolveHookTokenBudgetHonorsTrustedContractContextWindow(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"contract_version":"coding-ethos.hook/v1",
		"correlation_id":"trusted-context-window",
		"provider":"coding-ethos",
		"hook_event_name":"PostToolUse",
		"tool_name":"Bash",
		"tool_input":{"command":"go test ./internal/hooks"},
		"context_window_tokens":262144
	}`))
	if err != nil {
		t.Fatalf("decode trusted hook event: %v", err)
	}

	resolution := resolveHookTokenBudget(
		event,
		defaultHookOutputCompressionOptions(),
	)
	if resolution.Source != tokenBudgetSourceModelContext ||
		resolution.ContextWindowTokens != 262144 ||
		resolution.MaxTokens != tokenBudgetExtraLargeContext {
		t.Fatalf("trusted context resolution = %#v", resolution)
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

	if !strings.Contains(proxied.Text, "token_budget: status=truncated max_tokens=80") ||
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
		event.Payload.Bytes != len([]byte(output)) ||
		event.OutputHash != agentproxy.HashText(output) ||
		event.Metadata["coding_ethos.result.return_code"] != "0" ||
		event.Metadata["coding_ethos.result.return_code_known"] != "true" ||
		event.Metadata["coding_ethos.result.status"] != "succeeded" ||
		event.Metadata["coding_ethos.result.bytes"] != fmt.Sprint(len([]byte(output))) ||
		event.Metadata["coding_ethos.delivered.bytes"] !=
			fmt.Sprint(len([]byte(proxied.Text))) ||
		event.Metadata["coding_ethos.session_scope"] != "session-dense-output" ||
		event.Metadata["coding_ethos.token_budget.source"] != tokenBudgetSourceEnv ||
		event.Metadata["coding_ethos.token_budget.max_tokens"] != "80" ||
		event.Model != "test-model" {
		t.Fatalf("unexpected dense proxy ledger event: %#v", event)
	}
}

func TestProxyPostToolOutputRecordsFailedResultStatusAndOriginalBytes(t *testing.T) {
	repo := initProxyOutputRepo(t)
	output := "compiler failed\n"
	proxied := proxyPostToolOutput(
		Event{
			ProviderHint: "codex",
			SessionID:    "session-failed-output",
			ToolName:     toolBash,
			Cwd:          repo,
			ToolInput:    map[string]any{"command": "go test ./..."},
			ToolResponse: map[string]any{
				"stderr":      output,
				"return_code": 17,
			},
		},
		output,
	)

	if len(proxied.Events) != 1 {
		t.Fatalf("failed output events = %#v", proxied.Events)
	}
	event := proxied.Events[0]
	if event.Payload.Bytes != len(output) ||
		event.Metadata["coding_ethos.result.return_code"] != "17" ||
		event.Metadata["coding_ethos.result.status"] != "failed" {
		t.Fatalf("failed result evidence = %#v", event)
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

func writeProxyOutputFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create parent dir for %s: %v", path, err)
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeLargeProxyOutputFile(t *testing.T, path string) {
	t.Helper()

	writeProxyOutputFile(t, path, "package pkg\n\nfunc Large() {}\n")

	if err := os.Truncate(path, directAnatomyLargeFixtureBytes); err != nil {
		t.Fatalf("grow large fixture %s: %v", path, err)
	}
}
