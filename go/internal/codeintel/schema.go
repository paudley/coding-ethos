// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS schema_metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS traces (
		trace_id TEXT PRIMARY KEY,
		trace_kind TEXT NOT NULL,
		recorded_at_utc TEXT,
		repo_root TEXT,
		cwd TEXT,
		provider TEXT,
		event TEXT,
		tool TEXT,
		status TEXT,
		source_path TEXT,
		raw_json TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS findings (
		finding_id TEXT PRIMARY KEY,
		rule_id TEXT,
		tool TEXT,
		code TEXT,
		message TEXT,
		severity TEXT,
		policy_id TEXT,
		skill_id TEXT,
		evaluator_kind TEXT,
		cel_policy_id TEXT,
		cel_expression TEXT,
		policy_source TEXT,
		path TEXT,
		language TEXT,
		symbol_kind TEXT,
		symbol_name TEXT,
		search_text TEXT,
		raw_json TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS finding_occurrences (
		trace_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		finding_id TEXT NOT NULL,
		policy_id TEXT,
		skill_id TEXT,
		path TEXT,
		recorded_at_utc TEXT,
		PRIMARY KEY(trace_id, ordinal),
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS code_files (
		path TEXT PRIMARY KEY,
		language TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		size_bytes INTEGER NOT NULL,
		line_count INTEGER NOT NULL,
		indexed_at_utc TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS code_chunks (
		chunk_id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		language TEXT NOT NULL,
		node_kind TEXT NOT NULL,
		symbol_kind TEXT,
		symbol_name TEXT,
		symbol_path TEXT,
		parent_chunk_id TEXT,
		start_byte INTEGER NOT NULL,
		end_byte INTEGER NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		content_hash TEXT NOT NULL,
		search_text TEXT NOT NULL,
		raw_text TEXT NOT NULL,
		FOREIGN KEY(path) REFERENCES code_files(path) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS sarif_runs (
		sarif_run_id TEXT PRIMARY KEY,
		trace_id TEXT,
		source_path TEXT,
		category TEXT,
		tool_name TEXT,
		automation_id TEXT,
		run_guid TEXT,
		baseline_guid TEXT,
		produced_at_utc TEXT,
		raw_json TEXT NOT NULL,
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE SET NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sarif_results (
		sarif_result_id TEXT PRIMARY KEY,
		sarif_run_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		rule_id TEXT,
		level TEXT,
		message TEXT,
		fingerprint TEXT,
		finding_id TEXT,
		remediation_id TEXT,
		policy_id TEXT,
		skill_id TEXT,
		principle_ids TEXT,
		path TEXT,
		start_line INTEGER,
		start_column INTEGER,
		evaluator_kind TEXT,
		cel_policy_id TEXT,
		cel_expression TEXT,
		policy_source TEXT,
		search_text TEXT,
		raw_json TEXT NOT NULL,
		FOREIGN KEY(sarif_run_id) REFERENCES sarif_runs(sarif_run_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS remediations (
		remediation_id TEXT PRIMARY KEY,
		policy_id TEXT,
		skill_id TEXT,
		file TEXT,
		path TEXT,
		message TEXT,
		advice TEXT,
		search_text TEXT,
		raw_json TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS remediation_occurrences (
		trace_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		remediation_id TEXT NOT NULL,
		policy_id TEXT,
		skill_id TEXT,
		file TEXT,
		path TEXT,
		line INTEGER,
		recorded_at_utc TEXT,
		PRIMARY KEY(trace_id, ordinal),
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS remediation_events (
		event_id TEXT PRIMARY KEY,
		trace_id TEXT NOT NULL,
		remediation_id TEXT,
		finding_id TEXT,
		event TEXT,
		policy_id TEXT,
		skill_id TEXT,
		search_text TEXT,
		raw_json TEXT NOT NULL,
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
	)`,
	`CREATE TABLE IF NOT EXISTS remediation_outcomes (
		outcome_id TEXT PRIMARY KEY,
		remediation_id TEXT,
		finding_id TEXT,
		source_trace_id TEXT,
		followup_trace_id TEXT,
		policy_id TEXT,
		skill_id TEXT,
		file TEXT,
		path TEXT,
		provider TEXT,
		tool TEXT,
		outcome TEXT NOT NULL,
		attempt_ordinal INTEGER,
		recorded_at_utc TEXT,
		search_text TEXT,
		raw_json TEXT NOT NULL,
		FOREIGN KEY(source_trace_id) REFERENCES traces(trace_id) ON DELETE SET NULL,
		FOREIGN KEY(followup_trace_id) REFERENCES traces(trace_id) ON DELETE SET NULL
	)`,
	`CREATE TABLE IF NOT EXISTS embedding_records (
		embedding_id TEXT PRIMARY KEY,
		backend TEXT NOT NULL,
		collection TEXT NOT NULL,
		model_id TEXT NOT NULL,
		dimension INTEGER NOT NULL,
		input_kind TEXT,
		record_kind TEXT NOT NULL,
		record_id TEXT NOT NULL,
		trace_id TEXT,
		policy_id TEXT,
		skill_id TEXT,
		path TEXT,
		content_hash TEXT,
		provider TEXT,
		backend_row_id TEXT,
		created_at_utc TEXT,
		raw_json TEXT NOT NULL
	)`,
	`CREATE VIRTUAL TABLE IF NOT EXISTS code_intel_fts USING fts5(
		kind,
		policy_id,
		skill_id,
		path,
		message,
		search_text,
		record_id UNINDEXED,
		trace_id UNINDEXED
	)`,
	`CREATE INDEX IF NOT EXISTS idx_finding_occurrences_policy
		ON finding_occurrences(policy_id, skill_id, path)`,
	`CREATE INDEX IF NOT EXISTS idx_remediation_occurrences_policy
		ON remediation_occurrences(policy_id, skill_id, file, path)`,
	`CREATE INDEX IF NOT EXISTS idx_remediation_events_trace
		ON remediation_events(trace_id)`,
	`CREATE INDEX IF NOT EXISTS idx_code_chunks_path
		ON code_chunks(path, language, symbol_kind, symbol_name)`,
	`CREATE INDEX IF NOT EXISTS idx_code_chunks_hash
		ON code_chunks(content_hash)`,
	`CREATE INDEX IF NOT EXISTS idx_sarif_results_policy
		ON sarif_results(policy_id, skill_id, path)`,
	`CREATE INDEX IF NOT EXISTS idx_sarif_results_run
		ON sarif_results(sarif_run_id)`,
	`CREATE INDEX IF NOT EXISTS idx_remediation_outcomes_policy
		ON remediation_outcomes(policy_id, skill_id, outcome, path)`,
	`CREATE INDEX IF NOT EXISTS idx_embedding_records_record
		ON embedding_records(backend, collection, model_id, record_kind, record_id)`,
}

type migrationColumn struct {
	Name string
	Type string
}

var migrationColumns = map[string][]migrationColumn{
	"findings": {
		{Name: "evaluator_kind", Type: "TEXT"},
		{Name: "cel_policy_id", Type: "TEXT"},
		{Name: "cel_expression", Type: "TEXT"},
		{Name: "policy_source", Type: "TEXT"},
	},
}
