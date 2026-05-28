// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"fmt"
	"strings"
)

func FormatSessionSnapshotTOON(snapshot SessionSnapshot) string {
	lines := make(
		[]string,
		0,
		32+len(snapshot.CurrentBlockers)+len(snapshot.LinkedTraceIDs)+
			len(snapshot.RecommendedChecks),
	)
	lines = append(lines,
		"kind: "+sessionTOONCell(snapshot.Kind),
		"schema_version: "+sessionTOONCell(snapshot.SchemaVersion),
		"generated_at_utc: "+sessionTOONCell(snapshot.GeneratedAtUTC),
		"session_id: "+sessionTOONCell(snapshot.Session.ID),
		"session_source: "+sessionTOONCell(snapshot.Session.Source),
		"provider: "+sessionTOONCell(snapshot.Session.Provider),
		"model: "+sessionTOONCell(snapshot.Session.Model),
		"repo_root: "+sessionTOONCell(snapshot.Repository.Root),
		"worktree: "+sessionTOONCell(snapshot.Repository.Worktree),
		"branch: "+sessionTOONCell(snapshot.Repository.Branch),
		"head_commit: "+sessionTOONCell(snapshot.Repository.HeadCommit),
		fmt.Sprintf("hook_events: %d", snapshot.Hooks.Events),
		fmt.Sprintf("hook_blocked_events: %d", snapshot.Hooks.BlockedEvents),
		fmt.Sprintf("hook_decisions: %d", snapshot.Hooks.DecisionCount),
		fmt.Sprintf("memory_trace_events: %d", snapshot.Memory.TraceEvents),
		fmt.Sprintf("memory_import_events: %d", snapshot.Memory.ImportEvents),
		fmt.Sprintf("memory_export_events: %d", snapshot.Memory.ExportEvents),
		fmt.Sprintf("proxy_sessions: %d", snapshot.Proxy.Sessions),
		fmt.Sprintf("proxy_events: %d", snapshot.Proxy.Events),
		fmt.Sprintf("proxy_file_reads: %d", snapshot.Proxy.FileReads),
		fmt.Sprintf("proxy_truncations: %d", snapshot.Proxy.Truncations),
		fmt.Sprintf("proxy_output_compression: %d", snapshot.Proxy.OutputCompression),
		"code_intel_freshness: "+sessionTOONCell(snapshot.CodeIntel.Freshness),
		fmt.Sprintf("code_intel_traces: %d", snapshot.CodeIntel.TraceCount),
		fmt.Sprintf("code_intel_files: %d", snapshot.CodeIntel.CodeFileCount),
		fmt.Sprintf("code_intel_chunks: %d", snapshot.CodeIntel.CodeChunkCount),
		"risk_level: "+sessionTOONCell(snapshot.Risk.Level),
		fmt.Sprintf("current_blockers[%d]{trace_id,policy_id,severity,decision,message}:",
			len(snapshot.CurrentBlockers)),
	)

	for _, blocker := range snapshot.CurrentBlockers {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s,%s",
			sessionTOONCell(blocker.TraceID),
			sessionTOONCell(blocker.PolicyID),
			sessionTOONCell(blocker.Severity),
			sessionTOONCell(blocker.Decision),
			sessionTOONCell(blocker.Message),
		))
	}

	lines = append(lines, fmt.Sprintf(
		"linked_trace_ids[%d]{trace_id}:",
		len(snapshot.LinkedTraceIDs),
	))
	for _, traceID := range snapshot.LinkedTraceIDs {
		lines = append(lines, "  "+sessionTOONCell(traceID))
	}

	lines = append(lines, fmt.Sprintf(
		"recommended_checks[%d]{message}:",
		len(snapshot.RecommendedChecks),
	))
	for _, check := range snapshot.RecommendedChecks {
		lines = append(lines, "  "+sessionTOONCell(check))
	}

	return strings.Join(lines, "\n")
}

func sessionTOONCell(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "\\", "\\\\")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, "\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, ",", "\\,")

	return cleaned
}
