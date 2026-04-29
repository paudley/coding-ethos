// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

func TestFormatLintResultTOONUsesDiagnostics(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "pii",
			File:     ".codex/config.toml",
			Line:     8,
			Severity: "block",
			PolicyID: "repo.pii_scrubber",
			Message:  "local machine detail detected",
			Advice:   "Replace local paths with generic placeholders.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"format: toon",
		"tool: policy-lint",
		"scope: staged",
		"findings[1]{tool,file,line,column,severity,code,policy_id,message,advice,detail}:",
		"pii,.codex/config.toml,8,0,block,,repo.pii_scrubber,local machine detail detected,Replace local paths with generic placeholders.,",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, `"decisions"`) || strings.Contains(output, "{\n") {
		t.Fatalf("TOON output looks like raw JSON:\n%s", output)
	}
}

func TestSelectedFormatAutoDetectsAgent(t *testing.T) {
	getenv := func(name string) string {
		switch name {
		case FormatEnv:
			return FormatAuto
		case "CODEX_THREAD_ID":
			return "thread"
		default:
			return ""
		}
	}

	if got := SelectedFormatWithEnv(getenv); got != FormatTOON {
		t.Fatalf("SelectedFormatWithEnv() = %q, want toon", got)
	}
}

func TestTOONCellEscapesCommasAndNewlines(t *testing.T) {
	t.Parallel()

	got := TOONCell("a,b\nc")
	if got != `a\,b\nc` {
		t.Fatalf("TOONCell() = %q", got)
	}
}
