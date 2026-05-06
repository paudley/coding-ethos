// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

type SARIFRun struct {
	ID            string                 `json:"id"`
	TraceID       string                 `json:"trace_id,omitempty"`
	SourcePath    string                 `json:"source_path,omitempty"`
	Category      string                 `json:"category,omitempty"`
	ToolName      string                 `json:"tool_name,omitempty"`
	AutomationID  string                 `json:"automation_id,omitempty"`
	RunGUID       string                 `json:"run_guid,omitempty"`
	BaselineGUID  string                 `json:"baseline_guid,omitempty"`
	ProducedAtUTC string                 `json:"produced_at_utc,omitempty"`
	Raw           json.RawMessage        `json:"raw,omitempty"`
	Results       []SARIFResultReference `json:"results,omitempty"`
}

type SARIFResultReference struct {
	ASTLanguage   string          `json:"ast_language,omitempty"`
	PolicySource  string          `json:"policy_source,omitempty"`
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
	SearchText    string          `json:"search_text,omitempty"`
	ASTSymbolKind string          `json:"ast_symbol_kind,omitempty"`
	ASTNodeKind   string          `json:"ast_node_kind,omitempty"`
	ASTSymbolName string          `json:"ast_symbol_name,omitempty"`
	ASTSymbolPath string          `json:"ast_symbol_path,omitempty"`
	LinkedChunkID string          `json:"linked_chunk_id,omitempty"`
	EvaluatorKind string          `json:"evaluator_kind,omitempty"`
	CELPolicyID   string          `json:"cel_policy_id,omitempty"`
	CELExpression string          `json:"cel_expression,omitempty"`
	Raw           json.RawMessage `json:"raw,omitempty"`
	PrincipleIDs  []string        `json:"principle_ids,omitempty"`
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

type HookEventAnalytics struct {
	TraceID            string `json:"trace_id"`
	TrackingID         string `json:"tracking_id,omitempty"`
	SessionID          string `json:"session_id,omitempty"`
	Provider           string `json:"provider,omitempty"`
	Event              string `json:"event,omitempty"`
	Tool               string `json:"tool,omitempty"`
	Status             string `json:"status,omitempty"`
	OperationKind      string `json:"operation_kind,omitempty"`
	TargetKind         string `json:"target_kind,omitempty"`
	RiskCategory       string `json:"risk_category,omitempty"`
	CommandSHA256      string `json:"command_sha256,omitempty"`
	CommandShapeSHA256 string `json:"command_shape_sha256,omitempty"`
	TargetSetSHA256    string `json:"target_set_sha256,omitempty"`
	Cwd                string `json:"cwd,omitempty"`
	Source             string `json:"source,omitempty"`
	Matcher            string `json:"matcher,omitempty"`
	TranscriptPath     string `json:"transcript_path,omitempty"`
	RuntimeMS          int64  `json:"runtime_ms,omitempty"`
	DecisionCount      int    `json:"decision_count,omitempty"`
	Blocked            bool   `json:"blocked"`
	Rewritten          bool   `json:"rewritten"`
	AdditionalContext  bool   `json:"additional_context"`
}

type HookDecisionAnalytics struct {
	TraceID         string   `json:"trace_id"`
	TrackingID      string   `json:"tracking_id,omitempty"`
	PolicyID        string   `json:"policy_id,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	Severity        string   `json:"severity,omitempty"`
	SkillID         string   `json:"skill_id,omitempty"`
	Implementation  string   `json:"implementation,omitempty"`
	Message         string   `json:"message,omitempty"`
	MessageHash     string   `json:"message_hash,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	SuggestionHash  string   `json:"suggestion_hash,omitempty"`
	PrincipleIDs    []string `json:"principle_ids,omitempty"`
	DiagnosticCount int      `json:"diagnostic_count,omitempty"`
	DecisionOrdinal int      `json:"decision_ordinal"`
}

type HookTargetAnalytics struct {
	TraceID     string `json:"trace_id"`
	TargetPath  string `json:"target_path"`
	TargetKind  string `json:"target_kind,omitempty"`
	TargetIndex int    `json:"target_index"`
}

type HookUsageQuery struct {
	Provider      string
	Status        string
	PolicyID      string
	SkillID       string
	OperationKind string
	TargetKind    string
	RiskCategory  string
	Limit         int
}

type HookUsageSummary struct {
	LastSeenUTC    string  `json:"last_seen_utc,omitempty"`
	Tool           string  `json:"tool,omitempty"`
	OperationKind  string  `json:"operation_kind,omitempty"`
	TargetKind     string  `json:"target_kind,omitempty"`
	RiskCategory   string  `json:"risk_category,omitempty"`
	Status         string  `json:"status,omitempty"`
	PolicyID       string  `json:"policy_id,omitempty"`
	SkillID        string  `json:"skill_id,omitempty"`
	Provider       string  `json:"provider,omitempty"`
	LastTrackingID string  `json:"last_tracking_id,omitempty"`
	LastTraceID    string  `json:"last_trace_id,omitempty"`
	EventCount     int     `json:"event_count"`
	AvgRuntimeMS   float64 `json:"avg_runtime_ms,omitempty"`
	RewriteCount   int     `json:"rewrite_count"`
	BlockedCount   int     `json:"blocked_count"`
	DecisionCount  int     `json:"decision_count"`
}

type HookReview struct {
	ID            string `json:"id"`
	TraceID       string `json:"trace_id"`
	TrackingID    string `json:"tracking_id,omitempty"`
	Disposition   string `json:"disposition"`
	Reviewer      string `json:"reviewer,omitempty"`
	Notes         string `json:"notes,omitempty"`
	RecordedAtUTC string `json:"recorded_at_utc,omitempty"`
}

type HookReviewQuery struct {
	TraceID     string
	TrackingID  string
	Disposition string
	Limit       int
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
	Path          string `json:"path"`
	Language      string `json:"language"`
	ContentHash   string `json:"content_hash"`
	ParserName    string `json:"parser_name,omitempty"`
	ParserVersion string `json:"parser_version,omitempty"`
	IndexedAtUTC  string `json:"indexed_at_utc"`
	StaleReason   string `json:"stale_reason,omitempty"`
	SizeBytes     int    `json:"size_bytes"`
	LineCount     int    `json:"line_count"`
}

type CodeChunk struct {
	ID               string `json:"id"`
	Path             string `json:"path"`
	Language         string `json:"language"`
	NodeKind         string `json:"node_kind"`
	SymbolKind       string `json:"symbol_kind,omitempty"`
	SymbolName       string `json:"symbol_name,omitempty"`
	SymbolPath       string `json:"symbol_path,omitempty"`
	ParentSymbolPath string `json:"parent_symbol_path,omitempty"`
	ParentChunkID    string `json:"parent_chunk_id,omitempty"`
	ContentHash      string `json:"content_hash"`
	SearchText       string `json:"search_text"`
	RawText          string `json:"raw_text,omitempty"`
	StartByte        int    `json:"start_byte"`
	EndByte          int    `json:"end_byte"`
	StartLine        int    `json:"start_line"`
	EndLine          int    `json:"end_line"`
}

type CodeEdge struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Path             string `json:"path"`
	SourceChunkID    string `json:"source_chunk_id,omitempty"`
	TargetPath       string `json:"target_path,omitempty"`
	TargetChunkID    string `json:"target_chunk_id,omitempty"`
	TargetSymbolPath string `json:"target_symbol_path,omitempty"`
	TargetName       string `json:"target_name,omitempty"`
	RawText          string `json:"raw_text,omitempty"`
}

type ASTFindingLink struct {
	ID          string `json:"id"`
	FindingKind string `json:"finding_kind"`
	FindingID   string `json:"finding_id"`
	ChunkID     string `json:"chunk_id"`
	Path        string `json:"path"`
	PolicyID    string `json:"policy_id,omitempty"`
	SkillID     string `json:"skill_id,omitempty"`
	SymbolPath  string `json:"symbol_path,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
	Stale       bool   `json:"stale"`
}

type CodeContextQuery struct {
	ChunkID    string
	Path       string
	SymbolPath string
	Line       int
	Limit      int
}

type CodeContext struct {
	Parent        *CodeChunk       `json:"parent,omitempty"`
	Children      []CodeChunk      `json:"children,omitempty"`
	OutgoingEdges []CodeEdge       `json:"outgoing_edges,omitempty"`
	IncomingEdges []CodeEdge       `json:"incoming_edges,omitempty"`
	FindingLinks  []ASTFindingLink `json:"finding_links,omitempty"`
	Chunk         CodeChunk        `json:"chunk"`
}

type CodeIndexSummary struct {
	Skipped       []string `json:"skipped,omitempty"`
	FilesIndexed  int      `json:"files_indexed"`
	ChunksIndexed int      `json:"chunks_indexed"`
}

func (store *Store) IngestSARIFRun(ctx context.Context, run SARIFRun) error {
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		return errors.New("SARIF run id is required")
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
		return errors.New("remediation outcome id is required")
	}

	if outcome.Outcome == "" {
		return errors.New("remediation outcome is required")
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

func (store *Store) RecordHookReview(ctx context.Context, review HookReview) error {
	review = normalizeHookReview(review)
	if review.TraceID == "" {
		return errors.New("hook review trace id is required")
	}

	if review.Disposition == "" {
		return errors.New("hook review disposition is required")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin hook review write: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := insertHookReview(ctx, tx, review); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit hook review write: %w", err)
	}

	return nil
}

func (store *Store) UpsertEmbeddingRecord(
	ctx context.Context,
	record EmbeddingRecord,
) error {
	record = normalizeEmbeddingRecord(record)
	if record.ID == "" {
		return errors.New("embedding id is required")
	}

	if record.Backend == "" || record.Collection == "" || record.ModelID == "" {
		return errors.New("embedding backend, collection, and model id are required")
	}

	if record.RecordKind == "" || record.RecordID == "" {
		return errors.New("embedding record kind and record id are required")
	}

	if record.Dimension <= 0 {
		return errors.New("embedding dimension must be positive")
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
	return store.ReplaceCodeFileIndex(ctx, file, chunks, nil)
}

func (store *Store) ReplaceCodeFileIndex(
	ctx context.Context,
	file CodeFile,
	chunks []CodeChunk,
	edges []CodeEdge,
) error {
	file.Path = strings.TrimSpace(file.Path)
	file.Language = strings.TrimSpace(file.Language)

	file.ContentHash = strings.TrimSpace(file.ContentHash)
	if file.Path == "" || file.Language == "" || file.ContentHash == "" {
		return errors.New("code file path, language, and content hash are required")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin code chunk write: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := replaceCodeFileChunks(ctx, tx, file, chunks, edges); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit code chunk write: %w", err)
	}

	return nil
}

func normalizeHookReview(review HookReview) HookReview {
	review.TraceID = strings.TrimSpace(review.TraceID)
	review.TrackingID = strings.TrimSpace(review.TrackingID)
	review.Disposition = strings.TrimSpace(review.Disposition)
	review.Reviewer = strings.TrimSpace(review.Reviewer)
	review.Notes = strings.TrimSpace(review.Notes)
	review.RecordedAtUTC = strings.TrimSpace(review.RecordedAtUTC)
	review.ID = firstNonEmpty(review.ID, stableID(
		"hook-review",
		review.TraceID,
		review.TrackingID,
		review.Disposition,
		review.Reviewer,
		review.RecordedAtUTC,
	))

	return review
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
	outcome.SearchText = firstNonEmpty(
		outcome.SearchText,
		remediationOutcomeSearchText(outcome),
	)
	outcome.ID = firstNonEmpty(outcome.ID, stableID(
		"remediation-outcome",
		outcome.RemediationID,
		outcome.FindingID,
		outcome.SourceTraceID,
		outcome.FollowupTraceID,
		outcome.Outcome,
		strconv.Itoa(outcome.AttemptOrdinal),
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
