// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics_test

import (
	"reflect"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

func TestParseRuffDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		ruffFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "pkg/app.py",
		Line:     4,
		Column:   8,
		Severity: "error",
		Code:     "F401",
		Message:  "unused import",
	})
}

func TestParseRuffTextDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		"pkg/app.py:4:8: F401 unused import\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "pkg/app.py",
		Line:     4,
		Column:   8,
		Severity: "error",
		Code:     "F401",
		Message:  "unused import",
	})
}

func TestParseRuffFormatDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"ruff",
		"Would reformat: lib/python/pkg.py\n1 file would be reformatted\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "ruff",
		File:     "lib/python/pkg.py",
		Line:     1,
		Column:   1,
		Severity: "error",
		Code:     "format",
		Message:  "File would be reformatted by ruff format.",
	})
}

func TestParsePyrightDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"pyright",
		pyrightFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "pyright",
		File:     "pkg/app.py",
		Line:     5,
		Column:   3,
		Severity: "error",
		Code:     "reportAssignmentType",
		Message:  "bad type",
	})
}

func TestParseMypyJSONLinesDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"mypy",
		mypyFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "mypy",
		File:     "pkg/app.py",
		Line:     88,
		Column:   12,
		Severity: "error",
		Code:     "no-any-return",
		Message:  "Returning Any",
	})
}

func TestParseMypyTextDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"mypy",
		"pkg/app.py:88:12: error: Returning Any [no-any-return]\n",
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "mypy",
		File:     "pkg/app.py",
		Line:     88,
		Column:   12,
		Severity: "error",
		Code:     "no-any-return",
		Message:  "Returning Any",
	})
}

func TestParsePylintJSON2Diagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"pylint",
		`{"messages":[{"path":"pkg/app.py","line":7,"column":4,`+
			`"type":"warning","symbol":"unused-import",`+
			`"messageId":"W0611","message":"Unused import os"}]}`,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "pylint",
		File:     "pkg/app.py",
		Line:     7,
		Column:   5,
		Severity: "warning",
		Code:     "unused-import",
		Message:  "Unused import os",
	})
}

func TestParseTypeCheckerPolicyCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tool   string
		output string
		want   diagnostics.Diagnostic
	}{
		{
			name: "pyright optional member",
			tool: "pyright",
			output: `{"generalDiagnostics":[{"file":"pkg/app.py","severity":"error",` +
				`"message":"\"run\" is not a known attribute of \"None\"",` +
				`"rule":"reportOptionalMemberAccess",` +
				`"range":{"start":{"line":10,"character":6}}}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "pyright",
				File:     "pkg/app.py",
				Line:     11,
				Column:   7,
				Severity: "error",
				Code:     "reportOptionalMemberAccess",
				Message:  `"run" is not a known attribute of "None"`,
			},
		},
		{
			name:   "mypy no untyped def",
			tool:   "mypy",
			output: `pkg/app.py:12:1: error: Function is missing a type annotation [no-untyped-def]`,
			want: diagnostics.Diagnostic{
				Tool:     "mypy",
				File:     "pkg/app.py",
				Line:     12,
				Column:   1,
				Severity: "error",
				Code:     "no-untyped-def",
				Message:  "Function is missing a type annotation",
			},
		},
		{
			name: "pylint no member",
			tool: "pylint",
			output: `[{"path":"pkg/app.py","line":20,"column":10,` +
				`"type":"error","symbol":"no-member","messageId":"E1101",` +
				`"message":"Instance has no member \"run\""}]`,
			want: diagnostics.Diagnostic{
				Tool:     "pylint",
				File:     "pkg/app.py",
				Line:     20,
				Column:   11,
				Severity: "error",
				Code:     "no-member",
				Message:  `Instance has no member "run"`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertDiagnostic(t, diagnostics.Parse(test.tool, test.output, ""), test.want)
		})
	}
}

func TestParseGolangciLintDiagnostics(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"golangci-lint",
		golangciFixture,
		"",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "golangci-lint",
		File:     "pkg/app.go",
		Line:     8,
		Column:   2,
		Severity: "error",
		Code:     "ineffassign",
		Message:  "ineffectual assignment to err",
	})
}

func TestParseToolchainDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tool   string
		output string
		want   diagnostics.Diagnostic
	}{
		{
			name: "hadolint-json",
			tool: "hadolint",
			output: `[{"file":"Dockerfile","line":3,"column":1,` +
				`"level":"warning","code":"DL3008",` +
				`"message":"Pin versions in apt get install."}]`,
			want: diagnostics.Diagnostic{
				Tool:     "hadolint",
				File:     "Dockerfile",
				Line:     3,
				Column:   1,
				Severity: "warning",
				Code:     "DL3008",
				Message:  "Pin versions in apt get install.",
			},
		},
		{
			name: "actionlint-jsonl",
			tool: "actionlint",
			output: `{"filepath":".github/workflows/ci.yml",` +
				`"line":12,"column":5,"kind":"syntax-check",` +
				`"message":"property \"run\" is not defined"}`,
			want: diagnostics.Diagnostic{
				Tool:     "actionlint",
				File:     ".github/workflows/ci.yml",
				Line:     12,
				Column:   5,
				Severity: "error",
				Code:     "syntax-check",
				Message:  `property "run" is not defined`,
			},
		},
		{
			name: "shellcheck-json",
			tool: "shellcheck",
			output: `{"comments":[{"file":"script.sh","line":3,` +
				`"column":7,"level":"warning","code":2086,` +
				`"message":"Double quote to prevent globbing and word splitting."}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "shellcheck",
				File:     "script.sh",
				Line:     3,
				Column:   7,
				Severity: "warning",
				Code:     "SC2086",
				Message:  "Double quote to prevent globbing and word splitting.",
			},
		},
		{
			name:   "yamllint-parsable",
			tool:   "yamllint",
			output: `config.yaml:2:5: [error] wrong indentation (indentation)`,
			want: diagnostics.Diagnostic{
				Tool:     "yamllint",
				File:     "config.yaml",
				Line:     2,
				Column:   5,
				Severity: "error",
				Code:     "indentation",
				Message:  "wrong indentation",
			},
		},
		{
			name: "bandit-json",
			tool: "bandit",
			output: `{"results":[{"filename":"pkg/app.py","line_number":10,` +
				`"issue_severity":"HIGH","issue_confidence":"HIGH",` +
				`"test_id":"B602","issue_text":"subprocess call with shell=True"}]}`,
			want: diagnostics.Diagnostic{
				Tool:     "bandit",
				File:     "pkg/app.py",
				Line:     10,
				Severity: "error",
				Code:     "B602",
				Message:  "subprocess call with shell=True",
			},
		},
		{
			name: "sqlfluff-json",
			tool: "sqlfluff",
			output: `[{"filepath":"queries/app.sql","violations":[{` +
				`"line_no":2,"line_pos":7,"code":"LT01",` +
				`"description":"Expected single whitespace."}]}]`,
			want: diagnostics.Diagnostic{
				Tool:     "sqlfluff",
				File:     "queries/app.sql",
				Line:     2,
				Column:   7,
				Severity: "error",
				Code:     "LT01",
				Message:  "Expected single whitespace.",
			},
		},
		{
			name: "tombi-text",
			tool: "tombi",
			output: "\x1b[1;31m  Error\x1b[0m: invalid key\n" +
				"    at config.toml:2:4\n",
			want: diagnostics.Diagnostic{
				Tool:     "tombi",
				File:     "config.toml",
				Line:     2,
				Column:   4,
				Severity: "error",
				Message:  "invalid key",
			},
		},
		{
			name:   "dotenv-linter-text",
			tool:   "dotenv-linter",
			output: `.env.example:3 LowercaseKey: The key should be uppercase`,
			want: diagnostics.Diagnostic{
				Tool:     "dotenv-linter",
				File:     ".env.example",
				Line:     3,
				Severity: "warning",
				Code:     "LowercaseKey",
				Message:  "The key should be uppercase",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertDiagnostic(t, diagnostics.Parse(test.tool, test.output, ""), test.want)
		})
	}
}

func TestParseTextToolDiagnostics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		tool   string
		output string
		want   diagnostics.Diagnostic
	}{
		{
			name:   "golangci-text",
			tool:   "golangci-lint",
			output: "pkg/app.go:8:2: unchecked error (errcheck)\n",
			want: diagnostics.Diagnostic{
				Tool:     "golangci-lint",
				File:     "pkg/app.go",
				Line:     8,
				Column:   2,
				Severity: "error",
				Code:     "errcheck",
				Message:  "unchecked error",
			},
		},
		{
			name:   "hadolint-text",
			tool:   "hadolint",
			output: "Dockerfile:3 DL3008 warning: Pin versions in apt get install.\n",
			want: diagnostics.Diagnostic{
				Tool:     "hadolint",
				File:     "Dockerfile",
				Line:     3,
				Severity: "warning",
				Code:     "DL3008",
				Message:  "Pin versions in apt get install.",
			},
		},
		{
			name:   "actionlint-text",
			tool:   "actionlint",
			output: ".github/workflows/ci.yml:12:5: property \"run\" is not defined [syntax-check]\n",
			want: diagnostics.Diagnostic{
				Tool:     "actionlint",
				File:     ".github/workflows/ci.yml",
				Line:     12,
				Column:   5,
				Severity: "error",
				Code:     "syntax-check",
				Message:  `property "run" is not defined`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assertDiagnostic(t, diagnostics.Parse(test.tool, test.output, ""), test.want)
		})
	}
}

func TestInferToolRecognizesCommonDirectTools(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"actionlint":    {"actionlint", "-format", "{{json .}}"},
		"bandit":        {"python", "-m", "bandit", "-r", "pkg"},
		"golangci-lint": {"golangci-lint", "run"},
		"hadolint":      {"/usr/local/bin/hadolint", "Dockerfile"},
		"mypy":          {"uv", "run", "mypy", "pkg"},
		"custom-tool":   {"custom-tool", "arg"},
		"yamllint":      {"yamllint", "."},
	}
	for want, argv := range tests {
		if got := diagnostics.InferTool(argv); got != want {
			t.Fatalf("InferTool(%#v) = %q, want %q", argv, got, want)
		}
	}
}

func TestFallbackParserUsesStderrWhenStdoutEmpty(t *testing.T) {
	t.Parallel()

	parsed := diagnostics.Parse(
		"unknown-tool",
		"",
		"pkg/app.py:10:2: warning: fix this",
	)

	assertDiagnostic(t, parsed, diagnostics.Diagnostic{
		Tool:     "unknown-tool",
		File:     "pkg/app.py",
		Line:     10,
		Column:   2,
		Severity: "warning",
		Message:  "fix this",
	})
}

func TestInferToolScansWrappedCommands(t *testing.T) {
	t.Parallel()

	tool := diagnostics.InferTool(
		[]string{"uv", "run", "--project", "/repo", "ruff", "check"},
	)
	if tool != "ruff" {
		t.Fatalf("InferTool() = %q, want ruff", tool)
	}
}

func TestEnrichMapsKnownDiagnosticEvidence(t *testing.T) {
	t.Parallel()

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{
			{Tool: "ruff", Code: "PLC" + "0415", Message: "import outside top-level"},
			{Tool: "ruff", Code: "F401", Message: "unused import"},
		},
		[]diagnostics.EvidenceMap{
			{
				Source:       "ruff",
				Codes:        []string{"PLC" + "0415"},
				PolicyID:     "python.conditional_imports",
				SkillID:      "conditional-imports",
				PrincipleIDs: []string{"no-conditional-imports"},
				Confidence:   "high",
				Meaning:      "import away from module scope",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Move required imports to module scope.",
					Steps:   []string{"Import at module scope."},
					Rerun:   []string{"make pre-commit"},
				},
			},
		},
	)

	if enriched[0].PolicyID != "python.conditional_imports" {
		t.Fatalf("mapped policy = %q", enriched[0].PolicyID)
	}

	if enriched[0].Advice != "Move required imports to module scope." {
		t.Fatalf("mapped advice = %q", enriched[0].Advice)
	}

	if enriched[0].SkillID != "conditional-imports" {
		t.Fatalf("mapped skill = %q", enriched[0].SkillID)
	}

	if len(enriched[0].PrincipleIDs) != 1 ||
		enriched[0].PrincipleIDs[0] != "no-conditional-imports" {
		t.Fatalf("mapped principles = %#v", enriched[0].PrincipleIDs)
	}

	if enriched[1].PolicyID != "" {
		t.Fatalf("unmapped policy = %q, want empty", enriched[1].PolicyID)
	}
}

func TestEnrichMapsWildcardDiagnosticEvidence(t *testing.T) {
	t.Parallel()

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{
			{Tool: "shellcheck", Code: "SC2086", Message: "quote expansion"},
		},
		[]diagnostics.EvidenceMap{
			{
				Source:   "shellcheck",
				Codes:    []string{"SC*"},
				PolicyID: "shell.static_analysis",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Fix shell diagnostics structurally.",
				},
			},
		},
	)

	if enriched[0].PolicyID != "shell.static_analysis" {
		t.Fatalf("wildcard policy = %q", enriched[0].PolicyID)
	}
}

func TestEnrichMapsMessageDiagnosticEvidence(t *testing.T) {
	t.Parallel()

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{
			{
				Tool:    "pyright",
				Message: "Import cycle detected between pkg.a and pkg.b",
			},
		},
		[]diagnostics.EvidenceMap{
			{
				Source: "pyright",
				MessageSubstrings: []string{
					"import cycle detected",
				},
				PolicyID: "python.import_cycles",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Break the concrete dependency cycle.",
				},
			},
		},
	)

	if enriched[0].PolicyID != "python.import_cycles" {
		t.Fatalf("message-mapped policy = %q", enriched[0].PolicyID)
	}
	if enriched[0].Advice != "Break the concrete dependency cycle." {
		t.Fatalf("message-mapped advice = %q", enriched[0].Advice)
	}
}

func TestDedupeMergesSamePolicyLocation(t *testing.T) {
	t.Parallel()

	deduped := diagnostics.Dedupe([]diagnostics.Diagnostic{
		{
			Tool:         "mypy",
			File:         "pkg/app.py",
			Line:         12,
			Column:       4,
			Code:         "union-attr",
			PolicyID:     "python.optional_required_types",
			Message:      "Item None has no attribute run",
			Advice:       "Make the required contract explicit.",
			PrincipleIDs: []string{"no-optional-types-for-required-dependencies"},
		},
		{
			Tool:         "pyright",
			File:         "pkg/app.py",
			Line:         12,
			Column:       4,
			Code:         "reportOptionalMemberAccess",
			PolicyID:     "python.optional_required_types",
			Message:      `"run" is not a known attribute of None`,
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		},
	})

	if len(deduped) != 1 {
		t.Fatalf("deduped = %#v", deduped)
	}
	if deduped[0].Tool != "mypy" ||
		!strings.Contains(deduped[0].Detail, "pyright:reportOptionalMemberAccess") {
		t.Fatalf("merged diagnostic = %#v", deduped[0])
	}
	if len(deduped[0].PrincipleIDs) != 2 {
		t.Fatalf("principles = %#v", deduped[0].PrincipleIDs)
	}
}

func assertDiagnostic(
	t *testing.T,
	parsed []diagnostics.Diagnostic,
	want diagnostics.Diagnostic,
) {
	t.Helper()

	if len(parsed) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(parsed), parsed)
	}

	got := parsed[0]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diagnostic = %#v, want %#v", got, want)
	}
}

const ruffFixture = `[
  {
    "filename": "pkg/app.py",
    "code": "F401",
    "message": "unused import",
    "location": {"row": 4, "column": 8}
  }
]`

const pyrightFixture = `{
  "generalDiagnostics": [
    {
      "file": "pkg/app.py",
      "severity": "error",
      "message": "bad type",
      "rule": "reportAssignmentType",
      "range": {"start": {"line": 4, "character": 2}}
    }
  ]
}`

const mypyFixture = `{"file":"pkg/app.py","line":88,"column":12,` +
	`"severity":"error","code":"no-any-return","message":"Returning Any"}`

const golangciFixture = `level=warning msg="runner warning"
{
  "Issues": [
    {
      "FromLinter": "ineffassign",
      "Text": "ineffectual assignment to err",
      "Severity": "error",
      "Pos": {"Filename": "pkg/app.go", "Line": 8, "Column": 2}
    }
  ]
}`
