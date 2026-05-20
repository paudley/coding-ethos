// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package agentproxy defines provider-neutral contracts for future transparent
// agent proxying. The package intentionally contains contracts and pure
// transforms only; policy decisions and persistence live in CEL and code-intel.
package agentproxy

import "time"

type EventKind string

const (
	EventSessionStarted    EventKind = "session_started"
	EventProviderCall      EventKind = "provider_call"
	EventProviderResponse  EventKind = "provider_response"
	EventToolCall          EventKind = "tool_call"
	EventToolOutput        EventKind = "tool_output"
	EventFileRead          EventKind = "file_read"
	EventFileListing       EventKind = "file_listing"
	EventPayloadInject     EventKind = "payload_injection"
	EventPayloadTrim       EventKind = "payload_truncation"
	EventEditProposal      EventKind = "edit_proposal"
	EventSearchRequest     EventKind = "search_request"
	EventPatchOutcome      EventKind = "patch_outcome"
	EventRemediationAction EventKind = "remediation_action"
	EventCacheHit          EventKind = "cache_hit"
	EventCacheMiss         EventKind = "cache_miss"
)

type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type EventDirection string

const (
	DirectionOutbound EventDirection = "outbound"
	DirectionInbound  EventDirection = "inbound"
	DirectionLocal    EventDirection = "local"
)

type PayloadKind string

const (
	PayloadPrompt       PayloadKind = "prompt"
	PayloadResponse     PayloadKind = "response"
	PayloadToolCall     PayloadKind = "tool_call"
	PayloadToolOutput   PayloadKind = "tool_output"
	PayloadFileContent  PayloadKind = "file_content"
	PayloadListing      PayloadKind = "directory_listing"
	PayloadEdit         PayloadKind = "edit"
	PayloadSearch       PayloadKind = "search"
	PayloadRemediation  PayloadKind = "remediation"
	PayloadPolicyAdvice PayloadKind = "policy_advice"
)

type ProviderEvent struct {
	RecordedAtUTC time.Time          `json:"recorded_at_utc"`
	Metadata      map[string]string  `json:"metadata,omitempty"`
	Cwd           string             `json:"cwd,omitempty"`
	InputHash     string             `json:"input_hash,omitempty"`
	Tool          string             `json:"tool,omitempty"`
	Model         string             `json:"model,omitempty"`
	Kind          EventKind          `json:"kind"`
	RepoRoot      string             `json:"repo_root,omitempty"`
	ID            string             `json:"id"`
	Provider      string             `json:"provider,omitempty"`
	OutputHash    string             `json:"output_hash,omitempty"`
	TargetPath    string             `json:"target_path,omitempty"`
	PolicyID      string             `json:"policy_id,omitempty"`
	Decision      string             `json:"decision,omitempty"`
	SessionID     string             `json:"session_id"`
	TraceID       string             `json:"trace_id,omitempty"`
	TrackingID    string             `json:"tracking_id,omitempty"`
	Direction     EventDirection     `json:"direction,omitempty"`
	PayloadKind   PayloadKind        `json:"payload_kind,omitempty"`
	CacheKey      string             `json:"cache_key,omitempty"`
	Policy        PolicyEvidence     `json:"policy,omitzero"`
	DLPFacts      []DLPFact          `json:"dlp_facts,omitempty"`
	Transforms    []TransformRecord  `json:"transforms,omitempty"`
	TokenUsage    TokenUsage         `json:"token_usage,omitzero"`
	Payload       PayloadMeasurement `json:"payload,omitzero"`
}

type TokenUsage struct {
	InputTokens  int `json:"input_tokens,omitempty"`
	OutputTokens int `json:"output_tokens,omitempty"`
	TotalTokens  int `json:"total_tokens,omitempty"`
}

type PayloadMeasurement struct {
	Bytes int `json:"bytes,omitempty"`
	Lines int `json:"lines,omitempty"`
}

type PolicyEvidence struct {
	PolicyID     string   `json:"policy_id,omitempty"`
	SkillID      string   `json:"skill_id,omitempty"`
	Decision     string   `json:"decision,omitempty"`
	Reason       string   `json:"reason,omitempty"`
	EvidenceID   string   `json:"evidence_id,omitempty"`
	MCPTool      string   `json:"mcp_tool,omitempty"`
	PrincipleIDs []string `json:"principle_ids,omitempty"`
}

type DLPFact struct {
	Type       string `json:"type"`
	Path       string `json:"path,omitempty"`
	Reason     string `json:"reason,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Line       int    `json:"line,omitempty"`
	Column     int    `json:"column,omitempty"`
}

type TransformRecord struct {
	Name          string            `json:"name"`
	InputHash     string            `json:"input_hash,omitempty"`
	OutputHash    string            `json:"output_hash,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	PolicyID      string            `json:"policy_id,omitempty"`
	Decision      string            `json:"decision,omitempty"`
	EvidencePath  string            `json:"evidence_path,omitempty"`
	Metadata      map[string]string `json:"metadata,omitempty"`
	InputTokens   int               `json:"input_tokens,omitempty"`
	OutputTokens  int               `json:"output_tokens,omitempty"`
	BytesRemoved  int               `json:"bytes_removed,omitempty"`
	FindingsCount int               `json:"findings_count,omitempty"`
}

type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	Name    string `json:"name,omitempty"`
}

type ProviderRequest struct {
	Metadata  map[string]string `json:"metadata,omitempty"`
	SessionID string            `json:"session_id"`
	Provider  string            `json:"provider"`
	Model     string            `json:"model,omitempty"`
	Messages  []Message         `json:"messages"`
}

type ProviderResponse struct {
	SessionID string     `json:"session_id"`
	Provider  string     `json:"provider"`
	Model     string     `json:"model,omitempty"`
	Messages  []Message  `json:"messages"`
	Usage     TokenUsage `json:"usage,omitzero"`
}

type Provider interface {
	Send(request ProviderRequest) (ProviderResponse, error)
}
