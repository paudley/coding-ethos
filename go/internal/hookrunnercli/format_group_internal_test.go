// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

const persistExtraRecordsTestPath = "lib/python/tests/parsing/" +
	"test_persist_extra_records_integration.py"

func TestRuffAutofixTOONOmitsRawRenderedDetail(t *testing.T) {
	t.Parallel()

	report := formatHookReport(hookReport{
		Tool:  "ruff-autofix",
		Title: "RUFF-AUTOFIX FAILED",
		Findings: []hookFinding{ruffFinding(
			"lbox-platform/scripts/_corpus_enrichment_helpers.py",
			"TRY301",
			"Abstract `raise` to an inner function",
			664,
			17,
		)},
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

	if root != "." && strings.Contains(report, root) {
		t.Fatalf("TOON report leaked absolute repo root:\n%s", report)
	}

	if !strings.Contains(
		report,
		"ruff,"+persistExtraRecordsTestPath+",61,1,error,E402",
	) {
		t.Fatalf("TOON report missing normalized path:\n%s", report)
	}
}

func TestHookReportCanRenderSARIF(t *testing.T) {
	t.Parallel()

	report := formatHookReport(hookReport{
		Tool:  "manifest_validation",
		Title: "MANIFEST VALIDATION FAILED",
		Findings: []hookFinding{{
			Tool:     "manifest_validation",
			File:     "manifest.yaml",
			Line:     1,
			Severity: "error",
			Code:     "schema",
			Message:  "'version' must be a string",
		}},
	}, hookOutputFormatSARIF)

	for _, want := range []string{
		`"version": "2.1.0"`,
		`"id": "manifest.validation"`,
		`"artifactLocation"`,
		`"uri": "manifest.yaml"`,
		`"'version' must be a string"`,
	} {
		if !strings.Contains(report, want) {
			t.Fatalf("SARIF report missing %q:\n%s", want, report)
		}
	}
}

func TestHookReportIncludesTraceIDInAgentFormats(t *testing.T) {
	t.Parallel()

	report := hookReport{
		Tool:  "manifest_validation",
		Title: "MANIFEST VALIDATION FAILED",
		Findings: []hookFinding{{
			Tool:    "manifest_validation",
			File:    "manifest.yaml",
			Message: "'version' must be a string",
		}},
	}

	toon := formatHookReport(report, hookOutputFormatTOON)
	if !strings.Contains(toon, "trace_id: ") {
		t.Fatalf("TOON report missing trace_id:\n%s", toon)
	}

	if !strings.Contains(toon, "manifest.validation") {
		t.Fatalf("TOON report missing default policy id:\n%s", toon)
	}

	jsonOutput := formatHookReport(report, hookOutputFormatJSON)
	if !strings.Contains(jsonOutput, `"trace_id": `) {
		t.Fatalf("JSON report missing trace_id:\n%s", jsonOutput)
	}
}

func TestEmitHookReportWritesTrace(t *testing.T) {
	root := setupGitHookTestRepo(t)
	t.Setenv(consumerRootEnv, root)

	var output bytes.Buffer
	emitHookReport(&output, hookReport{
		Tool:  "manifest_validation",
		Title: "MANIFEST VALIDATION FAILED",
		Findings: []hookFinding{{
			File:    "manifest.yaml",
			Message: "'version' must be a string",
		}},
	}, hookOutputFormatTOON)

	if !strings.Contains(output.String(), "trace_id: ") {
		t.Fatalf("emitted report missing trace_id:\n%s", output.String())
	}

	matches, err := filepath.Glob(
		filepath.Join(root, ".coding-ethos", "lint-runs", "*.json"),
	)
	if err != nil {
		t.Fatalf("glob traces: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("trace files = %#v, want one", matches)
	}
}

type testHookDiagnosticProducer struct {
	report hookReport
}

func (producer testHookDiagnosticProducer) HookReport() hookReport {
	return producer.report
}

func TestEmitHookReportAcceptsDiagnosticProducer(t *testing.T) {
	root := setupGitHookTestRepo(t)
	t.Setenv(consumerRootEnv, root)

	var output bytes.Buffer
	emitHookReport(&output, testHookDiagnosticProducer{report: hookReport{
		Tool:  "manifest_validation",
		Title: "MANIFEST VALIDATION FAILED",
		Findings: []hookFinding{{
			File:    "manifest.yaml",
			Message: "'version' must be a string",
		}},
	}}, hookOutputFormatTOON)

	if !strings.Contains(output.String(), "manifest.validation") ||
		!strings.Contains(output.String(), "trace_id: ") {
		t.Fatalf("producer output missing normalized fields:\n%s", output.String())
	}
}

func TestRuffAutofixTOONKeepsMultilineMessagesSingleRow(t *testing.T) {
	t.Parallel()

	root := repoRoot()
	report := formatHookReport(hookReport{
		Tool:  "ruff-autofix",
		Title: "RUFF-AUTOFIX FAILED",
		Findings: []hookFinding{ruffFinding(
			root+"/"+persistExtraRecordsTestPath,
			"S608",
			"Possible SQL injection vector through\nstring-based query construction",
			400,
			29,
		)},
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
		Findings: []hookFinding{ruffFinding(
			root+"/"+persistExtraRecordsTestPath,
			"E402",
			"Module level import not at top of file",
			61,
			1,
		)},
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

	unwanted := []string{
		"duration_ms",
		"failed_groups",
		"commands[",
		"fix_first",
	}
	if root != "." {
		unwanted = append(unwanted, root)
	}

	for _, unwanted := range unwanted {
		if strings.Contains(transcript, unwanted) {
			t.Fatalf(
				"failed commit transcript contains noisy field %q:\n%s",
				unwanted,
				transcript,
			)
		}
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

func ruffFinding(file, code, message string, line, column int) hookFinding {
	finding := hookFinding{
		Tool:     "ruff",
		File:     file,
		Line:     line,
		Column:   column,
		Severity: "error",
		Code:     code,
		Message:  message,
	}
	switch code {
	case "E402":
		finding.PolicyID = "python.import_order"
		finding.Advice = "Move imports to the top of the module or split setup into a helper."
	case "S608":
		finding.PolicyID = "python.sql_safety"
		finding.Advice = "Use parameterized SQL or a reviewed central SQL helper."
	}

	return finding
}
