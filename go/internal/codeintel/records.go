// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
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
	ASTLanguage      string          `json:"ast_language,omitempty"`
	PolicySource     string          `json:"policy_source,omitempty"`
	ID               string          `json:"id"`
	RuleID           string          `json:"rule_id,omitempty"`
	Level            string          `json:"level,omitempty"`
	Message          string          `json:"message,omitempty"`
	Fingerprint      string          `json:"fingerprint,omitempty"`
	ProxyEventID     string          `json:"proxy_event_id,omitempty"`
	ProxySessionID   string          `json:"proxy_session_id,omitempty"`
	ProxyEventKind   string          `json:"proxy_event_kind,omitempty"`
	ProxyDirection   string          `json:"proxy_direction,omitempty"`
	ProxyPayloadKind string          `json:"proxy_payload_kind,omitempty"`
	ProxyTraceID     string          `json:"proxy_trace_id,omitempty"`
	ProxyTrackingID  string          `json:"proxy_tracking_id,omitempty"`
	ProxyTransform   string          `json:"proxy_transform,omitempty"`
	FindingID        string          `json:"finding_id,omitempty"`
	RemediationID    string          `json:"remediation_id,omitempty"`
	PolicyID         string          `json:"policy_id,omitempty"`
	SkillID          string          `json:"skill_id,omitempty"`
	Path             string          `json:"path,omitempty"`
	SearchText       string          `json:"search_text,omitempty"`
	ASTSymbolKind    string          `json:"ast_symbol_kind,omitempty"`
	ASTNodeKind      string          `json:"ast_node_kind,omitempty"`
	ASTSymbolName    string          `json:"ast_symbol_name,omitempty"`
	ASTSymbolPath    string          `json:"ast_symbol_path,omitempty"`
	LinkedChunkID    string          `json:"linked_chunk_id,omitempty"`
	EvaluatorKind    string          `json:"evaluator_kind,omitempty"`
	CELPolicyID      string          `json:"cel_policy_id,omitempty"`
	CELExpression    string          `json:"cel_expression,omitempty"`
	Raw              json.RawMessage `json:"raw,omitempty"`
	PrincipleIDs     []string        `json:"principle_ids,omitempty"`
	StartLine        int             `json:"start_line,omitempty"`
	StartColumn      int             `json:"start_column,omitempty"`
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

type ProxySession struct {
	ID               string `json:"id"`
	Provider         string `json:"provider,omitempty"`
	Model            string `json:"model,omitempty"`
	RepoRoot         string `json:"repo_root,omitempty"`
	StartedAtUTC     string `json:"started_at_utc,omitempty"`
	LastSeenUTC      string `json:"last_seen_utc,omitempty"`
	RequestCount     int    `json:"request_count"`
	ToolCallCount    int    `json:"tool_call_count"`
	FileReadCount    int    `json:"file_read_count"`
	FileListingCount int    `json:"file_listing_count"`
	EditCount        int    `json:"edit_count"`
	CacheHitCount    int    `json:"cache_hit_count"`
	InjectionCount   int    `json:"injection_count"`
	TruncationCount  int    `json:"truncation_count"`
	DenialCount      int    `json:"denial_count"`
	TransformCount   int    `json:"transform_count"`
	InputTokens      int    `json:"input_tokens"`
	OutputTokens     int    `json:"output_tokens"`
	TotalTokens      int    `json:"total_tokens"`
}

type ProxyEvent struct {
	ID            string              `json:"id"`
	SessionID     string              `json:"session_id"`
	Kind          string              `json:"kind"`
	Provider      string              `json:"provider,omitempty"`
	Tool          string              `json:"tool,omitempty"`
	Model         string              `json:"model,omitempty"`
	RecordedAtUTC string              `json:"recorded_at_utc,omitempty"`
	TraceID       string              `json:"trace_id,omitempty"`
	TrackingID    string              `json:"tracking_id,omitempty"`
	RepoRoot      string              `json:"repo_root,omitempty"`
	Cwd           string              `json:"cwd,omitempty"`
	TargetPath    string              `json:"target_path,omitempty"`
	Direction     string              `json:"direction,omitempty"`
	PayloadKind   string              `json:"payload_kind,omitempty"`
	CacheKey      string              `json:"cache_key,omitempty"`
	InputHash     string              `json:"input_hash,omitempty"`
	OutputHash    string              `json:"output_hash,omitempty"`
	PolicyID      string              `json:"policy_id,omitempty"`
	Decision      string              `json:"decision,omitempty"`
	Policy        ProxyPolicyEvidence `json:"policy,omitzero"`
	DLPFacts      []ProxyDLPFact      `json:"dlp_facts,omitempty"`
	Metadata      map[string]string   `json:"metadata,omitempty"`
	Transforms    []ProxyTransform    `json:"transforms,omitempty"`
	InputTokens   int                 `json:"input_tokens"`
	OutputTokens  int                 `json:"output_tokens"`
	TotalTokens   int                 `json:"total_tokens"`
	PayloadBytes  int                 `json:"payload_bytes"`
}

type ProxyPolicyEvidence struct {
	PolicyID     string   `json:"policy_id,omitempty"`
	SkillID      string   `json:"skill_id,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	EvidenceID   string   `json:"evidence_id,omitempty"`
	MCPTool      string   `json:"mcp_tool,omitempty"`
	PrincipleIDs []string `json:"principle_ids,omitempty"`
}

type ProxyDLPFact struct {
	Type       string `json:"type"`
	Path       string `json:"path,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
}

type ProxyTransform struct {
	Name          string `json:"name"`
	Reason        string `json:"reason,omitempty"`
	InputHash     string `json:"input_hash,omitempty"`
	OutputHash    string `json:"output_hash,omitempty"`
	PolicyID      string `json:"policy_id,omitempty"`
	Decision      string `json:"decision,omitempty"`
	EvidencePath  string `json:"evidence_path,omitempty"`
	InputTokens   int    `json:"input_tokens,omitempty"`
	OutputTokens  int    `json:"output_tokens,omitempty"`
	BytesRemoved  int    `json:"bytes_removed,omitempty"`
	FindingsCount int    `json:"findings_count,omitempty"`
}

type ProxySessionQuery struct {
	Provider string
	Limit    int
}

type ProxyEventQuery struct {
	SessionID  string
	Kind       string
	Provider   string
	PolicyID   string
	Decision   string
	TargetPath string
	Limit      int
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
	Path             string `json:"path"`
	Language         string `json:"language"`
	ContentHash      string `json:"content_hash"`
	ParserName       string `json:"parser_name,omitempty"`
	ParserVersion    string `json:"parser_version,omitempty"`
	SourceModTimeUTC string `json:"source_mtime_utc,omitempty"`
	IndexedAtUTC     string `json:"indexed_at_utc"`
	DeletedAtUTC     string `json:"deleted_at_utc,omitempty"`
	StaleReason      string `json:"stale_reason,omitempty"`
	SizeBytes        int    `json:"size_bytes"`
	LineCount        int    `json:"line_count"`
}

type CodeChunk struct {
	ID               string   `json:"id"`
	Path             string   `json:"path"`
	Language         string   `json:"language"`
	NodeKind         string   `json:"node_kind"`
	SymbolKind       string   `json:"symbol_kind,omitempty"`
	SymbolName       string   `json:"symbol_name,omitempty"`
	SymbolPath       string   `json:"symbol_path,omitempty"`
	ParentSymbolPath string   `json:"parent_symbol_path,omitempty"`
	ParentChunkID    string   `json:"parent_chunk_id,omitempty"`
	ContentHash      string   `json:"content_hash"`
	NormalizedHash   string   `json:"normalized_hash,omitempty"`
	SearchText       string   `json:"search_text"`
	RawText          string   `json:"raw_text,omitempty"`
	MinHashSig       []uint64 `json:"minhash_sig,omitempty"`
	StartByte        int      `json:"start_byte"`
	EndByte          int      `json:"end_byte"`
	StartLine        int      `json:"start_line"`
	EndLine          int      `json:"end_line"`
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

// CodeEdgeQuery filters indexed AST/code relationship edges.
type CodeEdgeQuery struct {
	Path       string
	Kind       string
	TargetName string
	Limit      int
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
	Root       string
	SymbolPath string
	Line       int
	Limit      int
}

type CodeContext struct {
	Parent        *CodeChunk       `json:"parent,omitempty"`
	Children      []CodeChunk      `json:"children,omitempty"`
	Siblings      []CodeChunk      `json:"siblings,omitempty"`
	OutgoingEdges []CodeEdge       `json:"outgoing_edges,omitempty"`
	IncomingEdges []CodeEdge       `json:"incoming_edges,omitempty"`
	FindingLinks  []ASTFindingLink `json:"finding_links,omitempty"`
	Chunk         CodeChunk        `json:"chunk"`
}

type RepoMapEntry struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	StaleReason string `json:"stale_reason,omitempty"`
	Symbols     int    `json:"symbols"`
	Chunks      int    `json:"chunks"`
	LineCount   int    `json:"line_count"`
}

type SymbolSummary struct {
	Path        string `json:"path"`
	Language    string `json:"language"`
	SymbolKind  string `json:"symbol_kind,omitempty"`
	SymbolName  string `json:"symbol_name,omitempty"`
	SymbolPath  string `json:"symbol_path,omitempty"`
	NodeKind    string `json:"node_kind"`
	ContentHash string `json:"content_hash"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	TokenSize   int    `json:"token_size"`
}

type CompactCodeContext struct {
	RepoMap    []RepoMapEntry  `json:"repo_map,omitempty"`
	Symbols    []SymbolSummary `json:"symbols,omitempty"`
	Chunks     []CodeChunk     `json:"chunks,omitempty"`
	IndexFresh bool            `json:"index_fresh"`
}

type CompactCodeContextQuery struct {
	Path     string
	Root     string
	Language string
	Limit    int
}

type DirectoryAnatomyQuery struct {
	Path           string
	Root           string
	Language       string
	Limit          int
	SymbolsPerFile int
}

type DirectoryAnatomy struct {
	Path  string                 `json:"path"`
	Files []DirectoryAnatomyFile `json:"files,omitempty"`
}

type DirectoryAnatomyFile struct {
	Path            string                   `json:"path"`
	Language        string                   `json:"language"`
	StaleReason     string                   `json:"stale_reason,omitempty"`
	Symbols         []DirectoryAnatomySymbol `json:"symbols,omitempty"`
	SizeBytes       int                      `json:"size_bytes"`
	EstimatedTokens int                      `json:"estimated_tokens"`
	LineCount       int                      `json:"line_count"`
	SymbolCount     int                      `json:"symbol_count"`
	ChunkCount      int                      `json:"chunk_count"`
}

type DirectoryAnatomySymbol struct {
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	SymbolPath string `json:"symbol_path,omitempty"`
	StartLine  int    `json:"start_line"`
}

type CodeIndexSummary struct {
	Skipped       []string `json:"skipped,omitempty"`
	Deleted       []string `json:"deleted,omitempty"`
	FilesIndexed  int      `json:"files_indexed"`
	ChunksIndexed int      `json:"chunks_indexed"`
}

type DiffEditPatternSummary struct {
	PatternsRecorded int `json:"patterns_recorded"`
}

type DiffEditPattern struct {
	DiffSource        string `json:"diff_source"`
	GitHead           string `json:"git_head,omitempty"`
	FirstGitHead      string `json:"first_git_head,omitempty"`
	LastGitHead       string `json:"last_git_head,omitempty"`
	TargetPath        string `json:"target_path"`
	PatternHash       string `json:"pattern_hash"`
	RemovedSHA256     string `json:"removed_sha256,omitempty"`
	AddedSHA256       string `json:"added_sha256,omitempty"`
	ASTChunkID        string `json:"ast_chunk_id,omitempty"`
	ASTLanguage       string `json:"ast_language,omitempty"`
	ASTNodeKind       string `json:"ast_node_kind,omitempty"`
	ASTSymbolKind     string `json:"ast_symbol_kind,omitempty"`
	ASTSymbolName     string `json:"ast_symbol_name,omitempty"`
	ASTSymbolPath     string `json:"ast_symbol_path,omitempty"`
	LastSeenUTC       string `json:"last_seen_utc,omitempty"`
	HunkHeader        string `json:"hunk_header,omitempty"`
	OldStart          int64  `json:"old_start"`
	OldLines          int64  `json:"old_lines"`
	NewStart          int64  `json:"new_start"`
	NewLines          int64  `json:"new_lines"`
	SeenCount         int    `json:"seen_count"`
	DistinctAddHashes int    `json:"distinct_add_hashes,omitempty"`
}

type DiffEditPatternQuery struct {
	DiffSource string
	Path       string
	Limit      int
}

func (store *Store) IngestSARIFRun(ctx context.Context, run SARIFRun) error {
	run.ID = strings.TrimSpace(run.ID)
	if run.ID == "" {
		return apperror.StaticError("SARIF run id is required")
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin SARIF ingest: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	inlineErr0 := insertSARIFRun(ctx, transaction, run)
	if inlineErr0 != nil {
		return inlineErr0
	}

	inlineErr1 := transaction.Commit()
	if inlineErr1 != nil {
		return fmt.Errorf("commit SARIF ingest: %w", inlineErr1)
	}

	return nil
}

func (store *Store) RecordRemediationOutcome(
	ctx context.Context,
	outcome RemediationOutcome,
) error {
	outcome = normalizeRemediationOutcome(outcome)
	if outcome.ID == "" {
		return apperror.StaticError("remediation outcome id is required")
	}

	if outcome.Outcome == "" {
		return apperror.StaticError("remediation outcome is required")
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin remediation outcome write: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	inlineErr2 := insertRemediationOutcome(ctx, transaction, outcome)
	if inlineErr2 != nil {
		return inlineErr2
	}

	inlineErr3 := transaction.Commit()
	if inlineErr3 != nil {
		return fmt.Errorf("commit remediation outcome write: %w", inlineErr3)
	}

	return nil
}

func (store *Store) RecordHookReview(ctx context.Context, review HookReview) error {
	review = normalizeHookReview(review)
	if review.TraceID == "" {
		return apperror.StaticError("hook review trace id is required")
	}

	if review.Disposition == "" {
		return apperror.StaticError("hook review disposition is required")
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin hook review write: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	inlineErr4 := insertHookReview(ctx, transaction, review)
	if inlineErr4 != nil {
		return inlineErr4
	}

	inlineErr5 := transaction.Commit()
	if inlineErr5 != nil {
		return fmt.Errorf("commit hook review write: %w", inlineErr5)
	}

	return nil
}

func (store *Store) UpsertEmbeddingRecord(
	ctx context.Context,
	record EmbeddingRecord,
) error {
	record = normalizeEmbeddingRecord(record)
	if record.ID == "" {
		return apperror.StaticError("embedding id is required")
	}

	if record.Backend == "" || record.Collection == "" || record.ModelID == "" {
		return apperror.StaticError(
			"embedding backend, collection, and model id are required",
		)
	}

	if record.RecordKind == "" || record.RecordID == "" {
		return apperror.StaticError("embedding record kind and record id are required")
	}

	if record.Dimension <= 0 {
		return apperror.StaticError("embedding dimension must be positive")
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin embedding metadata write: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	inlineErr6 := insertEmbeddingRecord(ctx, transaction, record)
	if inlineErr6 != nil {
		return inlineErr6
	}

	inlineErr7 := transaction.Commit()
	if inlineErr7 != nil {
		return fmt.Errorf("commit embedding metadata write: %w", inlineErr7)
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
		return apperror.StaticError(
			"code file path, language, and content hash are required",
		)
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin code chunk write: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	inlineErr8 := replaceCodeFileChunks(ctx, transaction, file, chunks, edges)
	if inlineErr8 != nil {
		return inlineErr8
	}

	inlineErr9 := transaction.Commit()
	if inlineErr9 != nil {
		return fmt.Errorf("commit code chunk write: %w", inlineErr9)
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
