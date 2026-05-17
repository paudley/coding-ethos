// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"encoding/json"
	"fmt"
	"strings"
)

type hookExecutionSummary struct {
	Format      string                   `json:"format"`
	Status      string                   `json:"status"`
	Summary     string                   `json:"summary"`
	Groups      []hookExecutionGroupJSON `json:"groups"`
	FailedFirst []string                 `json:"failed_first,omitempty"`
	DurationMS  float64                  `json:"duration_ms"`
	Passed      int                      `json:"passed"`
	Failed      int                      `json:"failed"`
}

type hookExecutionGroupJSON struct {
	Name       string                     `json:"name"`
	Status     string                     `json:"status"`
	Commands   []hookExecutionCommandJSON `json:"commands,omitempty"`
	DurationMS float64                    `json:"duration_ms"`
	ExitCode   int                        `json:"exit_code"`
}

type hookExecutionCommandJSON struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	ExitCode   int     `json:"exit_code"`
	DurationMS float64 `json:"duration_ms"`
}

func formatHookExecutionSummary(results []hookGroupResult, format string) string {
	summary := buildHookExecutionSummary(results)
	summary.Format = format

	switch format {
	case hookOutputFormatJSON:
		data, err := json.MarshalIndent(summary, "", "  ")
		if err != nil {
			return formatHookExecutionSummaryHuman(summary)
		}

		return string(data)
	case hookOutputFormatTOON:
		return formatHookExecutionSummaryTOON(summary)
	default:
		return formatHookExecutionSummaryHuman(summary)
	}
}

func buildHookExecutionSummary(results []hookGroupResult) hookExecutionSummary {
	summary := hookExecutionSummary{
		Status: statusPass,
		Groups: make(
			[]hookExecutionGroupJSON,
			0,
			len(results),
		),
	}

	for _, result := range results {
		summary.DurationMS += result.DurationMS

		if result.ExitCode == 0 && result.RunnerFailure == nil {
			summary.Passed++
		} else {
			summary.Failed++
			summary.Status = statusFail
			summary.FailedFirst = append(summary.FailedFirst, result.Name)
		}

		group := hookExecutionGroupJSON{
			Name:       result.Name,
			Status:     hookStatusForExitCode(result.ExitCode),
			ExitCode:   result.ExitCode,
			DurationMS: result.DurationMS,
			Commands:   hookExecutionCommandSummaries(result.Commands),
		}
		if result.RunnerFailure != nil {
			group.Status = statusFail
		}

		summary.Groups = append(summary.Groups, group)
	}

	summary.Summary = fmt.Sprintf(
		"%d passed, %d failed, %.0fms total group time",
		summary.Passed,
		summary.Failed,
		summary.DurationMS,
	)

	return summary
}

func hookExecutionCommandSummaries(
	commands []hookCommandResult,
) []hookExecutionCommandJSON {
	if len(commands) == 0 {
		return nil
	}

	summaries := make([]hookExecutionCommandJSON, 0, len(commands))
	for _, command := range commands {
		summaries = append(summaries, hookExecutionCommandJSON(command))
	}

	return summaries
}

func formatHookExecutionSummaryHuman(summary hookExecutionSummary) string {
	lines := []string{
		"",
		"HOOK EXECUTION SUMMARY",
		summary.Summary,
	}
	if len(summary.FailedFirst) > 0 {
		lines = append(
			lines,
			"fix_first: "+strings.Join(summary.FailedFirst, ", "),
		)
	}

	for _, group := range summary.Groups {
		lines = append(lines, fmt.Sprintf(
			"  %s: %s exit=%d duration_ms=%.0f",
			group.Name,
			group.Status,
			group.ExitCode,
			group.DurationMS,
		))
		for _, command := range group.Commands {
			lines = append(lines, fmt.Sprintf(
				"    %s: %s exit=%d duration_ms=%.0f",
				command.Name,
				command.Status,
				command.ExitCode,
				command.DurationMS,
			))
		}
	}

	return strings.Join(lines, "\n")
}

func formatHookExecutionSummaryTOON(summary hookExecutionSummary) string {
	lines := []string{
		"format: " + toonCell(summary.Format),
		"status: " + toonCell(summary.Status),
		fmt.Sprintf("failed: %d", summary.Failed),
	}

	failedCommands := failedExecutionCommands(summary.Groups)

	if len(failedCommands) > 0 {
		lines = append(
			lines,
			fmt.Sprintf(
				"failed_checks[%d]{name,status}:",
				len(failedCommands),
			),
		)

		for _, command := range failedCommands {
			lines = append(lines, fmt.Sprintf(
				"  %s,%s",
				toonCell(command.Name),
				toonCell(command.Status),
			))
		}
	}

	if len(failedCommands) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("next[%d]{action}:", len(failedCommands)),
		)
		for _, command := range failedCommands {
			lines = append(lines, "  "+toonCell(
				"Fix "+command.Name+" diagnostics above, then rerun the commit.",
			))
		}
	}

	return strings.Join(lines, "\n")
}

type failedExecutionCommand struct {
	Group      string
	Name       string
	Status     string
	ExitCode   int
	DurationMS float64
}

func failedExecutionCommands(
	groups []hookExecutionGroupJSON,
) []failedExecutionCommand {
	failedCommands := []failedExecutionCommand{}

	for _, group := range groups {
		for _, command := range group.Commands {
			if command.ExitCode == 0 && command.Status != statusFail {
				continue
			}

			failedCommands = append(failedCommands, failedExecutionCommand{
				Group:      group.Name,
				Name:       command.Name,
				Status:     command.Status,
				ExitCode:   command.ExitCode,
				DurationMS: command.DurationMS,
			})
		}
	}

	return failedCommands
}
