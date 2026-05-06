// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const (
	maxHookLogAnalyzeRuns         = 250
	maxHookLogAnalyzeBytesPerRun  = 2 * 1024 * 1024
	maxHookLogQualityIssueSamples = 20
	hookLogFindingCellCount       = 10
	hookLogFindingCellCountSkill  = 11
	hookLogFindingColumns         = "{tool,file,line,column,severity,code," +
		"policy_id,skill_id,message,advice,detail}"
	hookLogTopCountLimit           = 10
	hookLogTruncatedOutputIssueKey = "truncated_output"
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
	QualityCounts []hookLogCount `json:"quality_issue_counts"`
	QualityIssues []hookLogIssue `json:"quality_issues"`
	RunsAnalyzed  int            `json:"runs_analyzed"`
	RunsAvailable int            `json:"runs_available"`
	RunsSkipped   int            `json:"runs_skipped"`
	Findings      int            `json:"findings"`
	QualityTotal  int            `json:"quality_issues_total"`
}

type hookLogCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

type hookLogIssue struct {
	RunID  string `json:"run_id"`
	Kind   string `json:"kind"`
	Sample string `json:"sample"`
	Line   int    `json:"line"`
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

func loadHookLogSummary(path, format string) (hookLogSummary, error) {
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

	sort.Slice(summary.Runs, func(left, right int) bool {
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

func analyzeHookLogs(path, format string) (hookLogAnalysis, error) {
	analysis := hookLogAnalysis{
		Format: format,
		Path:   path,
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return analysis, fmt.Errorf("read hook log dir %q: %w", path, err)
	}

	sort.Slice(entries, func(left, right int) bool {
		return entries[left].Name() > entries[right].Name()
	})

	toolCounts := map[string]int{}
	codeCounts := map[string]int{}
	repeatedCounts := map[string]int{}
	qualityCounts := map[string]int{}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		analysis.RunsAvailable++
		if analysis.RunsAnalyzed >= maxHookLogAnalyzeRuns {
			analysis.RunsSkipped++

			continue
		}

		runID := entry.Name()
		stdoutPath := filepath.Join(path, runID, "stdout.log")

		content, truncated, err := readHookLogOutput(stdoutPath)
		if err != nil {
			continue
		}

		analysis.RunsAnalyzed++
		analyzeHookLogOutput(
			runID,
			content,
			&analysis,
			toolCounts,
			codeCounts,
			repeatedCounts,
			qualityCounts,
		)

		if truncated {
			analysis.QualityTotal++

			qualityCounts[hookLogTruncatedOutputIssueKey]++
			if len(analysis.QualityIssues) < maxHookLogQualityIssueSamples {
				analysis.QualityIssues = append(analysis.QualityIssues, hookLogIssue{
					RunID:  runID,
					Kind:   hookLogTruncatedOutputIssueKey,
					Line:   0,
					Sample: fmt.Sprintf("stdout.log exceeded %d bytes", maxHookLogAnalyzeBytesPerRun),
				})
			}
		}
	}

	analysis.TopTools = topHookLogCounts(toolCounts)
	analysis.TopCodes = topHookLogCounts(codeCounts)
	analysis.Repeated = topHookLogCounts(repeatedCounts)
	analysis.QualityCounts = topHookLogCounts(qualityCounts)

	return analysis, nil
}

func readHookLogOutput(path string) (string, bool, error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", false, fmt.Errorf("open hook output %q: %w", path, err)
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, maxHookLogAnalyzeBytesPerRun+1))
	if err != nil {
		return "", false, fmt.Errorf("read hook output %q: %w", path, err)
	}

	truncated := len(content) > maxHookLogAnalyzeBytesPerRun
	if truncated {
		content = content[:maxHookLogAnalyzeBytesPerRun]
	}

	return string(content), truncated, nil
}

func analyzeHookLogOutput(
	runID string,
	output string,
	analysis *hookLogAnalysis,
	toolCounts map[string]int,
	codeCounts map[string]int,
	repeatedCounts map[string]int,
	qualityCounts map[string]int,
) {
	inFindingsTable := false
	currentTable := ""

	for lineNumber, line := range strings.Split(output, "\n") {
		if isHookLogFindingHeader(line) {
			inFindingsTable = true
			currentTable = "findings"

			continue
		}

		if tableName, ok := hookLogTableHeaderName(line); ok {
			inFindingsTable = false
			currentTable = tableName
		}

		if inFindingsTable && strings.HasPrefix(line, "  ") {
			processHookLogFinding(
				parseHookLogFindingRow(line),
				analysis,
				toolCounts,
				codeCounts,
				repeatedCounts,
			)
		}

		if !isHookLogQualityCandidate(currentTable, line) {
			continue
		}

		collectHookLogQualityIssues(
			hookLogQualityIssues(runID, lineNumber+1, line),
			analysis,
			qualityCounts,
		)
	}
}

func processHookLogFinding(
	finding hookFinding,
	analysis *hookLogAnalysis,
	toolCounts map[string]int,
	codeCounts map[string]int,
	repeatedCounts map[string]int,
) {
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

func collectHookLogQualityIssues(
	issues []hookLogIssue,
	analysis *hookLogAnalysis,
	qualityCounts map[string]int,
) {
	for _, issue := range issues {
		analysis.QualityTotal++

		qualityCounts[issue.Kind]++
		if len(analysis.QualityIssues) < maxHookLogQualityIssueSamples {
			analysis.QualityIssues = append(analysis.QualityIssues, issue)
		}
	}
}

func isHookLogFindingHeader(line string) bool {
	if line != strings.TrimSpace(line) {
		return false
	}

	trimmed := strings.TrimSpace(line)

	return strings.HasPrefix(trimmed, "findings[") &&
		strings.Contains(trimmed, hookLogFindingColumns)
}

func hookLogTableHeaderName(line string) (string, bool) {
	if line != strings.TrimSpace(line) {
		return "", false
	}

	trimmed := line
	if !strings.Contains(trimmed, "]{") || !strings.HasSuffix(trimmed, ":") {
		return "", false
	}

	tableName, _, ok := strings.Cut(trimmed, "[")
	if !ok {
		return "", false
	}

	return tableName, tableName != "findings"
}

func isHookLogQualityCandidate(currentTable, line string) bool {
	if currentTable == "quality_issue_examples" ||
		currentTable == "quality_issues" ||
		currentTable == "quality_issue_counts" {
		return false
	}

	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "path: ") {
		return false
	}

	if currentTable == "" && line != strings.TrimLeft(line, " \t") {
		return false
	}

	for _, prefix := range []string{
		"diff --git ",
		"index ",
		"@@",
		"--- ",
		"+++ ",
		"+",
		"-",
	} {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}

	return true
}

func parseHookLogFindingRow(line string) hookFinding {
	cells := splitTOONCSV(strings.TrimSpace(line))
	if len(cells) < hookLogFindingCellCount {
		return hookFinding{}
	}

	lineNumber, err := strconv.Atoi(cells[2])
	if err != nil {
		return hookFinding{}
	}

	column, err := strconv.Atoi(cells[3])
	if err != nil {
		return hookFinding{}
	}

	if len(cells) >= hookLogFindingCellCountSkill {
		return hookFinding{
			Tool:     cells[0],
			File:     cells[1],
			Line:     lineNumber,
			Column:   column,
			Severity: cells[4],
			Code:     cells[5],
			PolicyID: cells[6],
			SkillID:  cells[7],
			Message:  cells[8],
			Advice:   cells[9],
			Detail:   cells[10],
		}
	}

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
	if finding.Tool == "" ||
		finding.File == "" && finding.Code == "" && finding.Message == "" {
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
	if containsHookLogAbsolutePath(line) {
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

func containsHookLogAbsolutePath(line string) bool {
	if strings.Contains(line, repoRoot()) {
		return true
	}

	for _, marker := range []string{
		"/home/",
		"/Users/",
		"/var/folders/",
		"/tmp/",
		"/opt/",
	} {
		if strings.Contains(line, marker) {
			return true
		}
	}

	return false
}

func truncateHookLogSample(line string) string {
	const maxHookLogSampleLength = 180

	line = strings.TrimSpace(line)
	if len(line) <= maxHookLogSampleLength {
		return line
	}

	return line[:maxHookLogSampleLength] + " [truncated]"
}

func topHookLogCounts(counts map[string]int) []hookLogCount {
	items := make([]hookLogCount, 0, len(counts))
	for key, count := range counts {
		items = append(items, hookLogCount{Key: key, Count: count})
	}

	sort.Slice(items, func(left, right int) bool {
		if items[left].Count != items[right].Count {
			return items[left].Count > items[right].Count
		}

		return items[left].Key < items[right].Key
	})

	if len(items) > hookLogTopCountLimit {
		return items[:hookLogTopCountLimit]
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
			fmt.Sprintf("runs_available: %d", analysis.RunsAvailable),
			fmt.Sprintf("runs_analyzed: %d", analysis.RunsAnalyzed),
			fmt.Sprintf("runs_skipped: %d", analysis.RunsSkipped),
			fmt.Sprintf("findings: %d", analysis.Findings),
			fmt.Sprintf("quality_issues_total: %d", analysis.QualityTotal),
		}
		lines = append(lines, formatHookLogCountTable("top_tools", analysis.TopTools)...)
		lines = append(lines, formatHookLogCountTable("top_codes", analysis.TopCodes)...)
		lines = append(
			lines,
			formatHookLogCountTable("repeated_failures", analysis.Repeated)...,
		)

		lines = append(
			lines,
			formatHookLogCountTable("quality_issue_counts", analysis.QualityCounts)...,
		)
		if len(analysis.QualityIssues) > 0 {
			lines = append(
				lines,
				fmt.Sprintf("quality_issue_examples[%d]{kind,run_id,line,sample}:",
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
		fmt.Sprintf("Runs available: %d", analysis.RunsAvailable),
		fmt.Sprintf("Runs analyzed: %d", analysis.RunsAnalyzed),
		fmt.Sprintf("Runs skipped: %d", analysis.RunsSkipped),
		fmt.Sprintf("Findings: %d", analysis.Findings),
		"Top tools: " + hookLogCountsHuman(analysis.TopTools),
		"Top codes: " + hookLogCountsHuman(analysis.TopCodes),
		"Repeated failures: " + hookLogCountsHuman(analysis.Repeated),
		fmt.Sprintf("Quality issues: %d", analysis.QualityTotal),
		"Quality issue types: " + hookLogCountsHuman(analysis.QualityCounts),
	}

	return strings.Join(lines, "\n")
}

func formatHookLogCountTable(name string, counts []hookLogCount) []string {
	lines := make([]string, 1, 1+len(counts))

	lines[0] = fmt.Sprintf("%s[%d]{key,count}:", name, len(counts))
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
