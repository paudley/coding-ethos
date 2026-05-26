// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"fmt"
	"strings"
)

// FormatDownstreamAnalysisTOON renders compact downstream analysis for operators.
func FormatDownstreamAnalysisTOON(analysis DownstreamAnalysis) string {
	lines := []string{
		"tool: code-intel",
		"operation: downstream-analysis",
		"storage_backend: " + downstreamTOONCell(analysis.StorageHealth.Backend),
		"storage_source: " + downstreamTOONCell(analysis.StorageHealth.SourceOfTruth),
		"storage_recommendation: " + downstreamTOONCell(
			analysis.StorageHealth.Recommendation,
		),
		fmt.Sprintf("traces: %d", analysis.Stats.Traces),
		fmt.Sprintf("hook_runs: %d", analysis.LogSignals.HookRunCount),
		fmt.Sprintf("lint_runs: %d", analysis.LogSignals.LintRunCount),
		fmt.Sprintf("sqlite_busy_logs: %d", analysis.LogSignals.SQLiteBusyCount),
		fmt.Sprintf(
			"toolchain_failure_logs: %d",
			analysis.LogSignals.ToolchainFailureCount,
		),
	}

	lines = appendDownstreamStringsTOON(
		lines,
		"next_actions",
		analysis.IssueSummary.NextActions,
	)
	lines = appendDownstreamPolicyBlockersTOON(lines, analysis.PolicyBlockers)
	lines = appendDownstreamCommandsTOON(lines, analysis.AffectedCommands)
	lines = appendDownstreamRemediationLoopsTOON(lines, analysis.RemediationLoops)
	lines = appendDownstreamHotspotsTOON(lines, analysis.FindingHotspots)
	lines = appendDownstreamFilePressureTOON(lines, analysis.FilePressure)
	lines = appendDownstreamToolchainHealthTOON(lines, analysis.ToolchainHealth)
	lines = appendDownstreamEvidenceGapsTOON(lines, analysis.EvidenceGaps)

	return strings.Join(lines, "\n")
}

// FormatDownstreamAnalysisHuman renders a concise human downstream summary.
func FormatDownstreamAnalysisHuman(analysis DownstreamAnalysis) string {
	lines := []string{
		"Downstream coding-ethos analysis",
		fmt.Sprintf(
			"Storage: %s backed by %s (%s)",
			analysis.StorageHealth.Backend,
			analysis.StorageHealth.SourceOfTruth,
			analysis.StorageHealth.Recommendation,
		),
		fmt.Sprintf(
			"Signals: %d hook runs, %d lint runs, %d SQLite busy logs, "+
				"%d toolchain failure logs",
			analysis.LogSignals.HookRunCount,
			analysis.LogSignals.LintRunCount,
			analysis.LogSignals.SQLiteBusyCount,
			analysis.LogSignals.ToolchainFailureCount,
		),
	}

	lines = appendDownstreamHumanList(
		lines,
		"Next actions",
		analysis.IssueSummary.NextActions,
	)
	lines = append(lines, downstreamHumanPolicyBlockers(analysis.PolicyBlockers)...)
	lines = append(lines, downstreamHumanCommands(analysis.AffectedCommands)...)
	lines = append(lines, downstreamHumanLoops(analysis.RemediationLoops)...)

	return strings.Join(lines, "\n")
}

func appendDownstreamStringsTOON(
	lines []string,
	name string,
	values []string,
) []string {
	lines = append(lines, fmt.Sprintf("%s[%d]{action}:", name, len(values)))
	for _, value := range values {
		lines = append(lines, "  "+downstreamTOONCell(value))
	}

	return lines
}

func appendDownstreamPolicyBlockersTOON(
	lines []string,
	items []DownstreamPolicyBlocker,
) []string {
	lines = append(
		lines,
		fmt.Sprintf(
			"policy_blockers[%d]{policy_id,count,severity,top_command}:",
			len(items),
		),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%d,%s,%s",
			downstreamTOONCell(item.PolicyID),
			item.Count,
			downstreamTOONCell(item.Severity),
			downstreamTOONCell(downstreamCommandSummary(item.AffectedCommands)),
		))
	}

	return lines
}

func appendDownstreamCommandsTOON(
	lines []string,
	items []DownstreamAffectedCommand,
) []string {
	lines = append(
		lines,
		fmt.Sprintf(
			"affected_commands[%d]{tool,operation,target,risk,status,count}:",
			len(items),
		),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%s,%d",
			downstreamTOONCell(item.Tool),
			downstreamTOONCell(item.OperationKind),
			downstreamTOONCell(item.TargetKind),
			downstreamTOONCell(item.RiskCategory),
			downstreamTOONCell(item.Status),
			item.Count,
		))
	}

	return lines
}

func appendDownstreamRemediationLoopsTOON(
	lines []string,
	items []DownstreamRemediationLoop,
) []string {
	lines = append(
		lines,
		fmt.Sprintf(
			"remediation_loops[%d]{policy_id,file,trace_count,occurrence_count}:",
			len(items),
		),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%d,%d",
			downstreamTOONCell(item.PolicyID),
			downstreamTOONCell(firstNonEmpty(item.File, item.Path)),
			item.TraceCount,
			item.OccurrenceCount,
		))
	}

	return lines
}

func appendDownstreamHotspotsTOON(
	lines []string,
	items []DownstreamFindingHotspot,
) []string {
	lines = append(
		lines,
		fmt.Sprintf("finding_hotspots[%d]{path,tool,code,policy_id,count}:", len(items)),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%d",
			downstreamTOONCell(item.Path),
			downstreamTOONCell(item.Tool),
			downstreamTOONCell(item.Code),
			downstreamTOONCell(item.PolicyID),
			item.Count,
		))
	}

	return lines
}

func appendDownstreamFilePressureTOON(
	lines []string,
	items []DownstreamFilePressure,
) []string {
	lines = append(
		lines,
		fmt.Sprintf("file_pressure[%d]{path,language,line_count,size_bytes}:", len(items)),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%d,%d",
			downstreamTOONCell(item.Path),
			downstreamTOONCell(item.Language),
			item.LineCount,
			item.SizeBytes,
		))
	}

	return lines
}

func appendDownstreamToolchainHealthTOON(
	lines []string,
	items []DownstreamToolchainHealth,
) []string {
	lines = append(
		lines,
		fmt.Sprintf("toolchain_health[%d]{root_cause,count,message}:", len(items)),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%d,%s",
			downstreamTOONCell(item.RootCause),
			item.Count,
			downstreamTOONCell(item.Message),
		))
	}

	return lines
}

func appendDownstreamEvidenceGapsTOON(
	lines []string,
	items []DownstreamEvidenceGap,
) []string {
	lines = append(
		lines,
		fmt.Sprintf(
			"evidence_gaps[%d]{signal,source,query_index,count,recommendation}:",
			len(items),
		),
	)
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%d,%s",
			downstreamTOONCell(item.Signal),
			downstreamTOONCell(item.Source),
			downstreamTOONCell(item.QueryIndex),
			item.Count,
			downstreamTOONCell(item.Recommendation),
		))
	}

	return lines
}

func downstreamHumanPolicyBlockers(items []DownstreamPolicyBlocker) []string {
	lines := make([]string, 0, 1+len(items))
	lines = append(lines, fmt.Sprintf("Policy blockers (%d):", len(items)))

	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"- %s: %d (%s)",
			firstNonEmpty(item.PolicyID, "unknown"),
			item.Count,
			downstreamCommandSummary(item.AffectedCommands),
		))
	}

	return lines
}

func downstreamHumanCommands(items []DownstreamAffectedCommand) []string {
	lines := make([]string, 0, 1+len(items))
	lines = append(lines, fmt.Sprintf("Affected commands (%d):", len(items)))

	for _, item := range items {
		lines = append(lines, fmt.Sprintf(
			"- %s %s %s: %d",
			firstNonEmpty(item.Tool, "unknown"),
			firstNonEmpty(item.OperationKind, "unknown"),
			firstNonEmpty(item.Status, "unknown"),
			item.Count,
		))
	}

	return lines
}

func downstreamHumanLoops(items []DownstreamRemediationLoop) []string {
	lines := make([]string, 0, 1+len(items))
	lines = append(lines, fmt.Sprintf("Remediation loops (%d):", len(items)))

	for _, item := range items {
		target := firstNonEmpty(item.File, item.Path)
		if target != "" {
			target = " " + target
		}

		lines = append(lines, fmt.Sprintf(
			"- %s%s: %d traces",
			firstNonEmpty(item.PolicyID, "unknown"),
			target,
			item.TraceCount,
		))
	}

	return lines
}

func appendDownstreamHumanList(
	lines []string,
	title string,
	values []string,
) []string {
	lines = append(lines, fmt.Sprintf("%s (%d):", title, len(values)))
	for _, value := range values {
		lines = append(lines, "- "+value)
	}

	return lines
}

func downstreamCommandSummary(items []DownstreamAffectedCommand) string {
	if len(items) == 0 {
		return ""
	}

	item := items[0]
	parts := []string{}

	for _, value := range []string{
		item.Tool,
		item.OperationKind,
		item.TargetKind,
		item.RiskCategory,
		item.Status,
	} {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, value)
		}
	}

	return strings.Join(parts, " ")
}

func downstreamTOONCell(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "\\", "\\\\")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, "\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, ",", "\\,")

	return cleaned
}
