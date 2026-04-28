// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics_test

import (
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/diagnostics"
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
