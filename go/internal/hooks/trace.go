// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	hookTraceEnv        = "CODE_ETHOS_HOOK_RUN_DIR"
	hookTraceFile       = "event.json"
	hookTraceFileMode   = 0o600
	commandPreviewLimit = 300
)

var hookTraceFallbackCounter atomic.Uint64

type HookTrace struct {
	Command            *HookTraceCommand           `json:"command,omitempty"`
	Tool               string                      `json:"tool,omitempty"`
	Matcher            string                      `json:"matcher,omitempty"`
	RecordedAtUTC      string                      `json:"recorded_at_utc"`
	Provider           string                      `json:"provider,omitempty"`
	Event              string                      `json:"event"`
	OperationKind      string                      `json:"operation_kind,omitempty"`
	SessionID          string                      `json:"session_id,omitempty"`
	TargetSetSHA256    string                      `json:"target_set_sha256,omitempty"`
	Source             string                      `json:"source,omitempty"`
	TranscriptPath     string                      `json:"transcript_path,omitempty"`
	Cwd                string                      `json:"cwd,omitempty"`
	TraceID            string                      `json:"trace_id"`
	TrackingID         string                      `json:"tracking_id,omitempty"`
	TargetKind         string                      `json:"target_kind,omitempty"`
	Status             string                      `json:"status"`
	RiskCategory       string                      `json:"risk_category,omitempty"`
	AgentRemediation   []agentmsg.Remediation      `json:"agent_remediation,omitempty"`
	Files              []string                    `json:"files,omitempty"`
	Decisions          []HookTraceDecision         `json:"decisions,omitempty"`
	Findings           []evidence.Finding          `json:"findings,omitempty"`
	RemediationEvents  []evidence.RemediationEvent `json:"remediation_events,omitempty"`
	RemediationSummary agentmsg.Summary            `json:"remediation_summary,omitempty"`
	RuntimeMS          int64                       `json:"runtime_ms,omitempty"`
	SchemaVersion      int                         `json:"schema_version"`
	OutputShape        HookTraceOutputShape        `json:"output_shape"`
}

type HookTraceCommand struct {
	SHA256      string `json:"sha256"`
	ShapeSHA256 string `json:"shape_sha256,omitempty"`
	Preview     string `json:"preview"`
}

type HookTraceDecision struct {
	PolicyID        string   `json:"policy_id,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	Severity        string   `json:"severity,omitempty"`
	SkillID         string   `json:"skill_id,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	Implementation  string   `json:"implementation,omitempty"`
	Message         string   `json:"message,omitempty"`
	MessageHash     string   `json:"message_hash,omitempty"`
	SuggestionHash  string   `json:"suggestion_hash,omitempty"`
	EvidenceKeys    []string `json:"evidence_keys,omitempty"`
	PrincipleIDs    []string `json:"principle_ids,omitempty"`
	DiagnosticCount int      `json:"diagnostic_count,omitempty"`
}

type HookTraceOutputShape struct {
	HasHookSpecificOutput bool `json:"has_hook_specific_output"`
	HasUpdatedInput       bool `json:"has_updated_input"`
	HasAdditionalContext  bool `json:"has_additional_context"`
	Blocked               bool `json:"blocked"`
}

func WriteAgentHookTraceFromEnv(event Event, result Result) error {
	runDir := strings.TrimSpace(os.Getenv(hookTraceEnv))
	if runDir == "" {
		return nil
	}

	return WriteAgentHookTrace(runDir, event, result)
}

func WriteAgentHookTrace(runDir string, event Event, result Result) (err error) {
	remediation := agentmsg.FromDecisions(result.Decisions, event.ToolName)
	findings := evidence.FromDecisions(result.Decisions)
	traceID := hookTraceID(event, result)
	analytics := traceAnalytics(event, result)
	trace := HookTrace{
		SchemaVersion:      evidence.SchemaVersion,
		TraceID:            traceID,
		TrackingID:         result.TrackingID,
		RecordedAtUTC:      time.Now().UTC().Format(time.RFC3339),
		Provider:           event.Provider(),
		Event:              event.HookEventName,
		Tool:               event.ToolName,
		SessionID:          event.SessionID,
		Matcher:            event.Matcher,
		Source:             event.Source,
		TranscriptPath:     event.TranscriptPath,
		Cwd:                event.Cwd,
		Files:              analytics.NormalizedTargets,
		OperationKind:      analytics.OperationKind,
		TargetKind:         analytics.TargetKind,
		RiskCategory:       analytics.RiskCategory,
		TargetSetSHA256:    analytics.TargetSetHash,
		Status:             result.Status,
		RuntimeMS:          result.RuntimeMS,
		Decisions:          traceDecisions(result.Decisions),
		Findings:           findings,
		AgentRemediation:   remediation,
		RemediationSummary: agentmsg.Summarize(remediation),
		RemediationEvents: evidence.RemediationEvents(
			remediation,
			findings,
			traceID,
			"suggested",
		),
		OutputShape: traceOutputShape(result),
	}

	if command := event.Command(); command != "" {
		trace.Command = &HookTraceCommand{
			SHA256:      sha256Hex(command),
			ShapeSHA256: analytics.CommandShapeHash,
			Preview:     truncateForTrace(command, commandPreviewLimit),
		}
	}

	path := filepath.Join(runDir, hookTraceFile)

	file, err := os.OpenFile(
		filepath.Clean(path),
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		hookTraceFileMode,
	)
	if err != nil {
		return fmt.Errorf("open hook trace: %w", err)
	}

	defer func() {
		closeErr := file.Close()
		if err == nil && closeErr != nil {
			err = fmt.Errorf("close hook trace: %w", closeErr)
		}
	}()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(trace); err != nil {
		return fmt.Errorf("encode hook trace: %w", err)
	}

	return nil
}

func traceDecisions(decisions []policy.Decision) []HookTraceDecision {
	if len(decisions) == 0 {
		return nil
	}

	trace := make([]HookTraceDecision, 0, len(decisions))
	for _, decision := range decisions {
		trace = append(trace, HookTraceDecision{
			PolicyID:        decision.PolicyID,
			Decision:        decision.Decision,
			Severity:        decision.Severity,
			SkillID:         evidenceString(decision.Evidence, "skill_id"),
			Suggestion:      truncateForTrace(decision.Suggestion, commandPreviewLimit),
			Implementation:  evidenceString(decision.Evidence, "implementation"),
			Message:         truncateForTrace(decision.Message, commandPreviewLimit),
			MessageHash:     optionalSHA256(decision.Message),
			EvidenceKeys:    sortedEvidenceKeys(decision.Evidence),
			PrincipleIDs:    append([]string(nil), decision.PrincipleIDs...),
			SuggestionHash:  optionalSHA256(decision.Suggestion),
			DiagnosticCount: len(decision.Diagnostics),
		})
	}

	return trace
}

func evidenceString(evidence map[string]any, key string) string {
	value, ok := evidence[key]
	if !ok {
		return ""
	}

	text, ok := value.(string)
	if !ok {
		return ""
	}

	return text
}

func sortedEvidenceKeys(evidence map[string]any) []string {
	if len(evidence) == 0 {
		return nil
	}

	keys := make([]string, 0, len(evidence))
	for key := range evidence {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

func traceOutputShape(result Result) HookTraceOutputShape {
	shape := HookTraceOutputShape{
		Blocked: result.Blocked(),
	}
	if result.HookSpecificOutput == nil {
		return shape
	}

	shape.HasHookSpecificOutput = true
	shape.HasUpdatedInput = len(result.HookSpecificOutput.UpdatedInput) > 0
	shape.HasAdditionalContext = result.HookSpecificOutput.AdditionalContext != ""

	return shape
}

func hookTraceID(event Event, result Result) string {
	runID := randomTraceComponent()

	parts := []string{
		runID,
		event.Provider(),
		event.HookEventName,
		event.ToolName,
		event.Cwd,
		event.Command(),
		result.Status,
	}
	for _, decision := range result.Decisions {
		parts = append(parts, decision.PolicyID, decision.Decision, decision.Message)
	}

	return "hook-" + sha256Hex(strings.Join(parts, "\x00"))[:16]
}

func randomTraceComponent() string {
	var random [8]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}

	return fmt.Sprintf(
		"%d-%d-%d",
		time.Now().UTC().UnixNano(),
		os.Getpid(),
		hookTraceFallbackCounter.Add(1),
	)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func optionalSHA256(value string) string {
	if strings.TrimSpace(value) == "" {
		return ""
	}

	return sha256Hex(value)
}

func truncateForTrace(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit] + "..."
}
