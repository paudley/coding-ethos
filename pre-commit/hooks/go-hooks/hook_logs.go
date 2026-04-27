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
