// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func TestRebuildDuckDBIndexImportsLegacySQLite(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	legacy, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open legacy SQLite: %v", err)
	}

	err = legacy.IngestTrace(ctx, Trace{
		ID:            "trace-1",
		Kind:          "hook",
		RecordedAtUTC: "2026-05-24T00:00:00Z",
		Tool:          "Bash",
		Status:        "blocked",
		Raw:           []byte(`{"trace_id":"trace-1"}`),
		HookEvent: &HookEventAnalytics{
			TraceID:            "trace-1",
			Tool:               "Bash",
			Status:             "blocked",
			OperationKind:      "lint",
			TargetKind:         "unknown",
			RiskCategory:       "policy_block",
			CommandShapeSHA256: "shape-1",
			Blocked:            true,
		},
		HookDecisions: []HookDecisionAnalytics{
			{
				TraceID:  "trace-1",
				PolicyID: "filesystem.line_limits",
				Decision: "block",
				Severity: "block",
			},
		},
		Findings: []evidence.Finding{
			{
				ID:       "finding-1",
				Tool:     "ruff",
				Code:     "E501",
				PolicyID: "filesystem.line_limits",
				Message:  "line too long",
				SourceSpan: evidence.SourceSpan{
					Path: "src/big.py",
				},
			},
		},
		AgentRemediation: []agentmsg.Remediation{
			{
				ID:       "rem-1",
				PolicyID: "filesystem.line_limits",
				SkillID:  "managed-toolchain",
				File:     "src/big.py",
				Message:  "split large file",
			},
		},
	})
	if err != nil {
		t.Fatalf("ingest legacy trace: %v", err)
	}
	_, err = legacy.database.ExecContext(ctx, extendedLegacySeedSQL)
	if err != nil {
		t.Fatalf("seed extended legacy rows: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy SQLite: %v", err)
	}

	err = NewEventLog(DefaultEventLogDir(root)).Append("run-1", []EventRecord{
		{
			Kind:    "hook_trace",
			TraceID: "trace-1",
			Tool:    "Bash",
		},
	})
	if err != nil {
		t.Fatalf("append event log: %v", err)
	}

	summary, err := RebuildDuckDBIndex(ctx, root, "", "")
	if err != nil {
		t.Fatalf("rebuild DuckDB index: %v", err)
	}
	if !summary.ImportedLegacySQLite ||
		summary.EventCount != 1 ||
		summary.ImportedEventCount != 1 ||
		summary.Stats.Traces != 1 ||
		summary.Stats.HookEvents != 1 ||
		summary.Stats.HookDecisions != 1 ||
		summary.Stats.HookTargets != 1 ||
		summary.Stats.HookReviews != 1 ||
		summary.Stats.ProxySessions != 1 ||
		summary.Stats.ProxyEvents != 1 ||
		summary.Stats.ProxyTransforms != 1 ||
		summary.Stats.Findings != 1 ||
		summary.Stats.Files != 1 ||
		summary.Stats.CodeChunks != 1 ||
		summary.Stats.CodeEdges != 1 ||
		summary.Stats.ASTFindingLinks != 1 ||
		summary.Stats.SARIFRuns != 1 ||
		summary.Stats.SARIFResults != 1 ||
		summary.Stats.Remediations != 1 ||
		summary.Stats.RemediationEvents != 1 ||
		summary.Stats.RemediationOutcomes != 1 ||
		summary.Stats.EmbeddingRecords != 1 ||
		summary.Stats.FtsRows != 4 ||
		!summary.RemovedLegacySQLite {
		t.Fatalf("unexpected rebuild summary: %#v", summary)
	}

	duckStore, err := OpenDuckDBReadOnly(ctx, DefaultDuckDBPath(root))
	if err != nil {
		t.Fatalf("open rebuilt DuckDB: %v", err)
	}
	defer duckStore.Close()

	analysis, err := AnalyzeDownstreamDuckDB(ctx, root, duckStore, 5)
	if err != nil {
		t.Fatalf("analyze DuckDB downstream: %v", err)
	}
	assertDownstreamPolicyBlocker(t, analysis, "filesystem.line_limits")
	assertDownstreamAffectedCommand(t, analysis, "filesystem.line_limits", "Bash")
	if analysis.StorageHealth.Backend != "duckdb" ||
		analysis.StorageHealth.SourceOfTruth != "event_log" ||
		analysis.StorageHealth.EventCount != 1 {
		t.Fatalf("storage health = %#v", analysis.StorageHealth)
	}
	if len(analysis.AffectedCommands) == 0 {
		t.Fatalf("missing top-level affected commands: %#v", analysis)
	}
	if analysis.IssueSummary.StorageDecision == "" {
		t.Fatalf("missing issue summary: %#v", analysis.IssueSummary)
	}
}

const extendedLegacySeedSQL = `INSERT INTO hook_targets(trace_id, ordinal, target_path, target_kind)
VALUES ('trace-1', 0, 'src/big.py', 'source_file');
INSERT INTO hook_reviews(review_id, trace_id, tracking_id, disposition, reviewer, notes, recorded_at_utc)
VALUES ('review-1', 'trace-1', 'track-1', 'correct_block', 'tester', '', '2026-05-24T00:00:00Z');
INSERT INTO proxy_sessions(
	session_id, provider, model, repo_root, started_at_utc, last_seen_utc,
	request_count, tool_call_count, file_read_count, file_listing_count,
	edit_count, cache_hit_count, injection_count, truncation_count, denial_count,
	transform_count, input_tokens, output_tokens, total_tokens, raw_json
) VALUES (
	'session-1', 'codex', 'gpt', '', '2026-05-24T00:00:00Z',
	'2026-05-24T00:00:00Z', 1, 1, 0, 0, 0, 0, 0, 0, 1, 1, 1, 2, 3, '{}'
);
INSERT INTO proxy_events(
	event_id, session_id, event_kind, provider, tool, model, recorded_at_utc,
	trace_id, tracking_id, repo_root, cwd, target_path, direction, payload_kind,
	cache_key, input_hash, output_hash, payload_bytes, policy_id, decision,
	input_tokens, output_tokens, total_tokens, policy_evidence_json, dlp_json,
	metadata_json, raw_json
) VALUES (
	'proxy-1', 'session-1', 'tool_call', 'codex', 'Bash', 'gpt',
	'2026-05-24T00:00:00Z', 'trace-1', 'track-1', '', '', 'src/big.py',
	'local', 'tool_call', '', 'in', 'out', 10, 'filesystem.line_limits',
	'block', 1, 2, 3, '{}', '[]', '{}', '{}'
);
INSERT INTO proxy_transforms(
	event_id, ordinal, name, reason, input_hash, output_hash, policy_id,
	decision, evidence_path, input_tokens, output_tokens, bytes_removed,
	findings_count
) VALUES (
	'proxy-1', 0, 'redact', 'test', 'in', 'out', 'filesystem.line_limits',
	'block', 'src/big.py', 1, 2, 3, 1
);
INSERT INTO code_files(path, language, content_hash, size_bytes, line_count, indexed_at_utc)
VALUES ('src/big.py', 'python', 'hash', 100, 20, '2026-05-24T00:00:00Z');
INSERT INTO code_delete_intents(
	intent_id, path, intent_kind, trace_id, recorded_at_utc, provider, event,
	tool, status, cwd, command_sha256, command_preview, raw_json
) VALUES (
	'intent-1', 'src/old.py', 'delete', 'trace-1', '2026-05-24T00:00:00Z',
	'codex', 'PreToolUse', 'Bash', 'blocked', '', 'sha', 'rm src/old.py', '{}'
);
INSERT INTO code_chunks(
	chunk_id, path, language, node_kind, symbol_kind, symbol_name, symbol_path,
	parent_symbol_path, parent_chunk_id, start_byte, end_byte, start_line,
	end_line, content_hash, normalized_hash, search_text, raw_text
) VALUES (
	'chunk-1', 'src/big.py', 'python', 'module', 'module', 'big', 'big',
	'', '', 0, 10, 1, 1, 'hash', 'nhash', 'big', 'big'
);
INSERT INTO code_edges(
	edge_id, edge_kind, path, source_chunk_id, target_path, target_chunk_id,
	target_symbol_path, target_name, raw_text
) VALUES (
	'edge-1', 'contains', 'src/big.py', 'chunk-1', 'src/big.py', 'chunk-1',
	'big', 'big', 'contains'
);
INSERT INTO ast_finding_links(
	link_id, finding_kind, finding_id, chunk_id, path, policy_id, skill_id,
	symbol_path, content_hash, stale
) VALUES (
	'link-1', 'finding', 'finding-1', 'chunk-1', 'src/big.py',
	'filesystem.line_limits', 'managed-toolchain', 'big', 'hash', 0
);
INSERT INTO sarif_runs(
	sarif_run_id, trace_id, source_path, category, tool_name, automation_id,
	run_guid, baseline_guid, produced_at_utc, raw_json
) VALUES (
	'sarif-1', 'trace-1', 'sarif.json', 'lint', 'ruff', '', '', '',
	'2026-05-24T00:00:00Z', '{}'
);
INSERT INTO sarif_results(
	sarif_result_id, sarif_run_id, ordinal, rule_id, level, message,
	fingerprint, finding_id, remediation_id, policy_id, skill_id, path,
	search_text, raw_json
) VALUES (
	'sarif-result-1', 'sarif-1', 0, 'E501', 'error', 'line too long', 'fp',
	'finding-1', 'rem-1', 'filesystem.line_limits', 'managed-toolchain',
	'src/big.py', 'line too long', '{}'
);
INSERT INTO remediation_events(
	event_id, trace_id, remediation_id, finding_id, event, policy_id, skill_id,
	search_text, raw_json
) VALUES (
	'rem-event-1', 'trace-1', 'rem-1', 'finding-1', 'suggested',
	'filesystem.line_limits', 'managed-toolchain', 'split large file', '{}'
);
INSERT INTO remediation_outcomes(
	outcome_id, remediation_id, finding_id, source_trace_id, followup_trace_id,
	policy_id, skill_id, file, path, provider, tool, outcome, attempt_ordinal,
	recorded_at_utc, search_text, raw_json
) VALUES (
	'outcome-1', 'rem-1', 'finding-1', 'trace-1', NULL, 'filesystem.line_limits',
	'managed-toolchain', 'src/big.py', 'src/big.py', 'codex', 'Bash',
	'repeated', 1, '2026-05-24T00:00:00Z', 'repeated split large file', '{}'
);
INSERT INTO embedding_records(
	embedding_id, backend, collection, model_id, dimension, input_kind,
	record_kind, record_id, trace_id, policy_id, skill_id, path, content_hash,
	provider, backend_row_id, created_at_utc, raw_json
) VALUES (
	'emb-1', 'sqlite-vec', 'remediations', 'model', 3, 'text', 'remediation',
	'rem-1', 'trace-1', 'filesystem.line_limits', 'managed-toolchain',
	'src/big.py', 'hash', 'codex', 'row-1', '2026-05-24T00:00:00Z', '{}'
);
INSERT INTO code_intel_fts(kind, policy_id, skill_id, path, message, search_text, record_id, trace_id)
VALUES (
	'finding', 'filesystem.line_limits', 'managed-toolchain', 'src/big.py',
	'line too long', 'line too long', 'finding-1', 'trace-1'
);`
