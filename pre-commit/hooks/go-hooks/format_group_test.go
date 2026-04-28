// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestParseRuffAutofixFindingsUsesStructuredDiagnostics(t *testing.T) {
	t.Parallel()

	findings := parseRuffAutofixFindings(`[
  {
    "filename": "lbox-platform/scripts/_corpus_enrichment_helpers.py",
    "code": "TRY301",
    "message": "Abstract ` + "`raise`" + ` to an inner function",
    "location": {"row": 664, "column": 17}
  }
]`)

	if len(findings) != 1 {
		t.Fatalf("parseRuffAutofixFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.Tool != "ruff" ||
		got.File != "lbox-platform/scripts/_corpus_enrichment_helpers.py" ||
		got.Line != 664 ||
		got.Column != 17 ||
		got.Code != "TRY301" ||
		got.Message != "Abstract `raise` to an inner function" {
		t.Fatalf("finding mismatch: %#v", got)
	}
}

func TestRuffAutofixTOONOmitsRawRenderedDetail(t *testing.T) {
	t.Parallel()

	report := formatHookReport(hookReport{
		Tool:  "ruff-autofix",
		Title: "RUFF-AUTOFIX FAILED",
		Findings: parseRuffAutofixFindings(`[
  {
    "filename": "lbox-platform/scripts/_corpus_enrichment_helpers.py",
    "code": "TRY301",
    "message": "Abstract ` + "`raise`" + ` to an inner function",
    "location": {"row": 664, "column": 17}
  }
]`),
		Guidance: []string{"Fix the reported diagnostics before committing."},
	}, hookOutputFormatTOON)

	for _, want := range []string{
		"ruff,lbox-platform/scripts/_corpus_enrichment_helpers.py,664,17,error,TRY301",
		"Abstract `raise` to an inner function",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("TOON report missing %q:\n%s", want, report)
		}
	}

	for _, unwanted := range []string{
		"-->",
		"external tool exited with status",
		"^",
		"|",
	} {
		if strings.Contains(report, unwanted) {
			t.Fatalf("TOON report contains raw rendered detail %q:\n%s", unwanted, report)
		}
	}
}

func TestParseGenericHookFindingsUsesFallbackDiagnostics(t *testing.T) {
	t.Parallel()

	findings := parseGenericHookFindings(
		"gofmt",
		"pkg/app.go:8:2: expected declaration, found '}'",
	)
	if len(findings) != 1 {
		t.Fatalf("parseGenericHookFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.Tool != "gofmt" ||
		got.File != "pkg/app.go" ||
		got.Line != 8 ||
		got.Column != 2 ||
		got.Message != "expected declaration, found '}'" {
		t.Fatalf("finding mismatch: %#v", got)
	}
}

func TestHookReportTOONKeepsRawOutputOutsideFindingsTable(t *testing.T) {
	t.Parallel()

	report := formatHookReport(hookReport{
		Tool:  "go-test",
		Title: "GO-TEST FAILED",
		Findings: []hookFinding{genericToolFailureFinding(
			"go-test",
			1,
		)},
		RawOutput: boundedRawOutputLines(`--- FAIL: TestExample (0.00s)
    app_test.go:12: wanted true
FAIL
FAIL pkg/app 0.013s`),
		Guidance: []string{"Fix the reported diagnostics before committing."},
	}, hookOutputFormatTOON)

	if !strings.Contains(report, "raw_output[4]{line}:") ||
		!strings.Contains(report, "external tool exited with status 1") {
		t.Fatalf("TOON report missing expected failure shape:\n%s", report)
	}

	findingsLine := ""
	for _, line := range strings.Split(report, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "go-test,,0,0,error") {
			findingsLine = line
		}
	}
	if findingsLine == "" {
		t.Fatalf("TOON report missing generic finding:\n%s", report)
	}

	for _, unwanted := range []string{"--- FAIL", "app_test.go:12", "FAIL pkg/app"} {
		if strings.Contains(findingsLine, unwanted) {
			t.Fatalf("finding row contains raw output %q:\n%s", unwanted, report)
		}
	}
}
