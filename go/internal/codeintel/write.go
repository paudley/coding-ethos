// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	unknownCELPolicyID   = ""
	unknownCELExpression = ""
	unknownPolicySource  = ""
)

func deleteTraceRows(ctx context.Context, tx *sql.Tx, traceID string) error {
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
		if _, err := tx.ExecContext(ctx, statement, traceID); err != nil {
			return fmt.Errorf("delete existing trace rows: %w", err)
		}
	}

	return nil
}

func insertTrace(ctx context.Context, tx *sql.Tx, trace Trace) error {
	_, err := tx.ExecContext(
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

func insertFindings(ctx context.Context, tx *sql.Tx, trace Trace) error {
	for index, finding := range trace.Findings {
		raw, err := json.Marshal(finding)
		if err != nil {
			return fmt.Errorf("marshal finding %q: %w", finding.ID, err)
		}
		if _, err := tx.ExecContext(
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
		); err != nil {
			return fmt.Errorf("insert finding %q: %w", finding.ID, err)
		}
		if _, err := tx.ExecContext(
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
		); err != nil {
			return fmt.Errorf("insert finding occurrence %q: %w", finding.ID, err)
		}
		if err := insertFTS(ctx, tx, ftsRow{
			Kind:       "finding",
			RecordID:   finding.ID,
			TraceID:    trace.ID,
			PolicyID:   finding.PolicyID,
			SkillID:    finding.SkillID,
			Path:       finding.SourceSpan.Path,
			Message:    finding.Message,
			SearchText: finding.SearchText,
		}); err != nil {
			return err
		}
	}

	return nil
}

func insertSARIFRun(ctx context.Context, tx *sql.Tx, run SARIFRun) error {
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM code_intel_fts WHERE kind = 'sarif_result' AND trace_id = ?",
		run.ID,
	); err != nil {
		return fmt.Errorf("delete existing SARIF FTS rows: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM sarif_runs WHERE sarif_run_id = ?",
		run.ID,
	); err != nil {
		return fmt.Errorf("delete existing SARIF run %q: %w", run.ID, err)
	}
	if _, err := tx.ExecContext(
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
	); err != nil {
		return fmt.Errorf("insert SARIF run %q: %w", run.ID, err)
	}
	for index, result := range run.Results {
		if err := insertSARIFResult(ctx, tx, run.ID, index, result); err != nil {
			return err
		}
	}

	return nil
}

func insertSARIFResult(
	ctx context.Context,
	tx *sql.Tx,
	runID string,
	index int,
	result SARIFResultReference,
) error {
	result.LinkedChunkID = firstNonEmpty(result.LinkedChunkID, linkedChunkID(ctx, tx, result))
	raw, err := json.Marshal(result.Raw)
	if err != nil {
		return fmt.Errorf("marshal SARIF result %q: %w", result.ID, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO sarif_results(
			sarif_result_id, sarif_run_id, ordinal, rule_id, level, message,
			fingerprint, finding_id, remediation_id, policy_id, skill_id,
			principle_ids, path, ast_language, ast_node_kind, ast_symbol_kind,
			ast_symbol_name, ast_symbol_path, linked_chunk_id,
			start_line, start_column, evaluator_kind,
			cel_policy_id, cel_expression, policy_source, search_text, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		result.ID,
		runID,
		index,
		result.RuleID,
		result.Level,
		result.Message,
		result.Fingerprint,
		result.FindingID,
		result.RemediationID,
		result.PolicyID,
		result.SkillID,
		strings.Join(result.PrincipleIDs, ","),
		result.Path,
		result.ASTLanguage,
		result.ASTNodeKind,
		result.ASTSymbolKind,
		result.ASTSymbolName,
		result.ASTSymbolPath,
		result.LinkedChunkID,
		result.StartLine,
		result.StartColumn,
		result.EvaluatorKind,
		result.CELPolicyID,
		result.CELExpression,
		result.PolicySource,
		result.SearchText,
		string(raw),
	); err != nil {
		return fmt.Errorf("insert SARIF result %q: %w", result.ID, err)
	}
	if result.LinkedChunkID != "" {
		if err := insertASTFindingLink(ctx, tx, ASTFindingLink{
			ID:          stableID("ast-finding-link", "sarif_result", result.ID, result.LinkedChunkID),
			FindingKind: "sarif_result",
			FindingID:   result.ID,
			ChunkID:     result.LinkedChunkID,
			Path:        result.Path,
			PolicyID:    result.PolicyID,
			SkillID:     result.SkillID,
			SymbolPath:  result.ASTSymbolPath,
		}); err != nil {
			return err
		}
	}
	return insertFTS(ctx, tx, ftsRow{
		Kind:       "sarif_result",
		RecordID:   result.ID,
		TraceID:    runID,
		PolicyID:   result.PolicyID,
		SkillID:    result.SkillID,
		Path:       result.Path,
		Message:    result.Message,
		SearchText: result.SearchText,
	})
}

func insertRemediationOutcome(ctx context.Context, tx *sql.Tx, outcome RemediationOutcome) error {
	raw, err := json.Marshal(outcome)
	if err != nil {
		return fmt.Errorf("marshal remediation outcome %q: %w", outcome.ID, err)
	}
	if _, err := tx.ExecContext(
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
	); err != nil {
		return fmt.Errorf("insert remediation outcome %q: %w", outcome.ID, err)
	}
	return insertFTS(ctx, tx, ftsRow{
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

func insertEmbeddingRecord(ctx context.Context, tx *sql.Tx, record EmbeddingRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal embedding record %q: %w", record.ID, err)
	}
	if _, err := tx.ExecContext(
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
	); err != nil {
		return fmt.Errorf("insert embedding record %q: %w", record.ID, err)
	}
	return insertFTS(ctx, tx, ftsRow{
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
	tx *sql.Tx,
	file CodeFile,
	chunks []CodeChunk,
	edges []CodeEdge,
) error {
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM code_intel_fts WHERE kind = 'code_chunk' AND path = ?",
		file.Path,
	); err != nil {
		return fmt.Errorf("delete code chunk FTS rows for %q: %w", file.Path, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM code_edges WHERE path = ?",
		file.Path,
	); err != nil {
		return fmt.Errorf("delete code edges for %q: %w", file.Path, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM ast_finding_links WHERE path = ?",
		file.Path,
	); err != nil {
		return fmt.Errorf("delete stale AST finding links for %q: %w", file.Path, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		"DELETE FROM code_chunks WHERE path = ?",
		file.Path,
	); err != nil {
		return fmt.Errorf("delete code chunks for %q: %w", file.Path, err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO code_files(
			path, language, content_hash, parser_name, parser_version,
			size_bytes, line_count, indexed_at_utc, stale_reason
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		file.Path,
		file.Language,
		file.ContentHash,
		file.ParserName,
		file.ParserVersion,
		file.SizeBytes,
		file.LineCount,
		file.IndexedAtUTC,
		file.StaleReason,
	); err != nil {
		return fmt.Errorf("upsert code file %q: %w", file.Path, err)
	}
	for _, chunk := range chunks {
		if err := insertCodeChunk(ctx, tx, chunk); err != nil {
			return err
		}
	}
	for _, edge := range edges {
		if err := insertCodeEdge(ctx, tx, edge); err != nil {
			return err
		}
	}

	return nil
}

func insertCodeChunk(ctx context.Context, tx *sql.Tx, chunk CodeChunk) error {
	if _, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO code_chunks(
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, parent_symbol_path, parent_chunk_id, start_byte, end_byte, start_line,
			end_line, content_hash, search_text, raw_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
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
		chunk.SearchText,
		chunk.RawText,
	); err != nil {
		return fmt.Errorf("insert code chunk %q: %w", chunk.ID, err)
	}
	return insertFTS(ctx, tx, ftsRow{
		Kind:       "code_chunk",
		RecordID:   chunk.ID,
		Path:       chunk.Path,
		Message:    strings.Join(compactStrings([]string{chunk.SymbolKind, chunk.SymbolName}), " "),
		SearchText: chunk.SearchText,
	})
}

func insertCodeEdge(ctx context.Context, tx *sql.Tx, edge CodeEdge) error {
	if _, err := tx.ExecContext(
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
	); err != nil {
		return fmt.Errorf("insert code edge %q: %w", edge.ID, err)
	}

	return nil
}

func insertASTFindingLink(ctx context.Context, tx *sql.Tx, link ASTFindingLink) error {
	if _, err := tx.ExecContext(
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
	); err != nil {
		return fmt.Errorf("insert AST finding link %q: %w", link.ID, err)
	}

	return nil
}

func linkedChunkID(ctx context.Context, tx *sql.Tx, result SARIFResultReference) string {
	if result.Path == "" || result.ASTSymbolPath == "" {
		return ""
	}
	row := tx.QueryRowContext(
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
	var id string
	if err := row.Scan(&id); err != nil {
		return ""
	}

	return id
}

func boolInt(value bool) int {
	if value {
		return 1
	}

	return 0
}

func insertRemediations(ctx context.Context, tx *sql.Tx, trace Trace) error {
	for index, remediation := range trace.AgentRemediation {
		id := remediation.ID
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("%s:remediation:%d", trace.ID, index)
		}
		search := remediationSearchText(remediation)
		raw, err := json.Marshal(remediation)
		if err != nil {
			return fmt.Errorf("marshal remediation %q: %w", id, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO remediations(
				remediation_id, policy_id, skill_id, file, path, message,
				advice, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			remediation.PolicyID,
			remediation.SkillID,
			remediation.File,
			remediation.Path,
			remediation.Message,
			remediation.Advice,
			search,
			string(raw),
		); err != nil {
			return fmt.Errorf("insert remediation %q: %w", id, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO remediation_occurrences(
				trace_id, ordinal, remediation_id, policy_id, skill_id,
				file, path, line, recorded_at_utc
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			trace.ID,
			index,
			id,
			remediation.PolicyID,
			remediation.SkillID,
			remediation.File,
			remediation.Path,
			remediation.Line,
			trace.RecordedAtUTC,
		); err != nil {
			return fmt.Errorf("insert remediation occurrence %q: %w", id, err)
		}
		if err := insertFTS(ctx, tx, ftsRow{
			Kind:       "remediation",
			RecordID:   id,
			TraceID:    trace.ID,
			PolicyID:   remediation.PolicyID,
			SkillID:    remediation.SkillID,
			Path:       firstNonEmpty(remediation.File, remediation.Path),
			Message:    remediation.Message,
			SearchText: search,
		}); err != nil {
			return err
		}
	}

	return nil
}

func insertRemediationEvents(ctx context.Context, tx *sql.Tx, trace Trace) error {
	for index, event := range trace.RemediationEvents {
		id := event.ID
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("%s:remediation-event:%d", trace.ID, index)
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal remediation event %q: %w", id, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO remediation_events(
				event_id, trace_id, remediation_id, finding_id, event,
				policy_id, skill_id, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			trace.ID,
			event.RemediationID,
			event.FindingID,
			event.Event,
			event.PolicyID,
			event.SkillID,
			event.SearchText,
			string(raw),
		); err != nil {
			return fmt.Errorf("insert remediation event %q: %w", id, err)
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

func insertFTS(ctx context.Context, tx *sql.Tx, row ftsRow) error {
	_, err := tx.ExecContext(
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
