// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type hookLogRun struct {
	RunID        string `json:"run_id"`
	StartedAtUTC string `json:"started_at_utc,omitempty"`
	ExitCode     int    `json:"exit_code"`
}

type hookLogSummary struct {
	Format string       `json:"format"`
	Path   string       `json:"path"`
	Runs   []hookLogRun `json:"runs"`
	Total  int          `json:"total"`
	Failed int          `json:"failed"`
	Passed int          `json:"passed"`
}

type hookLogAnalysis struct {
	Format        string         `json:"format"`
	Path          string         `json:"path"`
	TopTools      []hookLogCount `json:"top_tools"`
	TopCodes      []hookLogCount `json:"top_codes"`
	Repeated      []hookLogCount `json:"repeated_failures"`
	QualityIssues []hookLogIssue `json:"quality_issues"`
	RunsAnalyzed  int            `json:"runs_analyzed"`
	Findings      int            `json:"findings"`
}

type hookLogCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type hookLogIssue struct {
	RunID  string `json:"run_id"`
	Kind   string `json:"kind"`
	Line   int    `json:"line"`
	Sample string `json:"sample"`
}

func hookLogSummaryCommand(_ Config, args []string) int {
	logRoot := repoPath(filepath.Join(".coding-ethos", "hook-runs"))
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		logRoot = args[0]
	}

	summary, err := loadHookLogSummary(logRoot, selectedHookOutputFormat())
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	fmt.Fprintln(os.Stdout, formatHookLogSummary(summary))

	return 0
}

func hookLogAnalyzeCommand(_ Config, args []string) int {
	logRoot := repoPath(filepath.Join(".coding-ethos", "hook-runs"))
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		logRoot = args[0]
	}

	analysis, err := analyzeHookLogs(logRoot, selectedHookOutputFormat())
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	fmt.Fprintln(os.Stdout, formatHookLogAnalysis(analysis))

	return 0
}

func loadHookLogSummary(path string, format string) (hookLogSummary, error) {
	summary := hookLogSummary{
		Format: format,
		Path:   path,
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return summary, fmt.Errorf("read hook log dir %q: %w", path, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		run, err := loadHookLogRun(filepath.Join(path, entry.Name(), "metadata.env"))
		if err != nil {
			continue
		}

		if run.RunID == "" {
			run.RunID = entry.Name()
		}

		summary.Runs = append(summary.Runs, run)
	}

	sort.Slice(summary.Runs, func(left int, right int) bool {
		return summary.Runs[left].RunID < summary.Runs[right].RunID
	})

	summary.Total = len(summary.Runs)
	for _, run := range summary.Runs {
		if run.ExitCode == 0 {
			summary.Passed++
		} else {
			summary.Failed++
		}
	}

	return summary, nil
}

func analyzeHookLogs(path string, format string) (hookLogAnalysis, error) {
	analysis := hookLogAnalysis{
		Format: format,
		Path:   path,
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return analysis, fmt.Errorf("read hook log dir %q: %w", path, err)
	}

	toolCounts := map[string]int{}
	codeCounts := map[string]int{}
	repeatedCounts := map[string]int{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		runID := entry.Name()
		stdoutPath := filepath.Join(path, runID, "stdout.log")
		content, err := os.ReadFile(filepath.Clean(stdoutPath))
		if err != nil {
			continue
		}

		analysis.RunsAnalyzed++
		analyzeHookLogOutput(
			runID,
			string(content),
			&analysis,
			toolCounts,
			codeCounts,
			repeatedCounts,
		)
	}

	analysis.TopTools = topHookLogCounts(toolCounts, 10)
	analysis.TopCodes = topHookLogCounts(codeCounts, 10)
	analysis.Repeated = topHookLogCounts(repeatedCounts, 10)

	return analysis, nil
}

func analyzeHookLogOutput(
	runID string,
	output string,
	analysis *hookLogAnalysis,
	toolCounts map[string]int,
	codeCounts map[string]int,
	repeatedCounts map[string]int,
) {
	for lineNumber, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "  ") && strings.Count(line, ",") >= 8 {
			finding := parseHookLogFindingRow(line)
			if finding.Tool != "" {
				analysis.Findings++
				toolCounts[finding.Tool]++
			}
			if finding.Code != "" {
				codeCounts[finding.Tool+":"+finding.Code]++
			}
			key := hookLogFindingKey(finding)
			if key != "" {
				repeatedCounts[key]++
			}
		}

		analysis.QualityIssues = append(
			analysis.QualityIssues,
			hookLogQualityIssues(runID, lineNumber+1, line)...,
		)
	}
}

func parseHookLogFindingRow(line string) hookFinding {
	cells := splitTOONCSV(strings.TrimSpace(line))
	if len(cells) < 10 {
		return hookFinding{}
	}

	lineNumber, _ := strconv.Atoi(cells[2])
	column, _ := strconv.Atoi(cells[3])

	return hookFinding{
		Tool:     cells[0],
		File:     cells[1],
		Line:     lineNumber,
		Column:   column,
		Severity: cells[4],
		Code:     cells[5],
		PolicyID: cells[6],
		Message:  cells[7],
		Advice:   cells[8],
		Detail:   cells[9],
	}
}

func splitTOONCSV(line string) []string {
	cells := []string{}
	var current strings.Builder
	escaped := false
	for _, char := range line {
		if escaped {
			current.WriteRune(char)
			escaped = false

			continue
		}
		if char == '\\' {
			escaped = true

			continue
		}
		if char == ',' {
			cells = append(cells, current.String())
			current.Reset()

			continue
		}

		current.WriteRune(char)
	}
	cells = append(cells, current.String())

	return cells
}

func hookLogFindingKey(finding hookFinding) string {
	if finding.Tool == "" || finding.File == "" && finding.Code == "" && finding.Message == "" {
		return ""
	}

	return strings.Join([]string{
		finding.Tool,
		finding.File,
		strconv.Itoa(finding.Line),
		finding.Code,
		finding.Message,
	}, "|")
}

func hookLogQualityIssues(runID string, lineNumber int, line string) []hookLogIssue {
	issues := []hookLogIssue{}
	if strings.Contains(line, repoRoot()) {
		issues = append(issues, hookLogIssue{
			RunID:  runID,
			Kind:   "absolute_repo_path",
			Line:   lineNumber,
			Sample: truncateHookLogSample(line),
		})
	}
	if strings.Contains(line, "\\n") {
		issues = append(issues, hookLogIssue{
			RunID:  runID,
			Kind:   "escaped_newline_cell",
			Line:   lineNumber,
			Sample: truncateHookLogSample(line),
		})
	}
	if strings.HasPrefix(line, "raw_output[") {
		issues = append(issues, hookLogIssue{
			RunID:  runID,
			Kind:   "raw_output",
			Line:   lineNumber,
			Sample: truncateHookLogSample(line),
		})
	}

	return issues
}

func truncateHookLogSample(line string) string {
	const maxHookLogSampleLength = 180
	line = strings.TrimSpace(line)
	if len(line) <= maxHookLogSampleLength {
		return line
	}

	return line[:maxHookLogSampleLength] + " [truncated]"
}

func topHookLogCounts(counts map[string]int, limit int) []hookLogCount {
	items := make([]hookLogCount, 0, len(counts))
	for key, count := range counts {
		items = append(items, hookLogCount{Key: key, Count: count})
	}

	sort.Slice(items, func(left int, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}

		return items[left].Key < items[right].Key
	})

	if len(items) > limit {
		return items[:limit]
	}

	return items
}

func loadHookLogRun(path string) (hookLogRun, error) {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return hookLogRun{}, fmt.Errorf("read hook run metadata %q: %w", path, err)
	}

	values := parseMetadataEnv(string(content))

	exitCode, err := strconv.Atoi(values["exit_code"])
	if err != nil {
		return hookLogRun{}, fmt.Errorf("parse hook run exit code %q: %w", path, err)
	}

	return hookLogRun{
		RunID:        values["run_id"],
		StartedAtUTC: values["started_at_utc"],
		ExitCode:     exitCode,
	}, nil
}

func parseMetadataEnv(content string) map[string]string {
	values := map[string]string{}

	for line := range strings.SplitSeq(content, "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}

		values[strings.TrimSpace(key)] = trimShellQuotedValue(value)
	}

	return values
}

func trimShellQuotedValue(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.TrimPrefix(trimmed, "$'")
	trimmed = strings.Trim(trimmed, "'")
	trimmed = strings.Trim(trimmed, `"`)

	return trimmed
}

func formatHookLogAnalysis(analysis hookLogAnalysis) string {
	switch analysis.Format {
	case hookOutputFormatJSON:
		content, err := json.MarshalIndent(analysis, "", "  ")
		if err == nil {
			return string(content)
		}
	case hookOutputFormatTOON:
		lines := []string{
			"format: toon",
			"path: " + toonCell(analysis.Path),
			fmt.Sprintf("runs_analyzed: %d", analysis.RunsAnalyzed),
			fmt.Sprintf("findings: %d", analysis.Findings),
		}
		lines = append(lines, formatHookLogCountTable("top_tools", analysis.TopTools)...)
		lines = append(lines, formatHookLogCountTable("top_codes", analysis.TopCodes)...)
		lines = append(lines, formatHookLogCountTable("repeated_failures", analysis.Repeated)...)
		if len(analysis.QualityIssues) > 0 {
			lines = append(
				lines,
				fmt.Sprintf("quality_issues[%d]{kind,run_id,line,sample}:",
					len(analysis.QualityIssues),
				),
			)
			for _, issue := range analysis.QualityIssues {
				lines = append(lines, fmt.Sprintf(
					"  %s,%s,%d,%s",
					toonCell(issue.Kind),
					toonCell(issue.RunID),
					issue.Line,
					toonCell(issue.Sample),
				))
			}
		}

		return strings.Join(lines, "\n")
	}

	lines := []string{
		"Hook log analysis: " + analysis.Path,
		fmt.Sprintf("Runs analyzed: %d", analysis.RunsAnalyzed),
		fmt.Sprintf("Findings: %d", analysis.Findings),
		"Top tools: " + hookLogCountsHuman(analysis.TopTools),
		"Top codes: " + hookLogCountsHuman(analysis.TopCodes),
		"Repeated failures: " + hookLogCountsHuman(analysis.Repeated),
		fmt.Sprintf("Quality issues: %d", len(analysis.QualityIssues)),
	}

	return strings.Join(lines, "\n")
}

func formatHookLogCountTable(name string, counts []hookLogCount) []string {
	lines := []string{fmt.Sprintf("%s[%d]{key,count}:", name, len(counts))}
	for _, item := range counts {
		lines = append(lines, fmt.Sprintf("  %s,%d", toonCell(item.Key), item.Count))
	}

	return lines
}

func hookLogCountsHuman(counts []hookLogCount) string {
	if len(counts) == 0 {
		return "none"
	}

	items := make([]string, 0, len(counts))
	for _, count := range counts {
		items = append(items, fmt.Sprintf("%s=%d", count.Key, count.Count))
	}

	return strings.Join(items, ", ")
}

func formatHookLogSummary(summary hookLogSummary) string {
	switch summary.Format {
	case hookOutputFormatJSON:
		content, err := json.MarshalIndent(summary, "", "  ")
		if err == nil {
			return string(content)
		}
	case hookOutputFormatTOON:
		lines := []string{
			"format: toon",
			"path: " + toonCell(summary.Path),
			fmt.Sprintf("total: %d", summary.Total),
			fmt.Sprintf("passed: %d", summary.Passed),
			fmt.Sprintf("failed: %d", summary.Failed),
			fmt.Sprintf("runs[%d]{run_id,started_at_utc,exit_code}:", len(summary.Runs)),
		}
		for _, run := range summary.Runs {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s,%s,%d",
					toonCell(run.RunID),
					toonCell(run.StartedAtUTC),
					run.ExitCode,
				),
			)
		}

		return strings.Join(lines, "\n")
	}

	lines := []string{
		"Hook log summary: " + summary.Path,
		fmt.Sprintf(
			"Summary: %d passed, %d failed, %d total",
			summary.Passed,
			summary.Failed,
			summary.Total,
		),
	}
	for _, run := range summary.Runs {
		lines = append(
			lines,
			fmt.Sprintf("- %s exit=%d started=%s", run.RunID, run.ExitCode, run.StartedAtUTC),
		)
	}

	return strings.Join(lines, "\n")
}
