// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func collectGeminiViolations(batches []geminiBatchOutcome) []geminiViolation {
	totalViolations := 0
	for _, batch := range batches {
		totalViolations += len(batch.Result.Violations)
	}

	allViolations := make([]geminiViolation, 0, totalViolations)
	for _, batch := range batches {
		allViolations = append(allViolations, batch.Result.Violations...)
	}

	return allViolations
}

func geminiOutcomeStatus(outcome geminiCheckOutcome) string {
	if outcome.Filtered.hasBlockingCriticals() {
		return statusFail
	}

	if outcome.BatchErrors > 0 && outcome.BatchesCompleted == 0 {
		return statusError
	}

	if outcome.BatchErrors > 0 {
		return statusWarn
	}

	for _, violation := range outcome.Filtered.InDiff {
		if violation.Severity == severityWarning {
			return statusWarn
		}
	}

	return passVerdict
}

func formatGeminiReport(
	scope string,
	outcomes []geminiCheckOutcome,
	format string,
) string {
	if !hasGeminiIssues(outcomes) {
		return ""
	}

	switch format {
	case hookOutputFormatJSON:
		return formatGeminiReportJSON(scope, outcomes)
	case hookOutputFormatTOON:
		return formatGeminiReportTOON(scope, outcomes)
	default:
		return formatGeminiReportHuman(scope, outcomes)
	}
}

func formatGeminiReportHuman(scope string, outcomes []geminiCheckOutcome) string {
	lines := []string{
		"",
		strings.Repeat("=", reportDividerWidth),
		"GEMINI AI CODE CHECKS (GO)",
		strings.Repeat("=", reportDividerWidth),
		"Scope: " + scope,
		"",
	}

	for _, outcome := range outcomes {
		if shouldSkipGeminiOutcome(outcome) {
			continue
		}

		lines = appendGeminiOutcomeReport(lines, outcome)
	}

	lines = append(lines, strings.Repeat("=", reportDividerWidth))

	return strings.Join(lines, "\n")
}

func formatGeminiReportJSON(scope string, outcomes []geminiCheckOutcome) string {
	summary := geminiReportSummaryForOutcomes(
		scope,
		outcomes,
		hookOutputFormatJSON,
	)

	content, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return formatGeminiReportHuman(scope, outcomes)
	}

	return string(content)
}

func formatGeminiReportTOON(scope string, outcomes []geminiCheckOutcome) string {
	summary := geminiReportSummaryForOutcomes(
		scope,
		outcomes,
		hookOutputFormatTOON,
	)

	lines := []string{
		"tool: gemini",
		"scope: " + toonCell(summary.Scope),
		"status: " + summary.Status,
		fmt.Sprintf(
			"outcomes[%d]{name,status,model,service_tier,included_files,batches}:",
			len(summary.Outcomes),
		),
	}
	for _, outcome := range summary.Outcomes {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%s,%s,%d,%d",
				toonCell(outcome.Name),
				outcome.Status,
				toonCell(outcome.Model),
				toonCell(outcome.ServiceTier),
				outcome.IncludedFileCount,
				outcome.BatchCount,
			),
		)
	}

	violations := geminiReportViolations(summary.Outcomes)

	lines = append(
		lines,
		fmt.Sprintf(
			"violations[%d]{scope,severity,file,line,ethos_section,message}:",
			len(violations),
		),
	)
	for _, violation := range violations {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%s,%d,%s,%s",
				toonCell(violation.Scope),
				toonCell(violation.Severity),
				toonCell(violation.File),
				violation.Line,
				toonCell(violation.EthosSection),
				toonCell(violation.Message),
			),
		)
	}

	batchErrors := geminiReportBatchErrors(summary.Outcomes)
	if len(batchErrors) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("batch_errors[%d]{check,batch,files,error}:", len(batchErrors)),
		)
		for _, item := range batchErrors {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s,%d,%s,%s",
					toonCell(item.Check),
					item.Error.Batch,
					toonCell(strings.Join(item.Error.Files, " ")),
					toonCell(item.Error.Error),
				),
			)
		}
	}

	return strings.Join(lines, "\n")
}

type geminiScopedViolation struct {
	Scope        string
	Severity     string
	File         string
	Message      string
	EthosSection string
	Line         int
}

type geminiScopedBatchError struct {
	Check string
	Error geminiBatchError
}

func geminiReportSummaryForOutcomes(
	scope string,
	outcomes []geminiCheckOutcome,
	format string,
) geminiReportSummary {
	summary := geminiReportSummary{
		Format: format,
		Scope:  scope,
		Status: passVerdict,
	}

	for _, outcome := range outcomes {
		if shouldSkipGeminiOutcome(outcome) {
			continue
		}

		status := geminiOutcomeStatus(outcome)
		switch {
		case status == statusFail:
			summary.Status = statusFail
		case status == statusError && summary.Status != statusFail:
			summary.Status = statusError
		case status == statusWarn && summary.Status == passVerdict:
			summary.Status = statusWarn
		}

		summary.Outcomes = append(summary.Outcomes, geminiOutcomeReport{
			Name:              outcome.Plan.Name,
			Status:            status,
			Model:             outcome.Plan.Model,
			ServiceTier:       outcome.Plan.ServiceTier,
			IncludedFileCount: len(outcome.Plan.IncludedFiles),
			BatchCount:        len(outcome.Plan.Batches),
			BatchErrors:       geminiBatchErrorsForOutcome(outcome),
			SkippedLargeFiles: outcome.Plan.SkippedLargeFiles,
			InDiff:            outcome.Filtered.InDiff,
			PreExisting:       outcome.Filtered.PreExisting,
		})
	}

	return summary
}

func geminiBatchErrorsForOutcome(outcome geminiCheckOutcome) []geminiBatchError {
	errors := []geminiBatchError{}

	for index, batch := range outcome.Batches {
		if batch.Error == "" {
			continue
		}

		errors = append(errors, geminiBatchError{
			Batch: index + 1,
			Files: batch.Files,
			Error: batch.Error,
		})
	}

	return errors
}

func geminiReportViolations(
	outcomes []geminiOutcomeReport,
) []geminiScopedViolation {
	violations := []geminiScopedViolation{}

	for _, outcome := range outcomes {
		for _, violation := range outcome.InDiff {
			violations = append(
				violations,
				geminiScopedViolation{
					Scope:        "in_diff",
					Severity:     violation.Severity,
					File:         violation.File,
					Message:      violation.Message,
					EthosSection: violation.EthosSection,
					Line:         violation.Line,
				},
			)
		}

		for _, violation := range outcome.PreExisting {
			violations = append(
				violations,
				geminiScopedViolation{
					Scope:        "pre_existing",
					Severity:     violation.Severity,
					File:         violation.File,
					Message:      violation.Message,
					EthosSection: violation.EthosSection,
					Line:         violation.Line,
				},
			)
		}
	}

	sort.SliceStable(violations, func(left, right int) bool {
		if violations[left].File != violations[right].File {
			return violations[left].File < violations[right].File
		}

		if violations[left].Line != violations[right].Line {
			return violations[left].Line < violations[right].Line
		}

		return violations[left].Severity < violations[right].Severity
	})

	return violations
}

func geminiReportBatchErrors(
	outcomes []geminiOutcomeReport,
) []geminiScopedBatchError {
	errors := []geminiScopedBatchError{}

	for _, outcome := range outcomes {
		for _, batchError := range outcome.BatchErrors {
			errors = append(errors, geminiScopedBatchError{
				Error: batchError,
				Check: outcome.Name,
			})
		}
	}

	return errors
}

func hasGeminiIssues(outcomes []geminiCheckOutcome) bool {
	for _, outcome := range outcomes {
		if !shouldSkipGeminiOutcome(outcome) {
			return true
		}
	}

	return false
}

func shouldSkipGeminiOutcome(outcome geminiCheckOutcome) bool {
	status := geminiOutcomeStatus(outcome)

	return status == passVerdict && !outcome.Filtered.hasAnyInDiff()
}

func appendGeminiOutcomeReport(
	lines []string,
	outcome geminiCheckOutcome,
) []string {
	status := geminiOutcomeStatus(outcome)

	lines = append(lines, geminiOutcomeHeader(outcome, status))
	if len(outcome.Plan.SkippedLargeFiles) > 0 {
		lines = append(
			lines,
			"  Skipped large files: "+strings.Join(
				outcome.Plan.SkippedLargeFiles,
				", ",
			),
		)
	}

	lines = appendGeminiViolationSection(
		lines,
		"  [In your changes]",
		outcome.Filtered.InDiff,
	)
	if len(outcome.Filtered.PreExisting) > 0 {
		lines = appendGeminiViolationSection(
			lines,
			fmt.Sprintf("  [Pre-existing (%d)]", len(outcome.Filtered.PreExisting)),
			outcome.Filtered.PreExisting,
		)
	}

	lines = appendGeminiBatchErrors(lines, outcome.Batches)

	return append(lines, "")
}

func geminiOutcomeHeader(outcome geminiCheckOutcome, status string) string {
	return fmt.Sprintf(
		"%s: %s (model=%s, tier=%s, %d included file(s), %d batch(es))",
		outcome.Plan.Name,
		status,
		outcome.Plan.Model,
		outcome.Plan.ServiceTier,
		len(outcome.Plan.IncludedFiles),
		len(outcome.Plan.Batches),
	)
}

func appendGeminiViolationSection(
	lines []string,
	header string,
	violations []geminiViolation,
) []string {
	if len(violations) == 0 {
		return lines
	}

	lines = append(lines, header)
	for _, violation := range violations {
		lines = append(lines, geminiViolationLine(violation))
		if violation.EthosSection != "" {
			lines = append(
				lines,
				fmt.Sprintf("     (ETHOS %s)", violation.EthosSection),
			)
		}
	}

	return lines
}

func geminiViolationLine(violation geminiViolation) string {
	lineLabel := "?"
	if violation.Line > 0 {
		lineLabel = strconv.Itoa(violation.Line)
	}

	return fmt.Sprintf(
		"  %s %s:%s %s",
		formatSeverityIcon(violation.Severity),
		violation.File,
		lineLabel,
		violation.Message,
	)
}

func appendGeminiBatchErrors(
	lines []string,
	batches []geminiBatchOutcome,
) []string {
	for index, batch := range batches {
		if batch.Error != "" {
			lines = append(
				lines,
				fmt.Sprintf(
					"  !! Batch %d (%s): %s",
					index+1,
					strings.Join(batch.Files, ", "),
					batch.Error,
				),
			)
		}
	}

	return lines
}

func formatSeverityIcon(severity string) string {
	switch severity {
	case severityCritical:
		return "XX"
	case severityWarning:
		return "W "
	default:
		return "--"
	}
}

// The Go Gemini runner owns prompt-pack loading, file selection, model/service-tier
// resolution, concurrent batch execution, repo-local response caching, modal
// allowlist filtering, diff-aware reporting, and raw API transport.
