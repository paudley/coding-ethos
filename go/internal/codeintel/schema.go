// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

func schemaStatements() []string {
	statements := make([]string, 0, schemaStatementCapacity)
	statements = append(statements, traceSchemaStatements()...)
	statements = append(statements, hookSchemaStatements()...)
	statements = append(statements, proxySchemaStatements()...)
	statements = append(statements, codeSchemaStatements()...)
	statements = append(statements, gitSignalSchemaStatements()...)
	statements = append(statements, sarifSchemaStatements()...)
	statements = append(statements, remediationSchemaStatements()...)
	statements = append(statements, searchSchemaStatements()...)

	return statements
}

const schemaStatementCapacity = 44

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

func proxySchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS proxy_sessions (
		session_id TEXT PRIMARY KEY,
		provider TEXT,
		model TEXT,
		repo_root TEXT,
		started_at_utc TEXT,
		last_seen_utc TEXT,
		request_count INTEGER NOT NULL DEFAULT 0,
		tool_call_count INTEGER NOT NULL DEFAULT 0,
		file_read_count INTEGER NOT NULL DEFAULT 0,
		file_listing_count INTEGER NOT NULL DEFAULT 0,
		edit_count INTEGER NOT NULL DEFAULT 0,
		cache_hit_count INTEGER NOT NULL DEFAULT 0,
		injection_count INTEGER NOT NULL DEFAULT 0,
		truncation_count INTEGER NOT NULL DEFAULT 0,
		denial_count INTEGER NOT NULL DEFAULT 0,
		transform_count INTEGER NOT NULL DEFAULT 0,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		raw_json TEXT NOT NULL
	)`,
		`CREATE TABLE IF NOT EXISTS proxy_events (
		event_id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		event_kind TEXT NOT NULL,
		provider TEXT,
		tool TEXT,
		model TEXT,
		recorded_at_utc TEXT,
		trace_id TEXT,
		tracking_id TEXT,
		repo_root TEXT,
		cwd TEXT,
		target_path TEXT,
		direction TEXT,
		payload_kind TEXT,
		cache_key TEXT,
		input_hash TEXT,
		output_hash TEXT,
		payload_bytes INTEGER NOT NULL DEFAULT 0,
		policy_id TEXT,
		decision TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		total_tokens INTEGER NOT NULL DEFAULT 0,
		policy_evidence_json TEXT NOT NULL DEFAULT '{}',
		dlp_json TEXT NOT NULL DEFAULT '[]',
		metadata_json TEXT NOT NULL,
		raw_json TEXT NOT NULL,
		FOREIGN KEY(session_id) REFERENCES proxy_sessions(session_id) ON DELETE CASCADE
	)`,
		`CREATE TABLE IF NOT EXISTS proxy_transforms (
		event_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		name TEXT NOT NULL,
		reason TEXT,
		input_hash TEXT,
		output_hash TEXT,
		policy_id TEXT,
		decision TEXT,
		evidence_path TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		bytes_removed INTEGER NOT NULL DEFAULT 0,
		findings_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(event_id, ordinal),
		FOREIGN KEY(event_id) REFERENCES proxy_events(event_id) ON DELETE CASCADE
	)`,
	}
}

//nolint:funlen // Code-intel schema DDL stays together for migration review.
func codeSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS code_files (
		path TEXT PRIMARY KEY,
		language TEXT NOT NULL,
		content_hash TEXT NOT NULL,
		parser_name TEXT,
		parser_version TEXT,
		source_mtime_utc TEXT,
		deleted_at_utc TEXT,
		size_bytes INTEGER NOT NULL,
		line_count INTEGER NOT NULL,
		indexed_at_utc TEXT NOT NULL,
		stale_reason TEXT
	)`,
		`CREATE TABLE IF NOT EXISTS code_delete_intents (
		intent_id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		intent_kind TEXT NOT NULL,
		trace_id TEXT,
		recorded_at_utc TEXT,
		provider TEXT,
		event TEXT,
		tool TEXT,
		status TEXT,
		cwd TEXT,
		command_sha256 TEXT,
		command_preview TEXT,
		raw_json TEXT NOT NULL,
		FOREIGN KEY(trace_id) REFERENCES traces(trace_id) ON DELETE CASCADE
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
		normalized_hash TEXT,
		minhash_sig BLOB,
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
		`CREATE TABLE IF NOT EXISTS diff_edit_patterns (
		pattern_hash TEXT PRIMARY KEY,
		diff_source TEXT NOT NULL,
		first_git_head TEXT,
		last_git_head TEXT,
		target_path TEXT NOT NULL,
		hunk_header TEXT,
		removed_sha256 TEXT,
		added_sha256 TEXT,
		old_start INTEGER NOT NULL,
		old_lines INTEGER NOT NULL,
		new_start INTEGER NOT NULL,
		new_lines INTEGER NOT NULL,
		ast_chunk_id TEXT,
		ast_language TEXT,
		ast_node_kind TEXT,
		ast_symbol_kind TEXT,
		ast_symbol_name TEXT,
		ast_symbol_path TEXT,
		first_seen_utc TEXT NOT NULL,
		last_seen_utc TEXT NOT NULL,
		seen_count INTEGER NOT NULL DEFAULT 1,
		FOREIGN KEY(ast_chunk_id) REFERENCES code_chunks(chunk_id) ON DELETE SET NULL
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
		`CREATE TABLE IF NOT EXISTS lsh_bands (
		band_hash TEXT NOT NULL,
		band_index INTEGER NOT NULL,
		chunk_id TEXT NOT NULL,
		path TEXT NOT NULL,
		symbol_name TEXT NOT NULL,
		FOREIGN KEY(chunk_id) REFERENCES code_chunks(chunk_id) ON DELETE CASCADE
	)`,
	}
}

func gitSignalSchemaStatements() []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS git_signal_metadata (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
		`CREATE TABLE IF NOT EXISTS git_file_signals (
		path TEXT PRIMARY KEY,
		commit_count INTEGER NOT NULL,
		churn INTEGER NOT NULL,
		additions INTEGER NOT NULL,
		deletions INTEGER NOT NULL,
		author_count INTEGER NOT NULL,
		primary_author_name TEXT,
		primary_author_email TEXT,
		primary_author_commits INTEGER NOT NULL,
		first_seen_utc TEXT,
		last_seen_utc TEXT,
		hotspot_score DOUBLE NOT NULL
	)`,
		`CREATE TABLE IF NOT EXISTS git_file_authors (
		path TEXT NOT NULL,
		author_email TEXT NOT NULL,
		author_name TEXT NOT NULL,
		commit_count INTEGER NOT NULL,
		additions INTEGER NOT NULL,
		deletions INTEGER NOT NULL,
		last_seen_utc TEXT,
		PRIMARY KEY(path, author_email)
	)`,
		`CREATE TABLE IF NOT EXISTS git_cochanges (
		path TEXT NOT NULL,
		related_path TEXT NOT NULL,
		cochange_count INTEGER NOT NULL,
		last_seen_utc TEXT,
		hidden_coupling INTEGER NOT NULL,
		PRIMARY KEY(path, related_path)
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
		proxy_event_id TEXT,
		proxy_session_id TEXT,
		proxy_event_kind TEXT,
		proxy_direction TEXT,
		proxy_payload_kind TEXT,
		proxy_trace_id TEXT,
		proxy_tracking_id TEXT,
		proxy_transform TEXT,
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
		`CREATE TABLE IF NOT EXISTS code_intel_fts (
		fts_id TEXT,
		kind TEXT,
		policy_id TEXT,
		skill_id TEXT,
		path TEXT,
		message TEXT,
		search_text TEXT,
		record_id TEXT,
		trace_id TEXT
	)`,
		`ALTER TABLE code_intel_fts ADD COLUMN IF NOT EXISTS fts_id TEXT`,
		`UPDATE code_intel_fts
		SET fts_id = kind || ':' || record_id || ':' || trace_id
		WHERE fts_id IS NULL OR fts_id = ''`,
		`CREATE TABLE IF NOT EXISTS code_intel_search_terms (
		term TEXT NOT NULL,
		fts_id TEXT NOT NULL
	)`,
	}
}

func indexSchemaStatements() []string {
	return []string{
		`CREATE INDEX IF NOT EXISTS idx_finding_occurrences_policy
		ON finding_occurrences(policy_id, skill_id, path)`,
		`CREATE INDEX IF NOT EXISTS idx_traces_source_path
		ON traces(source_path)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_events_usage
		ON hook_events(provider, status, operation_kind, target_kind, risk_category)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_decisions_policy
		ON hook_decisions(policy_id, skill_id, decision, severity)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_targets_path
		ON hook_targets(target_path, target_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_hook_reviews_trace
		ON hook_reviews(trace_id, tracking_id, disposition)`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_events_session
		ON proxy_events(session_id, event_kind, recorded_at_utc)`,
		`CREATE INDEX IF NOT EXISTS idx_proxy_events_path
		ON proxy_events(target_path, policy_id, decision)`,
		`CREATE INDEX IF NOT EXISTS idx_remediation_occurrences_policy
		ON remediation_occurrences(policy_id, skill_id, file, path)`,
		`CREATE INDEX IF NOT EXISTS idx_remediation_events_trace
		ON remediation_events(trace_id)`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_path
		ON code_chunks(path, language, symbol_kind, symbol_name)`,
		`CREATE INDEX IF NOT EXISTS idx_code_delete_intents_path
		ON code_delete_intents(path, recorded_at_utc)`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_symbol_path
		ON code_chunks(path, symbol_path, node_kind)`,
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_hash
		ON code_chunks(content_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_code_edges_path
		ON code_edges(path, edge_kind, source_chunk_id, target_chunk_id)`,
		`CREATE INDEX IF NOT EXISTS idx_diff_edit_patterns_lookup
		ON diff_edit_patterns(target_path, diff_source, seen_count)`,
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
		`CREATE INDEX IF NOT EXISTS idx_code_chunks_normalized_hash
		ON code_chunks(normalized_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_lsh_bands_lookup
		ON lsh_bands(band_hash, band_index)`,
		`CREATE INDEX IF NOT EXISTS idx_code_intel_search_terms_term
		ON code_intel_search_terms(term, fts_id)`,
		`CREATE INDEX IF NOT EXISTS idx_code_intel_search_terms_fts_id
		ON code_intel_search_terms(fts_id)`,
	}
}
