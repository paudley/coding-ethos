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
