// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	hookTraceEnv        = "CODE_ETHOS_HOOK_RUN_DIR"
	hookTraceFile       = "event.json"
	hookTraceFileMode   = 0o600
	commandPreviewLimit = 300
)

type HookTrace struct {
	RecordedAtUTC string               `json:"recorded_at_utc"`
	Provider      string               `json:"provider,omitempty"`
	Event         string               `json:"event"`
	Tool          string               `json:"tool,omitempty"`
	Cwd           string               `json:"cwd,omitempty"`
	Command       *HookTraceCommand    `json:"command,omitempty"`
	Files         []string             `json:"files,omitempty"`
	Status        string               `json:"status"`
	Decisions     []HookTraceDecision  `json:"decisions,omitempty"`
	OutputShape   HookTraceOutputShape `json:"output_shape"`
}

type HookTraceCommand struct {
	SHA256  string `json:"sha256"`
	Preview string `json:"preview"`
}

type HookTraceDecision struct {
	PolicyID        string   `json:"policy_id,omitempty"`
	Decision        string   `json:"decision,omitempty"`
	Severity        string   `json:"severity,omitempty"`
	SkillID         string   `json:"skill_id,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	Implementation  string   `json:"implementation,omitempty"`
	Message         string   `json:"message,omitempty"`
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

func WriteAgentHookTrace(runDir string, event Event, result Result) error {
	trace := HookTrace{
		RecordedAtUTC: time.Now().UTC().Format(time.RFC3339),
		Provider:      event.Provider(),
		Event:         event.HookEventName,
		Tool:          event.ToolName,
		Cwd:           event.Cwd,
		Files:         event.Files(),
		Status:        result.Status,
		Decisions:     traceDecisions(result.Decisions),
		OutputShape:   traceOutputShape(result),
	}

	if command := event.Command(); command != "" {
		trace.Command = &HookTraceCommand{
			SHA256:  sha256Hex(command),
			Preview: truncateForTrace(command, commandPreviewLimit),
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
	defer file.Close()

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
			EvidenceKeys:    sortedEvidenceKeys(decision.Evidence),
			PrincipleIDs:    append([]string(nil), decision.PrincipleIDs...),
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

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))

	return hex.EncodeToString(sum[:])
}

func truncateForTrace(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	return value[:limit] + "..."
}
