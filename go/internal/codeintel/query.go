// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
)

const defaultRepeatedFailureLimit = 20

type RepeatedFailureQuery struct {
	PolicyID string
	SkillID  string
	Path     string
	Limit    int
}

type RepeatedFailure struct {
	PolicyID    string `json:"policy_id,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	Path        string `json:"path,omitempty"`
	LastTraceID string `json:"last_trace_id,omitempty"`
	LastSeenUTC string `json:"last_seen_utc,omitempty"`
	Count       int    `json:"count"`
	TraceCount  int    `json:"trace_count"`
}

type SearchQuery struct {
	Text  string `json:"text"`
	Limit int    `json:"limit"`
}

type SearchResult struct {
	Kind     string `json:"kind"`
	RecordID string `json:"record_id"`
	TraceID  string `json:"trace_id"`
	PolicyID string `json:"policy_id,omitempty"`
	SkillID  string `json:"skill_id,omitempty"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message,omitempty"`
}

func (store *Store) RepeatedFailures(
	ctx context.Context,
	query RepeatedFailureQuery,
) ([]RepeatedFailure, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultRepeatedFailureLimit
	}

	rows, err := store.db.QueryContext(
		ctx,
		`SELECT
			COALESCE(policy_id, '') AS policy_id,
			COALESCE(skill_id, '') AS skill_id,
			COALESCE(path, '') AS path,
			COUNT(*) AS count,
			COUNT(DISTINCT trace_id) AS trace_count,
			COALESCE(MAX(recorded_at_utc), '') AS last_seen_utc,
			COALESCE(MAX(trace_id), '') AS last_trace_id
		FROM finding_occurrences
		WHERE (? = '' OR policy_id = ?)
			AND (? = '' OR skill_id = ?)
			AND (? = '' OR path = ?)
		GROUP BY policy_id, skill_id, path
		HAVING trace_count > 1
		ORDER BY trace_count DESC, count DESC, last_seen_utc DESC
		LIMIT ?`,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Path,
		query.Path,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query repeated failures: %w", err)
	}
	defer rows.Close()

	return scanRepeatedFailures(rows)
}

func (store *Store) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	rows, err := store.db.QueryContext(
		ctx,
		`SELECT kind, record_id, trace_id, policy_id, skill_id, path, message
		FROM code_intel_fts
		WHERE code_intel_fts MATCH ?
		ORDER BY rank
		LIMIT ?`,
		query.Text,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("search code intelligence FTS: %w", err)
	}
	defer rows.Close()

	results := []SearchResult{}
	for rows.Next() {
		var result SearchResult
		if err := rows.Scan(
			&result.Kind,
			&result.RecordID,
			&result.TraceID,
			&result.PolicyID,
			&result.SkillID,
			&result.Path,
			&result.Message,
		); err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate search results: %w", err)
	}

	return results, nil
}

func scanRepeatedFailures(rows *sql.Rows) ([]RepeatedFailure, error) {
	results := []RepeatedFailure{}
	for rows.Next() {
		var result RepeatedFailure
		if err := rows.Scan(
			&result.PolicyID,
			&result.SkillID,
			&result.Path,
			&result.Count,
			&result.TraceCount,
			&result.LastSeenUTC,
			&result.LastTraceID,
		); err != nil {
			return nil, fmt.Errorf("scan repeated failure: %w", err)
		}
		results = append(results, result)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repeated failures: %w", err)
	}

	return results, nil
}
