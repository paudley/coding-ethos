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
}
