// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

func schemaStatements() []string {
	statements := make([]string, 0, schemaStatementCapacity)
	statements = append(statements, traceSchemaStatements()...)
	statements = append(statements, hookSchemaStatements()...)
	statements = append(statements, codeSchemaStatements()...)
	statements = append(statements, sarifSchemaStatements()...)
	statements = append(statements, remediationSchemaStatements()...)
	statements = append(statements, searchSchemaStatements()...)
	statements = append(statements, indexSchemaStatements()...)

	return statements
}

const schemaStatementCapacity = 32

func traceSchemaStatements() []string {
	return []string{
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
	}
}

func hookSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS hook_events (
		trace_id TEXT PRIMARY KEY,
		tracking_id TEXT,
		session_id TEXT,
		provider TEXT,
		event TEXT,
		tool TEXT,
		status TEXT,
		operation_kind TEXT,
		target_kind TEXT,
		risk_category TEXT,
		command_sha256 TEXT,
		command_shape_sha256 TEXT,
		target_set_sha256 TEXT,
		cwd TEXT,
		source TEXT,
		matcher TEXT,
		transcript_path TEXT,
		runtime_ms INTEGER,
		decision_count INTEGER NOT NULL DEFAULT 0,
		blocked INTEGER NOT NULL DEFAULT 0,
		rewritten INTEGER NOT NULL DEFAULT 0,
		additional_context INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
	)`,
		`CREATE TABLE IF NOT EXISTS hook_decisions (
		trace_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		tracking_id TEXT,
		policy_id TEXT,
		decision TEXT,
		severity TEXT,
		skill_id TEXT,
		implementation TEXT,
		principle_ids TEXT,
		diagnostic_count INTEGER NOT NULL DEFAULT 0,
		message_hash TEXT,
		suggestion_hash TEXT,
		message TEXT,
		suggestion TEXT,
		PRIMARY KEY(trace_id, ordinal),
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
	)`,
		`CREATE TABLE IF NOT EXISTS hook_targets (
		trace_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		target_path TEXT NOT NULL,
		target_kind TEXT,
		PRIMARY KEY(trace_id, ordinal),
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
	)`,
		`CREATE TABLE IF NOT EXISTS hook_reviews (
		review_id TEXT PRIMARY KEY,
		trace_id TEXT NOT NULL,
		tracking_id TEXT,
		disposition TEXT NOT NULL,
		reviewer TEXT,
		notes TEXT,
		recorded_at_utc TEXT
	)`,
	}
}

func codeSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS code_files (
		path TEXT PRIMARY KEY,
		language TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		parser_name TEXT,
		parser_version TEXT,
		size_bytes INTEGER NOT NULL,
		line_count INTEGER NOT NULL,
		indexed_at_utc TEXT NOT NULL,
		stale_reason TEXT
	)`,
		`CREATE TABLE IF NOT EXISTS code_chunks (
		chunk_id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		language TEXT NOT NULL,
		node_kind TEXT NOT NULL,
		symbol_kind TEXT,
		symbol_name TEXT,
		symbol_path TEXT,
		parent_symbol_path TEXT,
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
		`CREATE TABLE IF NOT EXISTS code_edges (
		edge_id TEXT PRIMARY KEY,
		edge_kind TEXT NOT NULL,
		path TEXT NOT NULL,
		source_chunk_id TEXT,
		target_path TEXT,
		target_chunk_id TEXT,
		target_symbol_path TEXT,
		target_name TEXT,
		raw_text TEXT,
		FOREIGN KEY(path) REFERENCES code_files(path) ON DELETE CASCADE,
		FOREIGN KEY(source_chunk_id) REFERENCES code_chunks(chunk_id) ON DELETE CASCADE,
		FOREIGN KEY(target_chunk_id) REFERENCES code_chunks(chunk_id) ON DELETE SET NULL
	)`,
		`CREATE TABLE IF NOT EXISTS ast_finding_links (
		link_id TEXT PRIMARY KEY,
		finding_kind TEXT NOT NULL,
		finding_id TEXT NOT NULL,
		chunk_id TEXT NOT NULL,
		path TEXT NOT NULL,
		policy_id TEXT,
		skill_id TEXT,
		symbol_path TEXT,
		content_hash TEXT,
		stale INTEGER NOT NULL DEFAULT 0,
		FOREIGN KEY(chunk_id) REFERENCES code_chunks(chunk_id) ON DELETE CASCADE
	)`,
	}
}

func sarifSchemaStatements() []string {
	return []string{
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
		ast_language TEXT,
		ast_node_kind TEXT,
		ast_symbol_kind TEXT,
		ast_symbol_name TEXT,
		ast_symbol_path TEXT,
		linked_chunk_id TEXT,
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
	}
}

func remediationSchemaStatements() []string {
	return []string{
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
	}
}

func searchSchemaStatements() []string {
	return []string{
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
	}
}

func indexSchemaStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_finding_occurrences_policy
		ON finding_occurrences(policy_id, skill_id, path)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_events_usage
		ON hook_events(provider, status, operation_kind, target_kind, risk_category)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_decisions_policy
		ON hook_decisions(policy_id, skill_id, decision, severity)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_targets_path
		ON hook_targets(target_path, target_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_reviews_trace
		ON hook_reviews(trace_id, tracking_id, disposition)`,
		`CREATE INDEX IF NOT EXISTS idx_remediation_occurrences_policy
		ON remediation_occurrences(policy_id, skill_id, file, path)`,
		`CREATE INDEX IF NOT EXISTS idx_remediation_events_trace
		ON remediation_events(trace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_path
		ON code_chunks(path, language, symbol_kind, symbol_name)`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_symbol_path
		ON code_chunks(path, symbol_path, node_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_hash
		ON code_chunks(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_code_edges_path
		ON code_edges(path, edge_kind, source_chunk_id, target_chunk_id)`,
		`CREATE INDEX IF NOT EXISTS idx_ast_finding_links_chunk
		ON ast_finding_links(chunk_id, policy_id, skill_id)`,
		`CREATE INDEX IF NOT EXISTS idx_sarif_results_policy
		ON sarif_results(policy_id, skill_id, path)`,
		`CREATE INDEX IF NOT EXISTS idx_sarif_results_run
		ON sarif_results(sarif_run_id)`,
		`CREATE INDEX IF NOT EXISTS idx_remediation_outcomes_policy
		ON remediation_outcomes(policy_id, skill_id, outcome, path)`,
		`CREATE INDEX IF NOT EXISTS idx_embedding_records_record
		ON embedding_records(backend, collection, model_id, record_kind, record_id)`,
	}
}

type migrationColumn struct {
	Name string
	Type string
}

func migrationColumns() map[string][]migrationColumn {
	return map[string][]migrationColumn{
		"findings": {
			{Name: "evaluator_kind", Type: "TEXT"},
			{Name: "cel_policy_id", Type: "TEXT"},
			{Name: "cel_expression", Type: "TEXT"},
			{Name: "policy_source", Type: "TEXT"},
		},
		"code_files": {
			{Name: "parser_name", Type: "TEXT"},
			{Name: "parser_version", Type: "TEXT"},
			{Name: "stale_reason", Type: "TEXT"},
		},
		"code_chunks": {
			{Name: "parent_symbol_path", Type: "TEXT"},
		},
		"sarif_results": {
			{Name: "ast_language", Type: "TEXT"},
			{Name: "ast_node_kind", Type: "TEXT"},
			{Name: "ast_symbol_kind", Type: "TEXT"},
			{Name: "ast_symbol_name", Type: "TEXT"},
			{Name: "ast_symbol_path", Type: "TEXT"},
			{Name: "linked_chunk_id", Type: "TEXT"},
		},
	}
}
