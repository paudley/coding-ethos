// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadHookLogSummaryReadsMetadataRuns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-a", "metadata.env"),
		"run_id=run-a\nstarted_at_utc=20260427T000000Z\nexit_code=0\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-b", "metadata.env"),
		"run_id=run-b\nstarted_at_utc=20260427T000001Z\nexit_code=1\n",
	)

	summary, err := loadHookLogSummary(root, hookOutputFormatTOON)
	if err != nil {
		t.Fatalf("loadHookLogSummary() returned error: %v", err)
	}

	if summary.Total != 2 || summary.Passed != 1 || summary.Failed != 1 {
		t.Fatalf("summary counts = %#v", summary)
	}

	output := formatHookLogSummary(summary)
	if !strings.Contains(output, "runs[2]{run_id,started_at_utc,exit_code}:") {
		t.Fatalf("TOON summary missing runs table:\n%s", output)
	}
}

func TestParseMetadataEnvTrimsQuotedValues(t *testing.T) {
	t.Parallel()

	values := parseMetadataEnv("run_id='abc'\nexit_code=\"1\"\n")
	if values["run_id"] != "abc" || values["exit_code"] != "1" {
		t.Fatalf("parseMetadataEnv() = %#v", values)
	}
}

func TestAnalyzeHookLogsRanksFailuresAndQualityIssues(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	repo := repoRoot()
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-a", "stdout.log"),
		`format: toon
tool: ruff-autofix
command: "/home/example/repo/git commit -m 'feat(test): subject\, with commas'"
findings[2]{tool,file,line,column,severity,code,policy_id,message,advice,detail}:
  ruff,lib/python/app.py,10,1,error,E402,python.import_order,Module level import not at top of file,Move imports to top,
  ruff,`+repo+`/lib/python/app.py,20,4,error,S608,python.sql_safety,Possible SQL injection vector through\nstring-based query construction,Use parameterized SQL,
raw_output[1]{line}:
  Error: noisy output
`,
	)
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-b", "stdout.log"),
		`format: toon
tool: ruff-autofix
findings[1]{tool,file,line,column,severity,code,policy_id,message,advice,detail}:
  ruff,lib/python/app.py,10,1,error,E402,python.import_order,Module level import not at top of file,Move imports to top,
`,
	)

	analysis, err := analyzeHookLogs(root, hookOutputFormatTOON)
	if err != nil {
		t.Fatalf("analyzeHookLogs() returned error: %v", err)
	}

	if analysis.RunsAnalyzed != 2 || analysis.Findings != 3 {
		t.Fatalf("analysis counts = %#v", analysis)
	}
	if len(analysis.TopTools) == 0 ||
		analysis.TopTools[0] != (hookLogCount{Key: "ruff", Count: 3}) {
		t.Fatalf("top tools = %#v", analysis.TopTools)
	}
	for _, count := range analysis.TopTools {
		if count.Key == "command" {
			t.Fatalf("command row was counted as a finding: %#v", analysis.TopTools)
		}
	}
	if len(analysis.TopCodes) == 0 ||
		analysis.TopCodes[0] != (hookLogCount{Key: "ruff:E402", Count: 2}) {
		t.Fatalf("top codes = %#v", analysis.TopCodes)
	}
	if len(analysis.Repeated) == 0 || analysis.Repeated[0].Count != 2 {
		t.Fatalf("repeated failures = %#v", analysis.Repeated)
	}

	kinds := map[string]bool{}
	for _, count := range analysis.QualityCounts {
		kinds[count.Key] = true
	}
	for _, want := range []string{
		"absolute_repo_path",
		"escaped_newline_cell",
		"raw_output",
	} {
		if !kinds[want] {
			t.Fatalf("missing quality issue %q in %#v", want, analysis.QualityIssues)
		}
	}

	output := formatHookLogAnalysis(analysis)
	for _, want := range []string{
		"top_tools[1]{key,count}:",
		"top_codes[2]{key,count}:",
		"quality_issue_counts[3]{key,count}:",
		"quality_issue_examples[4]{kind,run_id,line,sample}:",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("analysis output missing %q:\n%s", want, output)
		}
	}
}

func TestAnalyzeHookLogsCapsQualityIssueExamples(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lines := []string{"format: toon"}
	for range maxHookLogQualityIssueSamples + 5 {
		lines = append(lines, "command: /home/example/repo/git status")
	}
	mustWriteTestFile(
		t,
		filepath.Join(root, "run-a", "stdout.log"),
		strings.Join(lines, "\n"),
	)

	analysis, err := analyzeHookLogs(root, hookOutputFormatTOON)
	if err != nil {
		t.Fatalf("analyzeHookLogs() returned error: %v", err)
	}

	if analysis.QualityTotal != maxHookLogQualityIssueSamples+5 {
		t.Fatalf("quality total = %d", analysis.QualityTotal)
	}
	if len(analysis.QualityIssues) != maxHookLogQualityIssueSamples {
		t.Fatalf("quality examples = %d", len(analysis.QualityIssues))
	}
}

func TestSplitTOONCSVHandlesEscapedCommas(t *testing.T) {
	t.Parallel()

	cells := splitTOONCSV(`ruff,path.py,1,2,error,E402,,message with \, comma,advice,`)
	if len(cells) != 10 || cells[7] != "message with , comma" {
		t.Fatalf("splitTOONCSV() = %#v", cells)
	}
}
