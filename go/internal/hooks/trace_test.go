// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestWriteAgentHookTraceRecordsSanitizedEventAndDecisions(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	result := Result{
		Event:      "PreToolUse",
		Provider:   "codex",
		RuntimeMS:  17,
		Status:     "blocked",
		Tool:       "Bash",
		TrackingID: "deny-test-1",
		Decisions: []policy.Decision{{
			PolicyID:     "custom.no_subprocess_git",
			Decision:     "block",
			Severity:     "block",
			Message:      "Use the managed wrapper.",
			Suggestion:   "Use the protected Git wrapper.",
			PrincipleIDs: []string{"one-path-for-critical-operations"},
			Evidence: map[string]any{
				"implementation": "cel",
				"skill_id":       "safe-git-workflow",
				"when":           "command.contains('git')",
			},
		}},
	}

	err := WriteAgentHookTrace(runDir, Event{
		HookEventName: "PreToolUse",
		SessionID:     "session-1",
		Source:        "codex",
		ToolName:      "Bash",
		Cwd:           "/repo",
		ToolInput: map[string]any{
			"command": "git status --short",
			"path":    "pkg/example.py",
		},
	}, result)
	if err != nil {
		t.Fatalf("write hook trace: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(runDir, "event.json"))
	if err != nil {
		t.Fatalf("read hook trace: %v", err)
	}

	var trace map[string]any
	if err := json.Unmarshal(payload, &trace); err != nil {
		t.Fatalf("parse hook trace: %v\n%s", err, payload)
	}

	if trace["provider"] != "codex" || trace["event"] != "PreToolUse" ||
		trace["status"] != "blocked" {
		t.Fatalf("unexpected trace identity: %#v", trace)
	}
	if trace["schema_version"] != float64(1) {
		t.Fatalf("missing schema version: %#v", trace)
	}
	if trace["tracking_id"] != "deny-test-1" ||
		trace["session_id"] != "session-1" ||
		trace["operation_kind"] != "git_status" ||
		trace["target_kind"] != "repo_state" ||
		trace["risk_category"] != "bypass" ||
		trace["runtime_ms"] != float64(17) {
		t.Fatalf("missing hook analytics fields: %#v", trace)
	}
	if hash, ok := trace["target_set_sha256"].(string); !ok || len(hash) != 64 {
		t.Fatalf("missing target set hash: %#v", trace)
	}
	if traceID, ok := trace["trace_id"].(string); !ok || !strings.HasPrefix(traceID, "hook-") {
		t.Fatalf("missing trace id: %#v", trace)
	}

	command, ok := trace["command"].(map[string]any)
	if !ok {
		t.Fatalf("missing command trace: %#v", trace)
	}
	if command["preview"] != "git status --short" {
		t.Fatalf("unexpected command preview: %#v", command)
	}
	if sha, ok := command["sha256"].(string); !ok || len(sha) != 64 {
		t.Fatalf("missing command hash: %#v", command)
	}
	if sha, ok := command["shape_sha256"].(string); !ok || len(sha) != 64 {
		t.Fatalf("missing command shape hash: %#v", command)
	}

	decisions, ok := trace["decisions"].([]any)
	if !ok || len(decisions) != 1 {
		t.Fatalf("missing decision trace: %#v", trace)
	}
	decision, ok := decisions[0].(map[string]any)
	if !ok || decision["policy_id"] != "custom.no_subprocess_git" {
		t.Fatalf("unexpected decision trace: %#v", decisions)
	}
	if decision["implementation"] != "cel" ||
		decision["skill_id"] != "safe-git-workflow" ||
		decision["suggestion"] != "Use the protected Git wrapper." {
		t.Fatalf("missing decision parity metadata: %#v", decision)
	}
	if hash, ok := decision["message_hash"].(string); !ok || len(hash) != 64 {
		t.Fatalf("missing message variant hash: %#v", decision)
	}
	if hash, ok := decision["suggestion_hash"].(string); !ok || len(hash) != 64 {
		t.Fatalf("missing suggestion variant hash: %#v", decision)
	}
	if _, ok := decision["principle_ids"].([]any); !ok {
		t.Fatalf("missing principle ids: %#v", decision)
	}
	if keys, ok := decision["evidence_keys"].([]any); !ok || len(keys) == 0 {
		t.Fatalf("missing evidence key summary: %#v", decision)
	}
	remediation, ok := trace["agent_remediation"].([]any)
	if !ok || len(remediation) != 1 {
		t.Fatalf("missing agent remediation trace: %#v", trace)
	}
	item, ok := remediation[0].(map[string]any)
	if !ok || item["policy_id"] != "custom.no_subprocess_git" {
		t.Fatalf("unexpected remediation item: %#v", remediation)
	}
	if item["failed_action"] != "Bash" {
		t.Fatalf("missing failed action: %#v", item)
	}
	findings, ok := trace["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("missing normalized findings: %#v", trace)
	}
	finding, ok := findings[0].(map[string]any)
	if !ok || finding["policy_id"] != "custom.no_subprocess_git" || finding["id"] == "" {
		t.Fatalf("unexpected normalized finding: %#v", findings)
	}
	events, ok := trace["remediation_events"].([]any)
	if !ok || len(events) != 1 {
		t.Fatalf("missing remediation events: %#v", trace)
	}
	event, ok := events[0].(map[string]any)
	if !ok || event["finding_id"] != finding["id"] || event["event"] != "suggested" {
		t.Fatalf("unexpected remediation event: %#v", events)
	}

	raw := string(payload)
	if strings.Contains(raw, "tool_input") || strings.Contains(raw, "ToolInput") {
		t.Fatalf("trace must not dump raw provider input:\n%s", raw)
	}
}

func TestWriteAgentHookTraceUsesUniqueTraceIDForRepeatedViolations(t *testing.T) {
	t.Parallel()

	event := Event{
		HookEventName: "PreToolUse",
		Source:        "codex",
		ToolName:      "Bash",
		Cwd:           "/repo",
		ToolInput: map[string]any{
			"command": "git commit --no-verify -m test",
		},
	}
	result := Result{
		Event:    "PreToolUse",
		Provider: "codex",
		Status:   "blocked",
		Tool:     "Bash",
		Decisions: []policy.Decision{{
			PolicyID:   "git.hook_bypass",
			Decision:   "block",
			Severity:   "block",
			Message:    "Hook bypass is forbidden.",
			Suggestion: "Run the configured gate.",
		}},
	}

	first := writeTraceAndReadMap(t, event, result)
	second := writeTraceAndReadMap(t, event, result)
	if first["trace_id"] == second["trace_id"] {
		t.Fatalf("repeated hook traces reused trace_id: first=%#v second=%#v", first, second)
	}

	firstEvent := first["remediation_events"].([]any)[0].(map[string]any)
	secondEvent := second["remediation_events"].([]any)[0].(map[string]any)
	if firstEvent["id"] == secondEvent["id"] {
		t.Fatalf("repeated hook traces reused remediation event id: first=%#v second=%#v", firstEvent, secondEvent)
	}

	firstFinding := first["findings"].([]any)[0].(map[string]any)
	secondFinding := second["findings"].([]any)[0].(map[string]any)
	if firstFinding["id"] != secondFinding["id"] {
		t.Fatalf("finding identity should remain stable: first=%#v second=%#v", firstFinding, secondFinding)
	}
}

func writeTraceAndReadMap(t *testing.T, event Event, result Result) map[string]any {
	t.Helper()

	runDir := t.TempDir()
	if err := WriteAgentHookTrace(runDir, event, result); err != nil {
		t.Fatalf("write hook trace: %v", err)
	}
	payload, err := os.ReadFile(filepath.Join(runDir, "event.json"))
	if err != nil {
		t.Fatalf("read hook trace: %v", err)
	}
	var trace map[string]any
	if err := json.Unmarshal(payload, &trace); err != nil {
		t.Fatalf("parse hook trace: %v\n%s", err, payload)
	}

	return trace
}
