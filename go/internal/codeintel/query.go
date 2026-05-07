// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evidence"
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

type SARIFResultQuery struct {
	RunID    string
	TraceID  string
	PolicyID string
	SkillID  string
	Path     string
	Limit    int
}

type RemediationOutcomeQuery struct {
	PolicyID string
	SkillID  string
	Outcome  string
	Path     string
	Limit    int
}

type EmbeddingRecordQuery struct {
	Backend    string
	Collection string
	ModelID    string
	RecordKind string
	RecordID   string
	Limit      int
}

type EmbeddingCandidateQuery struct {
	RecordKind string
	PolicyID   string
	SkillID    string
	Path       string
	Limit      int
}

type CodeChunkQuery struct {
	Path       string
	Language   string
	SymbolKind string
	SymbolName string
	SymbolPath string
	Limit      int
}

type EmbeddingCandidate struct {
	Metadata   map[string]string `json:"metadata"`
	RecordKind string            `json:"record_kind"`
	RecordID   string            `json:"record_id"`
	PolicyID   string            `json:"policy_id,omitempty"`
	SkillID    string            `json:"skill_id,omitempty"`
	Path       string            `json:"path,omitempty"`
	Text       string            `json:"text"`
}

type HybridSearchQuery struct {
	Filters    map[string]string
	Text       string
	Collection string
	ModelID    string
	PolicyID   string
	SkillID    string
	Path       string
	Vector     []float32
	Limit      int
}

type HybridSearchResult struct {
	Metadata    map[string]string `json:"metadata,omitempty"`
	Kind        string            `json:"kind"`
	RecordID    string            `json:"record_id"`
	TraceID     string            `json:"trace_id,omitempty"`
	PolicyID    string            `json:"policy_id,omitempty"`
	SkillID     string            `json:"skill_id,omitempty"`
	Path        string            `json:"path,omitempty"`
	Message     string            `json:"message,omitempty"`
	Source      string            `json:"source"`
	Outcome     string            `json:"outcome,omitempty"`
	VectorID    string            `json:"vector_id,omitempty"`
	Score       float64           `json:"score"`
	VectorScore float64           `json:"vector_score,omitempty"`
	FTSScore    float64           `json:"fts_score,omitempty"`
}

type IndexStatus struct {
	Backend          string               `json:"backend"`
	ModelID          string               `json:"model_id,omitempty"`
	Collection       string               `json:"collection,omitempty"`
	VectorStats      evidence.VectorStats `json:"vector_stats"`
	Stats            Stats                `json:"stats"`
	EmbeddingRecords int                  `json:"embedding_records"`
	ReadyRecords     int                  `json:"ready_records"`
	MissingVectors   int                  `json:"missing_vectors"`
	Fresh            bool                 `json:"fresh"`
}

type RemediationEffectiveness struct {
	PolicyID   string `json:"policy_id,omitempty"`
	SkillID    string `json:"skill_id,omitempty"`
	Fixed      int    `json:"fixed"`
	Repeated   int    `json:"repeated"`
	Attempted  int    `json:"attempted"`
	Superseded int    `json:"superseded"`
	Unknown    int    `json:"unknown"`
	Total      int    `json:"total"`
}

func (store *Store) RepeatedFailures(
	ctx context.Context,
	query RepeatedFailureQuery,
) ([]RepeatedFailure, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = defaultRepeatedFailureLimit
	}

	rows, err := store.database.QueryContext(
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

func (store *Store) Search(
	ctx context.Context,
	query SearchQuery,
) ([]SearchResult, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 10
	}

	rows, err := store.database.QueryContext(
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

		err := rows.Scan(
			&result.Kind,
			&result.RecordID,
			&result.TraceID,
			&result.PolicyID,
			&result.SkillID,
			&result.Path,
			&result.Message,
		)
		if err != nil {
			return nil, fmt.Errorf("scan search result: %w", err)
		}

		results = append(results, result)
	}

	inlineErr0 := rows.Err()
	if inlineErr0 != nil {
		return nil, fmt.Errorf("iterate search results: %w", inlineErr0)
	}

	return results, nil
}

func (store *Store) SARIFResults(
	ctx context.Context,
	query SARIFResultQuery,
) ([]SARIFResultReference, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			result.sarif_result_id, result.rule_id, result.level,
			result.message, result.fingerprint, result.finding_id,
			result.remediation_id, result.policy_id, result.skill_id,
			result.principle_ids, result.path, COALESCE(result.ast_language, ''),
			COALESCE(result.ast_node_kind, ''), COALESCE(result.ast_symbol_kind, ''),
			COALESCE(result.ast_symbol_name, ''), COALESCE(result.ast_symbol_path, ''),
			COALESCE(result.linked_chunk_id, ''), result.start_line,
			result.start_column, result.evaluator_kind, result.cel_policy_id,
			result.cel_expression, result.policy_source, result.search_text
		FROM sarif_results AS result
		JOIN sarif_runs AS run ON run.sarif_run_id = result.sarif_run_id
		WHERE (? = '' OR result.sarif_run_id = ?)
			AND (? = '' OR run.trace_id = ?)
			AND (? = '' OR result.policy_id = ?)
			AND (? = '' OR result.skill_id = ?)
			AND (? = '' OR result.path = ?)
		ORDER BY result.sarif_run_id DESC, result.ordinal ASC
		LIMIT ?`,
		query.RunID,
		query.RunID,
		query.TraceID,
		query.TraceID,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Path,
		query.Path,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query SARIF results: %w", err)
	}
	defer rows.Close()

	return scanSARIFResults(rows)
}

func (store *Store) RemediationOutcomes(
	ctx context.Context,
	query RemediationOutcomeQuery,
) ([]RemediationOutcome, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			outcome_id, remediation_id, finding_id,
			COALESCE(source_trace_id, ''),
			COALESCE(followup_trace_id, ''),
			policy_id, skill_id, file, path, provider, tool, outcome,
			attempt_ordinal, recorded_at_utc, search_text
		FROM remediation_outcomes
		WHERE (? = '' OR policy_id = ?)
			AND (? = '' OR skill_id = ?)
			AND (? = '' OR outcome = ?)
			AND (? = '' OR path = ? OR file = ?)
		ORDER BY recorded_at_utc DESC, attempt_ordinal DESC
		LIMIT ?`,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Outcome,
		query.Outcome,
		query.Path,
		query.Path,
		query.Path,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query remediation outcomes: %w", err)
	}
	defer rows.Close()

	return scanRemediationOutcomes(rows)
}

func (store *Store) RemediationEffectiveness(
	ctx context.Context,
	query RemediationOutcomeQuery,
) ([]RemediationEffectiveness, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			COALESCE(policy_id, '') AS policy_id,
			COALESCE(skill_id, '') AS skill_id,
			SUM(CASE WHEN outcome = 'fixed' THEN 1 ELSE 0 END) AS fixed,
			SUM(CASE WHEN outcome = 'repeated' THEN 1 ELSE 0 END) AS repeated,
			SUM(CASE WHEN outcome = 'attempted' THEN 1 ELSE 0 END) AS attempted,
			SUM(CASE WHEN outcome = 'superseded' THEN 1 ELSE 0 END) AS superseded,
			SUM(CASE
				WHEN outcome NOT IN ('fixed', 'repeated', 'attempted', 'superseded')
					THEN 1
				ELSE 0
			END) AS unknown,
			COUNT(*) AS total
		FROM remediation_outcomes
		WHERE (? = '' OR policy_id = ?)
			AND (? = '' OR skill_id = ?)
			AND (? = '' OR path = ? OR file = ?)
		GROUP BY policy_id, skill_id
		ORDER BY repeated DESC, total DESC`,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Path,
		query.Path,
		query.Path,
	)
	if err != nil {
		return nil, fmt.Errorf("query remediation effectiveness: %w", err)
	}
	defer rows.Close()

	results := []RemediationEffectiveness{}

	for rows.Next() {
		var result RemediationEffectiveness

		err := rows.Scan(
			&result.PolicyID,
			&result.SkillID,
			&result.Fixed,
			&result.Repeated,
			&result.Attempted,
			&result.Superseded,
			&result.Unknown,
			&result.Total,
		)
		if err != nil {
			return nil, fmt.Errorf("scan remediation effectiveness: %w", err)
		}

		results = append(results, result)
	}

	inlineErr1 := rows.Err()
	if inlineErr1 != nil {
		return nil, fmt.Errorf("iterate remediation effectiveness: %w", inlineErr1)
	}

	return results, nil
}

func (store *Store) EmbeddingRecords(
	ctx context.Context,
	query EmbeddingRecordQuery,
) ([]EmbeddingRecord, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			embedding_id, backend, collection, model_id, dimension, input_kind,
			record_kind, record_id, trace_id, policy_id, skill_id, path,
			content_hash, provider, backend_row_id, created_at_utc
		FROM embedding_records
		WHERE (? = '' OR backend = ?)
			AND (? = '' OR collection = ?)
			AND (? = '' OR model_id = ?)
			AND (? = '' OR record_kind = ?)
			AND (? = '' OR record_id = ?)
		ORDER BY created_at_utc DESC, embedding_id
		LIMIT ?`,
		query.Backend,
		query.Backend,
		query.Collection,
		query.Collection,
		query.ModelID,
		query.ModelID,
		query.RecordKind,
		query.RecordKind,
		query.RecordID,
		query.RecordID,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query embedding records: %w", err)
	}
	defer rows.Close()

	return scanEmbeddingRecords(rows)
}

func scanEmbeddingRecords(rows *sql.Rows) ([]EmbeddingRecord, error) {
	results := []EmbeddingRecord{}

	for rows.Next() {
		var result EmbeddingRecord

		err := rows.Scan(
			&result.ID,
			&result.Backend,
			&result.Collection,
			&result.ModelID,
			&result.Dimension,
			&result.InputKind,
			&result.RecordKind,
			&result.RecordID,
			&result.TraceID,
			&result.PolicyID,
			&result.SkillID,
			&result.Path,
			&result.ContentHash,
			&result.Provider,
			&result.BackendRowID,
			&result.CreatedAtUTC,
		)
		if err != nil {
			return nil, fmt.Errorf("scan embedding record: %w", err)
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate embedding records: %w", err)
	}

	return results, nil
}

func (store *Store) EmbeddingCandidates(
	ctx context.Context,
	query EmbeddingCandidateQuery,
) ([]EmbeddingCandidate, error) {
	rows, err := store.queryEmbeddingCandidateRows(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEmbeddingCandidates(rows)
}

func (store *Store) queryEmbeddingCandidateRows(
	ctx context.Context,
	query EmbeddingCandidateQuery,
) (*sql.Rows, error) {
	rows, err := store.database.QueryContext(
		ctx,
		embeddingCandidatesSQL,
		query.RecordKind,
		query.RecordKind,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Path,
		query.Path,
		query.Path,
		query.RecordKind,
		query.RecordKind,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Path,
		query.Path,
		query.Path,
		query.RecordKind,
		query.RecordKind,
		query.Path,
		query.Path,
		query.RecordKind,
		query.RecordKind,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.Path,
		query.Path,
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query embedding candidates: %w", err)
	}

	return rows, nil
}

const embeddingCandidatesSQL = `SELECT 'remediation' AS record_kind,
			remediation_id, policy_id, skill_id,
			COALESCE(NULLIF(file, ''), path), search_text
		FROM remediations
		WHERE (? = '' OR 'remediation' = ?)
			AND (? = '' OR policy_id = ?)
			AND (? = '' OR skill_id = ?)
			AND (? = '' OR file = ? OR path = ?)
		UNION ALL
		SELECT 'remediation_outcome' AS record_kind, outcome_id, policy_id, skill_id,
			COALESCE(NULLIF(file, ''), path), search_text
		FROM remediation_outcomes
		WHERE (? = '' OR 'remediation_outcome' = ?)
			AND (? = '' OR policy_id = ?)
			AND (? = '' OR skill_id = ?)
			AND (? = '' OR file = ? OR path = ?)
		UNION ALL
		SELECT 'code_chunk' AS record_kind, chunk_id, '' AS policy_id,
			'' AS skill_id, path, search_text
		FROM code_chunks
		WHERE (? = '' OR 'code_chunk' = ?)
			AND (? = '' OR path = ?)
		UNION ALL
		SELECT 'sarif_result' AS record_kind, sarif_result_id, policy_id, skill_id,
			path, search_text
		FROM sarif_results
		WHERE (? = '' OR 'sarif_result' = ?)
			AND (? = '' OR policy_id = ?)
			AND (? = '' OR skill_id = ?)
			AND (? = '' OR path = ?)
		LIMIT ?`

func scanEmbeddingCandidates(rows *sql.Rows) ([]EmbeddingCandidate, error) {
	results := []EmbeddingCandidate{}

	for rows.Next() {
		var result EmbeddingCandidate

		err := rows.Scan(
			&result.RecordKind,
			&result.RecordID,
			&result.PolicyID,
			&result.SkillID,
			&result.Path,
			&result.Text,
		)
		if err != nil {
			return nil, fmt.Errorf("scan embedding candidate: %w", err)
		}

		result.Metadata = map[string]string{
			"record_kind": result.RecordKind,
			"record_id":   result.RecordID,
			"policy_id":   result.PolicyID,
			"skill_id":    result.SkillID,
			"path":        result.Path,
		}
		results = append(results, result)
	}

	inlineErr2 := rows.Err()
	if inlineErr2 != nil {
		return nil, fmt.Errorf("iterate embedding candidates: %w", inlineErr2)
	}

	return results, nil
}

func (store *Store) CodeChunks(
	ctx context.Context,
	query CodeChunkQuery,
) ([]CodeChunk, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, COALESCE(parent_symbol_path, ''), parent_chunk_id,
			start_byte, end_byte, start_line,
			end_line, content_hash, search_text, raw_text
		FROM code_chunks
		WHERE (? = '' OR path = ?)
			AND (? = '' OR language = ?)
			AND (? = '' OR symbol_kind = ?)
			AND (? = '' OR symbol_name = ?)
			AND (? = '' OR symbol_path = ?)
		ORDER BY path, start_line, start_byte
		LIMIT ?`,
		query.Path,
		query.Path,
		query.Language,
		query.Language,
		query.SymbolKind,
		query.SymbolKind,
		query.SymbolName,
		query.SymbolName,
		query.SymbolPath,
		query.SymbolPath,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query code chunks: %w", err)
	}
	defer rows.Close()

	return scanCodeChunks(rows)
}

func scanRepeatedFailures(rows *sql.Rows) ([]RepeatedFailure, error) {
	results := []RepeatedFailure{}

	for rows.Next() {
		var result RepeatedFailure

		err := rows.Scan(
			&result.PolicyID,
			&result.SkillID,
			&result.Path,
			&result.Count,
			&result.TraceCount,
			&result.LastSeenUTC,
			&result.LastTraceID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan repeated failure: %w", err)
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate repeated failures: %w", err)
	}

	return results, nil
}

func scanSARIFResults(rows *sql.Rows) ([]SARIFResultReference, error) {
	results := []SARIFResultReference{}

	for rows.Next() {
		var (
			result       SARIFResultReference
			principleIDs string
		)

		err := rows.Scan(
			&result.ID,
			&result.RuleID,
			&result.Level,
			&result.Message,
			&result.Fingerprint,
			&result.FindingID,
			&result.RemediationID,
			&result.PolicyID,
			&result.SkillID,
			&principleIDs,
			&result.Path,
			&result.ASTLanguage,
			&result.ASTNodeKind,
			&result.ASTSymbolKind,
			&result.ASTSymbolName,
			&result.ASTSymbolPath,
			&result.LinkedChunkID,
			&result.StartLine,
			&result.StartColumn,
			&result.EvaluatorKind,
			&result.CELPolicyID,
			&result.CELExpression,
			&result.PolicySource,
			&result.SearchText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan SARIF result: %w", err)
		}

		result.PrincipleIDs = compactStrings(splitCSV(principleIDs))
		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate SARIF results: %w", err)
	}

	return results, nil
}

func scanRemediationOutcomes(rows *sql.Rows) ([]RemediationOutcome, error) {
	results := []RemediationOutcome{}

	for rows.Next() {
		var result RemediationOutcome

		err := rows.Scan(
			&result.ID,
			&result.RemediationID,
			&result.FindingID,
			&result.SourceTraceID,
			&result.FollowupTraceID,
			&result.PolicyID,
			&result.SkillID,
			&result.File,
			&result.Path,
			&result.Provider,
			&result.Tool,
			&result.Outcome,
			&result.AttemptOrdinal,
			&result.RecordedAtUTC,
			&result.SearchText,
		)
		if err != nil {
			return nil, fmt.Errorf("scan remediation outcome: %w", err)
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate remediation outcomes: %w", err)
	}

	return results, nil
}

func splitCSV(value string) []string {
	if value == "" {
		return nil
	}

	return strings.Split(value, ",")
}
