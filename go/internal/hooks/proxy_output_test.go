// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

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
