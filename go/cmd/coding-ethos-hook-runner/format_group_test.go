// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const persistExtraRecordsTestPath = "lib/python/tests/parsing/" +
	"test_persist_extra_records_integration.py"

var errTestRunnerFailure = apperror.StaticError("test runner failure")

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
			t.Fatalf(
				"TOON report contains raw rendered detail %q:\n%s",
				unwanted,
				report,
			)
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

func TestParseGenericHookFindingsParsesGoTestWithoutPassingNoise(t *testing.T) {
	t.Parallel()

	findings := parseGenericHookFindings(
		"go-test",
		`{"Action":"pass","Package":"blackcat.ca/coding-ethos/go/cmd/`+
			`coding-ethos-hook","Elapsed":0.905}`+"\n"+
			`{"Action":"run","Package":"pkg/app","Test":"TestExample"}`+"\n"+
			`{"Action":"output","Package":"pkg/app","Test":"TestExample",`+
			`"Output":"    app_test.go:12: wanted true\n"}`+"\n"+
			`{"Action":"fail","Package":"pkg/app","Test":"TestExample",`+
			`"Elapsed":0.013}`+"\n",
	)

	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}

	if findings[0].File != "app_test.go" ||
		findings[0].Line != 12 ||
		findings[0].Code != "TestExample" ||
		findings[0].Message != "wanted true" {
		t.Fatalf("first finding = %#v", findings[0])
	}

	report := formatHookReport(hookReport{
		Tool:     "go-test",
		Title:    "GO-TEST FAILED",
		Findings: findings,
		Guidance: []string{"Fix the reported diagnostics before committing."},
	}, hookOutputFormatTOON)
	if strings.Contains(report, "raw_output") ||
		strings.Contains(report, "coding-ethos-hook") {
		t.Fatalf("go-test report kept passing noise:\n%s", report)
	}
}

func TestGenericToolFailureFindingReportsTimeout(t *testing.T) {
	t.Parallel()

	finding := genericToolFailureFindingForResult("shellcheck", externalToolResult{
		ExitCode: -1,
		TimedOut: true,
	})

	if finding.Code != timeoutCode ||
		finding.Message != "external tool timed out" ||
		finding.Detail == "" {
		t.Fatalf("timeout finding mismatch: %#v", finding)
	}
}

func TestReportSharedToolResultFormatsRunnerFailure(t *testing.T) {
	t.Parallel()

	output := formatSharedToolRunnerFailure(
		"yamllint",
		externalToolResult{RunnerFailure: errTestRunnerFailure, ExitCode: 1},
	)

	if !strings.Contains(output, "YAMLLINT RUNNER FAILED") ||
		!strings.Contains(output, "test runner failure") {
		t.Fatalf("runner failure output mismatch:\n%s", output)
	}
}
