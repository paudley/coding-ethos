// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

const persistExtraRecordsTestPath = "lib/python/tests/parsing/" +
	"test_persist_extra_records_integration.py"

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

func TestHookReportNormalizesAbsoluteRepoPaths(t *testing.T) {
	t.Parallel()

	root := repoRoot()
	report := formatHookReport(hookReport{
		Tool:  "ruff-autofix",
		Title: "RUFF-AUTOFIX FAILED",
		Findings: []hookFinding{{
			Tool:     "ruff",
			File:     root + "/" + persistExtraRecordsTestPath,
			Line:     61,
			Column:   1,
			Severity: "error",
			Code:     "E402",
			Message:  "Module level import not at top of file",
		}},
	}, hookOutputFormatTOON)

	if strings.Contains(report, root) {
		t.Fatalf("TOON report leaked absolute repo root:\n%s", report)
	}

	if !strings.Contains(
		report,
		"ruff,"+persistExtraRecordsTestPath+",61,1,error,E402",
	) {
		t.Fatalf("TOON report missing normalized path:\n%s", report)
	}
}

func TestRuffAutofixTOONKeepsMultilineMessagesSingleRow(t *testing.T) {
	t.Parallel()

	root := repoRoot()
	report := formatHookReport(hookReport{
		Tool:  "ruff-autofix",
		Title: "RUFF-AUTOFIX FAILED",
		Findings: parseRuffAutofixFindings(`[
  {
    "filename": "` + root + `/` + persistExtraRecordsTestPath + `",
    "code": "S608",
    "message": "Possible SQL injection vector through\nstring-based query construction",
    "location": {"row": 400, "column": 29}
  }
]`),
		Guidance: []string{"Fix the reported diagnostics before committing."},
	}, hookOutputFormatTOON)

	for _, want := range []string{
		"ruff," + persistExtraRecordsTestPath +
			",400,29,error,S608,python.sql_safety",
		"Possible SQL injection vector through string-based query construction",
		"Use parameterized SQL or a reviewed central SQL helper.",
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("TOON report missing %q:\n%s", want, report)
		}
	}

	for line := range strings.SplitSeq(report, "\n") {
		if strings.Contains(line, "string-based query construction") &&
			!strings.Contains(
				line,
				"Possible SQL injection vector through string-based query construction",
			) {
			t.Fatalf("TOON finding split multiline message across rows:\n%s", report)
		}
	}
}

func TestFailedCommitTranscriptStaysCompactAndRelative(t *testing.T) {
	t.Parallel()

	root := repoRoot()
	toolReport := formatHookReport(hookReport{
		Tool:  "ruff-autofix",
		Title: "RUFF-AUTOFIX FAILED",
		Findings: parseRuffAutofixFindings(`[
  {
    "filename": "` + root + `/` + persistExtraRecordsTestPath + `",
    "code": "E402",
    "message": "Module level import not at top of file",
    "location": {"row": 61, "column": 1}
  }
]`),
		Guidance: []string{"Fix the reported diagnostics before committing."},
	}, hookOutputFormatTOON)
	summary := formatHookExecutionSummary([]hookGroupResult{{
		Name:     "format",
		Status:   statusFail,
		ExitCode: 1,
		Commands: []hookCommandResult{{
			Name:     "ruff-autofix",
			Status:   statusFail,
			ExitCode: 1,
		}},
	}}, hookOutputFormatTOON)
	transcript := toolReport + "\n" + summary

	for _, want := range []string{
		"python.import_order",
		"Move imports to the top of the module or split setup into a helper.",
		"failed_checks[1]{name,status}:",
		"next[1]{action}:",
	} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("failed commit transcript missing %q:\n%s", want, transcript)
		}
	}

	for _, unwanted := range []string{
		root,
		"duration_ms",
		"failed_groups",
		"commands[",
		"fix_first",
	} {
		if strings.Contains(transcript, unwanted) {
			t.Fatalf(
				"failed commit transcript contains noisy field %q:\n%s",
				unwanted,
				transcript,
			)
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
		RawOutput: boundedRawOutputLines(
			"--- FAIL: TestExample (0.00s)\n" +
				"    app_test.go:12: wanted true\nFAIL\npkg/app 0.013s",
		),
		Guidance: []string{"Fix the reported diagnostics before committing."},
	}, hookOutputFormatTOON)

	if !strings.Contains(report, "raw_output[4]{line}:") ||
		!strings.Contains(report, "external tool exited with status 1") {
		t.Fatalf("TOON report missing expected failure shape:\n%s", report)
	}

	findingsLine := ""

	for line := range strings.SplitSeq(report, "\n") {
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
