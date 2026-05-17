// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/minhash"
)

const (
	unknownCELPolicyID   = ""
	unknownCELExpression = ""
	unknownPolicySource  = ""
	lshBandColumnCount   = 5
	sqliteBatchSize      = 900
)

func deleteTraceRows(ctx context.Context, transaction *sql.Tx, traceID string) error {
	for _, statement := range []string{
		"DELETE FROM code_intel_fts WHERE trace_id = ?",
		"DELETE FROM hook_targets WHERE trace_id = ?",
		"DELETE FROM hook_decisions WHERE trace_id = ?",
		"DELETE FROM hook_events WHERE trace_id = ?",
		"DELETE FROM remediation_events WHERE trace_id = ?",
		"DELETE FROM remediation_occurrences WHERE trace_id = ?",
		"DELETE FROM finding_occurrences WHERE trace_id = ?",
		"DELETE FROM traces WHERE trace_id = ?",
	} {
		_, inlineErrA := transaction.ExecContext(ctx, statement, traceID)
		if inlineErrA != nil {
			return fmt.Errorf("delete existing trace rows: %w", inlineErrA)
		}
	}

	return nil
}

func traceExists(
	ctx context.Context,
	transaction *sql.Tx,
	traceID string,
) (bool, error) {
	row := transaction.QueryRowContext(
		ctx,
		"SELECT 1 FROM traces WHERE trace_id = ? LIMIT 1",
		traceID,
	)

	var exists int

	err := row.Scan(&exists)
	if err == nil {
		return true, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}

	return false, fmt.Errorf("lookup trace %q: %w", traceID, err)
}

func insertTrace(ctx context.Context, transaction *sql.Tx, trace Trace) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO traces(
			trace_id, trace_kind, recorded_at_utc, repo_root, cwd, provider,
			event, tool, status, source_path, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.ID,
		trace.Kind,
		trace.RecordedAtUTC,
		trace.RepoRoot,
		trace.Cwd,
		trace.Provider,
		trace.Event,
		trace.Tool,
		trace.Status,
		trace.SourcePath,
		string(trace.Raw),
	)
	if err != nil {
		return fmt.Errorf("insert trace %q: %w", trace.ID, err)
	}

	return nil
}

func insertFindings(ctx context.Context, transaction *sql.Tx, trace Trace) error {
	for index, finding := range trace.Findings {
		err := insertFinding(ctx, transaction, trace, index, finding)
		if err != nil {
			return err
		}
	}

	return nil
}

func insertFinding(
	ctx context.Context,
	transaction *sql.Tx,
	trace Trace,
	index int,
	finding evidence.Finding,
) error {
	err := upsertFinding(ctx, transaction, finding)
	if err != nil {
		return err
	}

	err = insertFindingOccurrence(ctx, transaction, trace, index, finding)
	if err != nil {
		return err
	}

	return insertFTS(ctx, transaction, findingFTSRow(trace, finding))
}

func upsertFinding(
	ctx context.Context,
	transaction *sql.Tx,
	finding evidence.Finding,
) error {
	raw, err := json.Marshal(finding)
	if err != nil {
		return fmt.Errorf("marshal finding %q: %w", finding.ID, err)
	}

	_, err = transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO findings(
			finding_id, rule_id, tool, code, message, severity, policy_id,
			skill_id, evaluator_kind, cel_policy_id, cel_expression,
			policy_source, path, language, symbol_kind, symbol_name,
			search_text, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		finding.ID,
		finding.RuleID,
		finding.Tool,
		finding.Code,
		finding.Message,
		finding.Severity,
		finding.PolicyID,
		finding.SkillID,
		finding.EvaluatorKind,
		unknownCELPolicyID,
		unknownCELExpression,
		unknownPolicySource,
		finding.SourceSpan.Path,
		finding.SourceSpan.Language,
		finding.SourceSpan.SymbolKind,
		finding.SourceSpan.SymbolName,
		finding.SearchText,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert finding %q: %w", finding.ID, err)
	}

	return nil
}

func insertFindingOccurrence(
	ctx context.Context,
	transaction *sql.Tx,
	trace Trace,
	index int,
	finding evidence.Finding,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO finding_occurrences(
			trace_id, ordinal, finding_id, policy_id, skill_id, path, recorded_at_utc
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		trace.ID,
		index,
		finding.ID,
		finding.PolicyID,
		finding.SkillID,
		finding.SourceSpan.Path,
		trace.RecordedAtUTC,
	)
	if err != nil {
		return fmt.Errorf("insert finding occurrence %q: %w", finding.ID, err)
	}

	return nil
}

func findingFTSRow(trace Trace, finding evidence.Finding) ftsRow {
	return ftsRow{
		Kind:     "finding",
		RecordID: finding.ID,
		TraceID:  trace.ID,
		PolicyID: finding.PolicyID,
		SkillID:  finding.SkillID,
		Path:     finding.SourceSpan.Path,
		Message:  finding.Message,
		SearchText: diagnosticSearchText(
			finding.SearchText,
			finding.RuleID,
			finding.Code,
			finding.Message,
			finding.SourceSpan.SymbolName,
			finding.SourceSpan.SymbolKind,
		),
	}
}

func insertSARIFRun(ctx context.Context, transaction *sql.Tx, run SARIFRun) error {
	_, inlineErrD := transaction.ExecContext(
		ctx,
		"DELETE FROM code_intel_fts WHERE kind = 'sarif_result' AND trace_id = ?",
		run.ID,
	)
	if inlineErrD != nil {
		return fmt.Errorf("delete existing SARIF FTS rows: %w", inlineErrD)
	}

	_, inlineErrE := transaction.ExecContext(
		ctx,
		"DELETE FROM sarif_runs WHERE sarif_run_id = ?",
		run.ID,
	)
	if inlineErrE != nil {
		return fmt.Errorf("delete existing SARIF run %q: %w", run.ID, inlineErrE)
	}

	_, inlineErrF := transaction.ExecContext(
		ctx,
		`INSERT INTO sarif_runs(
			sarif_run_id, trace_id, source_path, category, tool_name,
			automation_id, run_guid, baseline_guid, produced_at_utc, raw_json
		) VALUES (?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.TraceID,
		run.SourcePath,
		run.Category,
		run.ToolName,
		run.AutomationID,
		run.RunGUID,
		run.BaselineGUID,
		run.ProducedAtUTC,
		string(run.Raw),
	)
	if inlineErrF != nil {
		return fmt.Errorf("insert SARIF run %q: %w", run.ID, inlineErrF)
	}

	for index, result := range run.Results {
		err := insertSARIFResult(ctx, transaction, run.ID, index, result)
		if err != nil {
			return err
		}
	}

	return nil
}

func insertSARIFResult(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	index int,
	result SARIFResultReference,
) error {
	result = prepareSARIFResult(ctx, transaction, result)

	raw, err := json.Marshal(result.Raw)
	if err != nil {
		return fmt.Errorf("marshal SARIF result %q: %w", result.ID, err)
	}

	err = upsertSARIFResult(ctx, transaction, runID, index, result, string(raw))
	if err != nil {
		return err
	}

	err = insertSARIFASTLink(ctx, transaction, result)
	if err != nil {
		return err
	}

	return insertFTS(ctx, transaction, sarifResultFTSRow(runID, result))
}

func prepareSARIFResult(
	ctx context.Context,
	transaction *sql.Tx,
	result SARIFResultReference,
) SARIFResultReference {
	result.LinkedChunkID = firstNonEmpty(
		result.LinkedChunkID,
		linkedChunkID(ctx, transaction, result),
	)
	result.SearchText = diagnosticSearchText(
		result.SearchText,
		result.RuleID,
		result.Message,
		result.PolicyID,
		result.SkillID,
		result.ASTSymbolName,
		result.ASTSymbolKind,
		result.ASTSymbolPath,
	)

	return result
}

func upsertSARIFResult(
	ctx context.Context,
	transaction *sql.Tx,
	runID string,
	index int,
	result SARIFResultReference,
	raw string,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO sarif_results(
			sarif_result_id, sarif_run_id, ordinal, rule_id, level, message,
			fingerprint, proxy_event_id, proxy_session_id, proxy_event_kind,
			proxy_direction, proxy_payload_kind, proxy_trace_id,
			proxy_tracking_id, proxy_transform, finding_id, remediation_id,
			policy_id, skill_id,
			principle_ids, path, ast_language, ast_node_kind, ast_symbol_kind,
			ast_symbol_name, ast_symbol_path, linked_chunk_id,
			start_line, start_column, evaluator_kind,
			cel_policy_id, cel_expression, policy_source, search_text, raw_json
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?, ?, ?
		)`,
		sarifResultSQLArgs(runID, index, result, raw)...,
	)
	if err != nil {
		return fmt.Errorf("insert SARIF result %q: %w", result.ID, err)
	}

	return nil
}

func sarifResultSQLArgs(
	runID string,
	index int,
	result SARIFResultReference,
	raw string,
) []any {
	return []any{
		result.ID, runID, index, result.RuleID, result.Level, result.Message,
		result.Fingerprint, result.ProxyEventID, result.ProxySessionID,
		result.ProxyEventKind, result.ProxyDirection, result.ProxyPayloadKind,
		result.ProxyTraceID, result.ProxyTrackingID, result.ProxyTransform,
		result.FindingID, result.RemediationID,
		result.PolicyID, result.SkillID, strings.Join(result.PrincipleIDs, ","),
		result.Path, result.ASTLanguage, result.ASTNodeKind, result.ASTSymbolKind,
		result.ASTSymbolName, result.ASTSymbolPath, result.LinkedChunkID,
		result.StartLine, result.StartColumn, result.EvaluatorKind,
		result.CELPolicyID, result.CELExpression, result.PolicySource,
		result.SearchText, raw,
	}
}

func insertSARIFASTLink(
	ctx context.Context,
	transaction *sql.Tx,
	result SARIFResultReference,
) error {
	if result.LinkedChunkID == "" {
		return nil
	}

	return insertASTFindingLink(ctx, transaction, ASTFindingLink{
		ID: stableID(
			"ast-finding-link",
			"sarif_result",
			result.ID,
			result.LinkedChunkID,
		),
		FindingKind: "sarif_result",
		FindingID:   result.ID,
		ChunkID:     result.LinkedChunkID,
		Path:        result.Path,
		PolicyID:    result.PolicyID,
		SkillID:     result.SkillID,
		SymbolPath:  result.ASTSymbolPath,
	})
}

func sarifResultFTSRow(runID string, result SARIFResultReference) ftsRow {
	return ftsRow{
		Kind:       "sarif_result",
		RecordID:   result.ID,
		TraceID:    runID,
		PolicyID:   result.PolicyID,
		SkillID:    result.SkillID,
		Path:       result.Path,
		Message:    result.Message,
		SearchText: result.SearchText,
	}
}

func insertRemediationOutcome(
	ctx context.Context,
	transaction *sql.Tx,
	outcome RemediationOutcome,
) error {
	raw, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("marshal remediation outcome %q: %w", outcome.ID, err)
	}

	_, inlineErrH := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO remediation_outcomes(
			outcome_id, remediation_id, finding_id, source_trace_id,
			followup_trace_id, policy_id, skill_id, file, path, provider,
			tool, outcome, attempt_ordinal, recorded_at_utc, search_text, raw_json
		) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		outcome.ID,
		outcome.RemediationID,
		outcome.FindingID,
		outcome.SourceTraceID,
		outcome.FollowupTraceID,
		outcome.PolicyID,
		outcome.SkillID,
		outcome.File,
		outcome.Path,
		outcome.Provider,
		outcome.Tool,
		outcome.Outcome,
		outcome.AttemptOrdinal,
		outcome.RecordedAtUTC,
		outcome.SearchText,
		string(raw),
	)
	if inlineErrH != nil {
		return fmt.Errorf("insert remediation outcome %q: %w", outcome.ID, inlineErrH)
	}

	return insertFTS(ctx, transaction, ftsRow{
		Kind:       "remediation_outcome",
		RecordID:   outcome.ID,
		TraceID:    firstNonEmpty(outcome.FollowupTraceID, outcome.SourceTraceID),
		PolicyID:   outcome.PolicyID,
		SkillID:    outcome.SkillID,
		Path:       firstNonEmpty(outcome.File, outcome.Path),
		Message:    outcome.Outcome,
		SearchText: outcome.SearchText,
	})
}

func insertEmbeddingRecord(
	ctx context.Context,
	transaction *sql.Tx,
	record EmbeddingRecord,
) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal embedding record %q: %w", record.ID, err)
	}

	_, inlineErrI := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO embedding_records(
			embedding_id, backend, collection, model_id, dimension, input_kind,
			record_kind, record_id, trace_id, policy_id, skill_id, path,
			content_hash, provider, backend_row_id, created_at_utc, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID,
		record.Backend,
		record.Collection,
		record.ModelID,
		record.Dimension,
		record.InputKind,
		record.RecordKind,
		record.RecordID,
		record.TraceID,
		record.PolicyID,
		record.SkillID,
		record.Path,
		record.ContentHash,
		record.Provider,
		record.BackendRowID,
		record.CreatedAtUTC,
		string(raw),
	)
	if inlineErrI != nil {
		return fmt.Errorf("insert embedding record %q: %w", record.ID, inlineErrI)
	}

	return insertFTS(ctx, transaction, ftsRow{
		Kind:       "embedding_record",
		RecordID:   record.ID,
		TraceID:    record.TraceID,
		PolicyID:   record.PolicyID,
		SkillID:    record.SkillID,
		Path:       record.Path,
		Message:    record.RecordKind,
		SearchText: embeddingSearchText(record),
	})
}

func replaceCodeFileChunks(
	ctx context.Context,
	transaction *sql.Tx,
	file CodeFile,
	chunks []CodeChunk,
	edges []CodeEdge,
) error {
	existingChunks, existingEdges, err := existingEntitiesForPath(
		ctx,
		transaction,
		file.Path,
	)
	if err != nil {
		return err
	}

	err = DeleteLSHBandsForPath(ctx, transaction, file.Path)
	if err != nil {
		return err
	}

	err = upsertCodeFile(ctx, transaction, file)
	if err != nil {
		return err
	}

	err = reconcileCodeChunks(ctx, transaction, chunks, existingChunks)
	if err != nil {
		return err
	}

	err = storeLSHBandsForChunks(ctx, transaction, file.Path, chunks)
	if err != nil {
		return err
	}

	return reconcileCodeEdges(ctx, transaction, edges, existingEdges)
}

func storeLSHBandsForChunks(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
	chunks []CodeChunk,
) error {
	config := minhash.DefaultConfig()
	rows := []lshBandRow{}

	for _, chunk := range chunks {
		if len(chunk.MinHashSig) == 0 {
			continue
		}

		sig := minhash.Signature{Values: chunk.MinHashSig}
		bandHashes := minhash.BandHashes(sig, config)

		for bandIndex, bandHash := range bandHashes {
			rows = append(rows, lshBandRow{
				BandHash:   bandHash,
				BandIndex:  bandIndex,
				ChunkID:    chunk.ID,
				Path:       path,
				SymbolName: chunk.SymbolName,
			})
		}
	}

	return storeLSHBandRows(ctx, transaction, rows)
}

type lshBandRow struct {
	BandHash   string
	ChunkID    string
	Path       string
	SymbolName string
	BandIndex  int
}

func storeLSHBandRows(
	ctx context.Context,
	transaction *sql.Tx,
	rows []lshBandRow,
) error {
	const batchSize = 100

	for start := 0; start < len(rows); start += batchSize {
		end := min(start+batchSize, len(rows))
		batch := rows[start:end]

		placeholders := make([]string, 0, len(batch))

		args := make([]any, 0, len(batch)*lshBandColumnCount)
		for _, row := range batch {
			placeholders = append(placeholders, "(?, ?, ?, ?, ?)")
			args = append(
				args,
				row.BandHash,
				row.BandIndex,
				row.ChunkID,
				row.Path,
				row.SymbolName,
			)
		}

		// #nosec G202 -- placeholders are generated from a fixed batch shape.
		_, err := transaction.ExecContext(
			ctx,
			`INSERT OR IGNORE INTO lsh_bands(
				band_hash, band_index, chunk_id, path, symbol_name
			) VALUES `+strings.Join(placeholders, ", "),
			args...,
		)
		if err != nil {
			return fmt.Errorf("store LSH band batch: %w", err)
		}
	}

	return nil
}

func existingEntitiesForPath(
	ctx context.Context,
	transaction *sql.Tx,
	path string,
) (map[string]bool, map[string]bool, error) {
	existingChunkIDs := make(map[string]bool)
	existingEdgeIDs := make(map[string]bool)

	rows, err := transaction.QueryContext(
		ctx,
		"SELECT chunk_id FROM code_chunks WHERE path = ?",
		path,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query existing chunk IDs: %w", err)
	}

	defer rows.Close()

	for rows.Next() {
		var chunkID string

		err = rows.Scan(&chunkID)
		if err != nil {
			return nil, nil, fmt.Errorf("scan existing chunk ID: %w", err)
		}

		existingChunkIDs[chunkID] = true
	}

	err = rows.Err()
	if err != nil {
		return nil, nil, fmt.Errorf("iterate existing chunk IDs: %w", err)
	}

	edgeRows, err := transaction.QueryContext(
		ctx,
		"SELECT edge_id FROM code_edges WHERE path = ?",
		path,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("query existing edge IDs: %w", err)
	}

	defer edgeRows.Close()

	for edgeRows.Next() {
		var edgeID string

		err = edgeRows.Scan(&edgeID)
		if err != nil {
			return nil, nil, fmt.Errorf("scan existing edge ID: %w", err)
		}

		existingEdgeIDs[edgeID] = true
	}

	err = edgeRows.Err()
	if err != nil {
		return nil, nil, fmt.Errorf("iterate existing edge IDs: %w", err)
	}

	return existingChunkIDs, existingEdgeIDs, nil
}

func reconcileCodeChunks(
	ctx context.Context,
	transaction *sql.Tx,
	newChunks []CodeChunk,
	existingChunkIDs map[string]bool,
) error {
	newChunkIDs := make(map[string]bool)

	for _, chunk := range newChunks {
		newChunkIDs[chunk.ID] = true

		// Always upsert to update start_line/end_line if a chunk moved but content
		// stayed same.
		err := insertCodeChunk(ctx, transaction, chunk)
		if err != nil {
			return err
		}
	}

	var obsoleteChunkIDs []any

	for chunkID := range existingChunkIDs {
		if !newChunkIDs[chunkID] {
			obsoleteChunkIDs = append(obsoleteChunkIDs, chunkID)
		}
	}

	if len(obsoleteChunkIDs) == 0 {
		return nil
	}

	err := deleteEmbeddingRecordsForCodeChunks(ctx, transaction, obsoleteChunkIDs)
	if err != nil {
		return err
	}

	err = batchDeleteEntities(
		ctx, transaction, "code_chunks", "chunk_id", obsoleteChunkIDs,
	)
	if err != nil {
		return err
	}

	err = batchDeleteEntities(
		ctx,
		transaction,
		"code_intel_fts",
		"record_id",
		obsoleteChunkIDs,
		"kind = 'code_chunk'",
	)
	if err != nil {
		return err
	}

	return batchDeleteEntities(
		ctx, transaction, "ast_finding_links", "chunk_id", obsoleteChunkIDs,
	)
}

func deleteEmbeddingRecordsForCodeChunks(
	ctx context.Context,
	transaction *sql.Tx,
	chunkIDs []any,
) error {
	if len(chunkIDs) == 0 {
		return nil
	}

	embeddingIDs, err := embeddingIDsForCodeChunks(ctx, transaction, chunkIDs)
	if err != nil {
		return err
	}

	if len(embeddingIDs) > 0 {
		err = batchDeleteEntities(
			ctx,
			transaction,
			"code_intel_fts",
			"record_id",
			embeddingIDs,
			"kind = 'embedding_record'",
			"message = 'code_chunk'",
		)
		if err != nil {
			return err
		}
	}

	return batchDeleteEntities(
		ctx,
		transaction,
		"embedding_records",
		"record_id",
		chunkIDs,
		"record_kind = 'code_chunk'",
	)
}

func embeddingIDsForCodeChunks(
	ctx context.Context,
	transaction *sql.Tx,
	chunkIDs []any,
) ([]any, error) {
	if len(chunkIDs) == 0 {
		return nil, nil
	}

	embeddingIDs := []any{}

	for offset := 0; offset < len(chunkIDs); offset += sqliteBatchSize {
		end := min(offset+sqliteBatchSize, len(chunkIDs))
		batch := chunkIDs[offset:end]

		batchIDs, err := embeddingIDsForCodeChunkBatch(ctx, transaction, batch)
		if err != nil {
			return nil, err
		}

		embeddingIDs = append(embeddingIDs, batchIDs...)
	}

	return embeddingIDs, nil
}

func embeddingIDsForCodeChunkBatch(
	ctx context.Context,
	transaction *sql.Tx,
	chunkIDs []any,
) ([]any, error) {
	placeholders := make([]string, len(chunkIDs))
	for index := range placeholders {
		placeholders[index] = "?"
	}

	// #nosec G202 -- placeholders are generated internally for bound parameters.
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT embedding_id
		FROM embedding_records
		WHERE record_kind = 'code_chunk'
			AND record_id IN (`+strings.Join(placeholders, ",")+`)`,
		chunkIDs...,
	)
	if err != nil {
		return nil, fmt.Errorf("query code chunk embedding IDs: %w", err)
	}
	defer rows.Close()

	embeddingIDs := []any{}

	for rows.Next() {
		var embeddingID string

		err = rows.Scan(&embeddingID)
		if err != nil {
			return nil, fmt.Errorf("scan code chunk embedding ID: %w", err)
		}

		embeddingIDs = append(embeddingIDs, embeddingID)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate code chunk embedding IDs: %w", err)
	}

	return embeddingIDs, nil
}

func reconcileCodeEdges(
	ctx context.Context,
	transaction *sql.Tx,
	newEdges []CodeEdge,
	existingEdgeIDs map[string]bool,
) error {
	newEdgeIDs := make(map[string]bool)

	for _, edge := range newEdges {
		newEdgeIDs[edge.ID] = true

		// Always upsert to refresh metadata (target path/name, raw text)
		// even if the ID is stable.
		err := insertCodeEdge(ctx, transaction, edge)
		if err != nil {
			return err
		}
	}

	var obsoleteEdgeIDs []any

	for edgeID := range existingEdgeIDs {
		if !newEdgeIDs[edgeID] {
			obsoleteEdgeIDs = append(obsoleteEdgeIDs, edgeID)
		}
	}

	if len(obsoleteEdgeIDs) > 0 {
		return batchDeleteEntities(
			ctx, transaction, "code_edges", "edge_id", obsoleteEdgeIDs,
		)
	}

	return nil
}

func batchDeleteEntities(
	ctx context.Context,
	transaction *sql.Tx,
	table string,
	column string,
	ids []any,
	extraWhere ...string,
) error {
	// SQLite has a limit of 999 parameters.
	for offset := 0; offset < len(ids); offset += sqliteBatchSize {
		end := min(offset+sqliteBatchSize, len(ids))
		batch := ids[offset:end]

		placeholders := make([]string, len(batch))
		for j := range placeholders {
			placeholders[j] = "?"
		}

		where := fmt.Sprintf("%s IN (%s)", column, strings.Join(placeholders, ","))
		if len(extraWhere) > 0 {
			where = fmt.Sprintf("%s AND %s", strings.Join(extraWhere, " AND "), where)
		}

		// #nosec G201 -- table and column names are internal literals.
		query := fmt.Sprintf("DELETE FROM %s WHERE %s", table, where)

		_, err := transaction.ExecContext(ctx, query, batch...)
		if err != nil {
			return fmt.Errorf("batch delete from %s: %w", table, err)
		}
	}

	return nil
}

func upsertCodeFile(
	ctx context.Context,
	transaction *sql.Tx,
	file CodeFile,
) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO code_files(
			path, language, content_hash, parser_name, parser_version,
			source_mtime_utc, deleted_at_utc, size_bytes, line_count,
			indexed_at_utc, stale_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		file.Path,
		file.Language,
		file.ContentHash,
		file.ParserName,
		file.ParserVersion,
		file.SourceModTimeUTC,
		file.DeletedAtUTC,
		file.SizeBytes,
		file.LineCount,
		file.IndexedAtUTC,
		file.StaleReason,
	)
	if err != nil {
		return fmt.Errorf("upsert code file %q: %w", file.Path, err)
	}

	return nil
}

func insertCodeChunk(ctx context.Context, transaction *sql.Tx, chunk CodeChunk) error {
	_, inlineErrO := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO code_chunks(
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, parent_symbol_path, parent_chunk_id, start_byte, end_byte, start_line,
			end_line, content_hash, normalized_hash, minhash_sig, search_text, raw_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		chunk.ID,
		chunk.Path,
		chunk.Language,
		chunk.NodeKind,
		chunk.SymbolKind,
		chunk.SymbolName,
		chunk.SymbolPath,
		chunk.ParentSymbolPath,
		chunk.ParentChunkID,
		chunk.StartByte,
		chunk.EndByte,
		chunk.StartLine,
		chunk.EndLine,
		chunk.ContentHash,
		chunk.NormalizedHash,
		packMinHashSig(chunk.MinHashSig),
		chunk.SearchText,
		chunk.RawText,
	)
	if inlineErrO != nil {
		return fmt.Errorf("insert code chunk %q: %w", chunk.ID, inlineErrO)
	}

	return insertFTS(ctx, transaction, ftsRow{
		Kind:     "code_chunk",
		RecordID: chunk.ID,
		Path:     chunk.Path,
		Message: strings.Join(
			compactStrings([]string{chunk.SymbolKind, chunk.SymbolName}),
			" ",
		),
		SearchText: chunk.SearchText,
	})
}

func insertCodeEdge(ctx context.Context, transaction *sql.Tx, edge CodeEdge) error {
	_, inlineErrP := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO code_edges(
			edge_id, edge_kind, path, source_chunk_id, target_path,
			target_chunk_id, target_symbol_path, target_name, raw_text
		) VALUES (?, ?, ?, NULLIF(?, ''), ?, NULLIF(?, ''), ?, ?, ?)`,
		edge.ID,
		edge.Kind,
		edge.Path,
		edge.SourceChunkID,
		edge.TargetPath,
		edge.TargetChunkID,
		edge.TargetSymbolPath,
		edge.TargetName,
		edge.RawText,
	)
	if inlineErrP != nil {
		return fmt.Errorf("insert code edge %q: %w", edge.ID, inlineErrP)
	}

	return nil
}

func insertASTFindingLink(
	ctx context.Context,
	transaction *sql.Tx,
	link ASTFindingLink,
) error {
	_, inlineErrQ := transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO ast_finding_links(
			link_id, finding_kind, finding_id, chunk_id, path, policy_id,
			skill_id, symbol_path, content_hash, stale
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID,
		link.FindingKind,
		link.FindingID,
		link.ChunkID,
		link.Path,
		link.PolicyID,
		link.SkillID,
		link.SymbolPath,
		link.ContentHash,
		boolInt(link.Stale),
	)
	if inlineErrQ != nil {
		return fmt.Errorf("insert AST finding link %q: %w", link.ID, inlineErrQ)
	}

	return nil
}

func linkedChunkID(
	ctx context.Context,
	transaction *sql.Tx,
	result SARIFResultReference,
) string {
	if result.Path == "" || result.ASTSymbolPath == "" {
		return ""
	}

	row := transaction.QueryRowContext(
		ctx,
		`SELECT chunk_id FROM code_chunks
		WHERE path = ?
			AND symbol_path = ?
			AND (? = '' OR node_kind = ?)
		ORDER BY start_line
		LIMIT 1`,
		result.Path,
		result.ASTSymbolPath,
		result.ASTNodeKind,
		result.ASTNodeKind,
	)

	var recordID string

	err := row.Scan(&recordID)
	if err != nil {
		return ""
	}

	return recordID
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func insertRemediations(ctx context.Context, transaction *sql.Tx, trace Trace) error {
	for index, remediation := range trace.AgentRemediation {
		recordID := remediation.ID
		if strings.TrimSpace(recordID) == "" {
			recordID = fmt.Sprintf("%s:remediation:%d", trace.ID, index)
		}

		search := remediationSearchText(remediation)

		raw, err := json.Marshal(remediation)
		if err != nil {
			return fmt.Errorf("marshal remediation %q: %w", recordID, err)
		}

		_, inlineErrR := transaction.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO remediations(
				remediation_id, policy_id, skill_id, file, path, message,
				advice, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			recordID,
			remediation.PolicyID,
			remediation.SkillID,
			remediation.File,
			remediation.Path,
			remediation.Message,
			remediation.Advice,
			search,
			string(raw),
		)
		if inlineErrR != nil {
			return fmt.Errorf("insert remediation %q: %w", recordID, inlineErrR)
		}

		_, inlineErrS := transaction.ExecContext(
			ctx,
			`INSERT INTO remediation_occurrences(
				trace_id, ordinal, remediation_id, policy_id, skill_id,
				file, path, line, recorded_at_utc
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			trace.ID,
			index,
			recordID,
			remediation.PolicyID,
			remediation.SkillID,
			remediation.File,
			remediation.Path,
			remediation.Line,
			trace.RecordedAtUTC,
		)
		if inlineErrS != nil {
			return fmt.Errorf(
				"insert remediation occurrence %q: %w",
				recordID,
				inlineErrS,
			)
		}

		inlineErr1 := insertFTS(ctx, transaction, ftsRow{
			Kind:       "remediation",
			RecordID:   recordID,
			TraceID:    trace.ID,
			PolicyID:   remediation.PolicyID,
			SkillID:    remediation.SkillID,
			Path:       firstNonEmpty(remediation.File, remediation.Path),
			Message:    remediation.Message,
			SearchText: search,
		})
		if inlineErr1 != nil {
			return inlineErr1
		}
	}

	return nil
}

func insertRemediationEvents(
	ctx context.Context,
	transaction *sql.Tx,
	trace Trace,
) error {
	for index, event := range trace.RemediationEvents {
		recordID := event.ID
		if strings.TrimSpace(recordID) == "" {
			recordID = fmt.Sprintf("%s:remediation-event:%d", trace.ID, index)
		}

		raw, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal remediation event %q: %w", recordID, err)
		}

		_, inlineErrT := transaction.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO remediation_events(
				event_id, trace_id, remediation_id, finding_id, event,
				policy_id, skill_id, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			recordID,
			trace.ID,
			event.RemediationID,
			event.FindingID,
			event.Event,
			event.PolicyID,
			event.SkillID,
			event.SearchText,
			string(raw),
		)
		if inlineErrT != nil {
			return fmt.Errorf("insert remediation event %q: %w", recordID, inlineErrT)
		}
	}

	return nil
}

type ftsRow struct {
	Kind       string
	RecordID   string
	TraceID    string
	PolicyID   string
	SkillID    string
	Path       string
	Message    string
	SearchText string
}

func insertFTS(ctx context.Context, transaction *sql.Tx, row ftsRow) error {
	_, err := transaction.ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(
			kind, record_id, trace_id, policy_id, skill_id, path, message, search_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Kind,
		row.RecordID,
		row.TraceID,
		row.PolicyID,
		row.SkillID,
		row.Path,
		row.Message,
		row.SearchText,
	)
	if err != nil {
		return fmt.Errorf("insert code intelligence FTS row: %w", err)
	}

	return nil
}

func remediationSearchText(remediation agentmsg.Remediation) string {
	return strings.Join(compactStrings([]string{
		remediation.PolicyID,
		remediation.SkillID,
		remediation.Message,
		remediation.Advice,
		remediation.Command,
		remediation.File,
		remediation.Path,
		strings.Join(remediation.NextSteps, " "),
	}), "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func compactStrings(values []string) []string {
	result := []string{}

	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

func stableID(prefix string, values ...string) string {
	hash := sha256.New()

	_, _ = hash.Write([]byte(prefix))
	for _, value := range values {
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write([]byte(strings.TrimSpace(value)))
	}

	return prefix + ":" + hex.EncodeToString(hash.Sum(nil))[:24]
}

var _ evidence.TraceIngestor = TraceIngester{}
