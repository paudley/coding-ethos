// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type SARIFRun struct {
	Raw           json.RawMessage        `json:"raw,omitempty"`
	Results       []SARIFResultReference `json:"results,omitempty"`
	ID            string                 `json:"id"`
	TraceID       string                 `json:"trace_id,omitempty"`
	SourcePath    string                 `json:"source_path,omitempty"`
	Category      string                 `json:"category,omitempty"`
	ToolName      string                 `json:"tool_name,omitempty"`
	AutomationID  string                 `json:"automation_id,omitempty"`
	RunGUID       string                 `json:"run_guid,omitempty"`
	BaselineGUID  string                 `json:"baseline_guid,omitempty"`
	ProducedAtUTC string                 `json:"produced_at_utc,omitempty"`
}

type SARIFResultReference struct {
	Raw           json.RawMessage `json:"raw,omitempty"`
	PrincipleIDs  []string        `json:"principle_ids,omitempty"`
	ID            string          `json:"id"`
	RuleID        string          `json:"rule_id,omitempty"`
	Level         string          `json:"level,omitempty"`
	Message       string          `json:"message,omitempty"`
	Fingerprint   string          `json:"fingerprint,omitempty"`
	FindingID     string          `json:"finding_id,omitempty"`
	RemediationID string          `json:"remediation_id,omitempty"`
	PolicyID      string          `json:"policy_id,omitempty"`
	SkillID       string          `json:"skill_id,omitempty"`
	Path          string          `json:"path,omitempty"`
	EvaluatorKind string          `json:"evaluator_kind,omitempty"`
	CELPolicyID   string          `json:"cel_policy_id,omitempty"`
	CELExpression string          `json:"cel_expression,omitempty"`
	PolicySource  string          `json:"policy_source,omitempty"`
	SearchText    string          `json:"search_text,omitempty"`
	StartLine     int             `json:"start_line,omitempty"`
	StartColumn   int             `json:"start_column,omitempty"`
}

type RemediationOutcome struct {
	ID              string `json:"id"`
	RemediationID   string `json:"remediation_id,omitempty"`
	FindingID       string `json:"finding_id,omitempty"`
	SourceTraceID   string `json:"source_trace_id,omitempty"`
	FollowupTraceID string `json:"followup_trace_id,omitempty"`
	PolicyID        string `json:"policy_id,omitempty"`
	SkillID         string `json:"skill_id,omitempty"`
	File            string `json:"file,omitempty"`
	Path            string `json:"path,omitempty"`
	Provider        string `json:"provider,omitempty"`
	Tool            string `json:"tool,omitempty"`
	Outcome         string `json:"outcome"`
	RecordedAtUTC   string `json:"recorded_at_utc,omitempty"`
	SearchText      string `json:"search_text,omitempty"`
	AttemptOrdinal  int    `json:"attempt_ordinal,omitempty"`
}

type EmbeddingRecord struct {
	ID           string `json:"id"`
	Backend      string `json:"backend"`
	Collection   string `json:"collection"`
	ModelID      string `json:"model_id"`
	InputKind    string `json:"input_kind,omitempty"`
	RecordKind   string `json:"record_kind"`
	RecordID     string `json:"record_id"`
	TraceID      string `json:"trace_id,omitempty"`
	PolicyID     string `json:"policy_id,omitempty"`
	SkillID      string `json:"skill_id,omitempty"`
	Path         string `json:"path,omitempty"`
	ContentHash  string `json:"content_hash,omitempty"`
	Provider     string `json:"provider,omitempty"`
	BackendRowID string `json:"backend_row_id,omitempty"`
	CreatedAtUTC string `json:"created_at_utc,omitempty"`
	Dimension    int    `json:"dimension"`
}

type CodeFile struct {
	Path         string `json:"path"`
	Language     string `json:"language"`
	ContentHash  string `json:"content_hash"`
	IndexedAtUTC string `json:"indexed_at_utc"`
	SizeBytes    int    `json:"size_bytes"`
	LineCount    int    `json:"line_count"`
}

type CodeChunk struct {
	ID            string `json:"id"`
	Path          string `json:"path"`
	Language      string `json:"language"`
	NodeKind      string `json:"node_kind"`
	SymbolKind    string `json:"symbol_kind,omitempty"`
	SymbolName    string `json:"symbol_name,omitempty"`
	SymbolPath    string `json:"symbol_path,omitempty"`
	ParentChunkID string `json:"parent_chunk_id,omitempty"`
	ContentHash   string `json:"content_hash"`
	SearchText    string `json:"search_text"`
	RawText       string `json:"raw_text,omitempty"`
	StartByte     int    `json:"start_byte"`
	EndByte       int    `json:"end_byte"`
	StartLine     int    `json:"start_line"`
	EndLine       int    `json:"end_line"`
}

type CodeIndexSummary struct {
	FilesIndexed  int      `json:"files_indexed"`
	ChunksIndexed int      `json:"chunks_indexed"`
	Skipped       []string `json:"skipped,omitempty"`
}

func (store *Store) IngestSARIFRun(ctx context.Context, run SARIFRun) error {
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		return fmt.Errorf("SARIF run id is required")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SARIF ingest: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := insertSARIFRun(ctx, tx, run); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit SARIF ingest: %w", err)
	}

	return nil
}

func (store *Store) RecordRemediationOutcome(
	ctx context.Context,
	outcome RemediationOutcome,
) error {
	outcome = normalizeRemediationOutcome(outcome)
	if outcome.ID == "" {
		return fmt.Errorf("remediation outcome id is required")
	}
	if outcome.Outcome == "" {
		return fmt.Errorf("remediation outcome is required")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remediation outcome write: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := insertRemediationOutcome(ctx, tx, outcome); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit remediation outcome write: %w", err)
	}

	return nil
}

func (store *Store) UpsertEmbeddingRecord(ctx context.Context, record EmbeddingRecord) error {
	record = normalizeEmbeddingRecord(record)
	if record.ID == "" {
		return fmt.Errorf("embedding id is required")
	}
	if record.Backend == "" || record.Collection == "" || record.ModelID == "" {
		return fmt.Errorf("embedding backend, collection, and model id are required")
	}
	if record.RecordKind == "" || record.RecordID == "" {
		return fmt.Errorf("embedding record kind and record id are required")
	}
	if record.Dimension <= 0 {
		return fmt.Errorf("embedding dimension must be positive")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin embedding metadata write: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := insertEmbeddingRecord(ctx, tx, record); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit embedding metadata write: %w", err)
	}

	return nil
}

func (store *Store) ReplaceCodeFileChunks(
	ctx context.Context,
	file CodeFile,
	chunks []CodeChunk,
) error {
	file.Path = strings.TrimSpace(file.Path)
	file.Language = strings.TrimSpace(file.Language)
	file.ContentHash = strings.TrimSpace(file.ContentHash)
	if file.Path == "" || file.Language == "" || file.ContentHash == "" {
		return fmt.Errorf("code file path, language, and content hash are required")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin code chunk write: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := replaceCodeFileChunks(ctx, tx, file, chunks); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit code chunk write: %w", err)
	}

	return nil
}

func normalizeRemediationOutcome(outcome RemediationOutcome) RemediationOutcome {
	outcome.RemediationID = strings.TrimSpace(outcome.RemediationID)
	outcome.FindingID = strings.TrimSpace(outcome.FindingID)
	outcome.SourceTraceID = strings.TrimSpace(outcome.SourceTraceID)
	outcome.FollowupTraceID = strings.TrimSpace(outcome.FollowupTraceID)
	outcome.PolicyID = strings.TrimSpace(outcome.PolicyID)
	outcome.SkillID = strings.TrimSpace(outcome.SkillID)
	outcome.File = strings.TrimSpace(outcome.File)
	outcome.Path = strings.TrimSpace(outcome.Path)
	outcome.Provider = strings.TrimSpace(outcome.Provider)
	outcome.Tool = strings.TrimSpace(outcome.Tool)
	outcome.Outcome = strings.TrimSpace(outcome.Outcome)
	outcome.RecordedAtUTC = strings.TrimSpace(outcome.RecordedAtUTC)
	outcome.SearchText = firstNonEmpty(outcome.SearchText, remediationOutcomeSearchText(outcome))
	outcome.ID = firstNonEmpty(outcome.ID, stableID(
		"remediation-outcome",
		outcome.RemediationID,
		outcome.FindingID,
		outcome.SourceTraceID,
		outcome.FollowupTraceID,
		outcome.Outcome,
		fmt.Sprintf("%d", outcome.AttemptOrdinal),
	))

	return outcome
}

func normalizeEmbeddingRecord(record EmbeddingRecord) EmbeddingRecord {
	record.Backend = strings.TrimSpace(record.Backend)
	record.Collection = strings.TrimSpace(record.Collection)
	record.ModelID = strings.TrimSpace(record.ModelID)
	record.InputKind = strings.TrimSpace(record.InputKind)
	record.RecordKind = strings.TrimSpace(record.RecordKind)
	record.RecordID = strings.TrimSpace(record.RecordID)
	record.TraceID = strings.TrimSpace(record.TraceID)
	record.PolicyID = strings.TrimSpace(record.PolicyID)
	record.SkillID = strings.TrimSpace(record.SkillID)
	record.Path = strings.TrimSpace(record.Path)
	record.ContentHash = strings.TrimSpace(record.ContentHash)
	record.Provider = strings.TrimSpace(record.Provider)
	record.BackendRowID = firstNonEmpty(record.BackendRowID, record.ID)
	record.CreatedAtUTC = strings.TrimSpace(record.CreatedAtUTC)
	record.ID = firstNonEmpty(record.ID, stableID(
		"embedding",
		record.Backend,
		record.Collection,
		record.ModelID,
		record.RecordKind,
		record.RecordID,
		record.ContentHash,
	))

	return record
}

func remediationOutcomeSearchText(outcome RemediationOutcome) string {
	return strings.Join(compactStrings([]string{
		outcome.RemediationID,
		outcome.FindingID,
		outcome.PolicyID,
		outcome.SkillID,
		outcome.File,
		outcome.Path,
		outcome.Provider,
		outcome.Tool,
		outcome.Outcome,
	}), "\n")
}

func embeddingSearchText(record EmbeddingRecord) string {
	return strings.Join(compactStrings([]string{
		record.Backend,
		record.Collection,
		record.ModelID,
		record.InputKind,
		record.RecordKind,
		record.RecordID,
		record.PolicyID,
		record.SkillID,
		record.Path,
		record.ContentHash,
		record.Provider,
	}), "\n")
}
