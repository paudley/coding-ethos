// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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

const (
	customNoSubprocessGitPolicy = "custom.no_subprocess_git"
	gitStatusShortCommand       = "git status --short"
	providerCodex               = "codex"
)

func TestWriteAgentHookTraceRecordsSanitizedEventAndDecisions(t *testing.T) {
	t.Parallel()

	runDir := t.TempDir()
	result := Result{
		Event:      eventPreToolUse,
		Provider:   providerCodex,
		RuntimeMS:  17,
		Status:     "blocked",
		Tool:       toolBash,
		TrackingID: "deny-test-1",
		Decisions: []policy.Decision{{
			PolicyID:     customNoSubprocessGitPolicy,
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
		HookEventName: eventPreToolUse,
		SessionID:     "session-1",
		Source:        providerCodex,
		ToolName:      toolBash,
		Cwd:           "/repo",
		ToolInput: map[string]any{
			"command": gitStatusShortCommand,
			"path":    "pkg/example.py",
		},
	}, result)
	if err != nil {
		t.Fatalf("write hook trace: %v", err)
	}

	trace, payload := readTracePayload(t, runDir)
	assertTraceIdentity(t, trace)
	assertTraceCommand(t, trace)
	assertTraceDecision(t, trace)
	assertTraceRemediation(t, trace)
	assertTraceHidesRawProviderInput(t, payload)
}

func readTracePayload(t *testing.T, runDir string) (map[string]any, []byte) {
	t.Helper()

	payload, err := os.ReadFile(filepath.Join(runDir, "event.json"))
	if err != nil {
		t.Fatalf("read hook trace: %v", err)
	}

	var trace map[string]any

	err = json.Unmarshal(payload, &trace)
	if err != nil {
		t.Fatalf("parse hook trace: %v\n%s", err, payload)
	}

	return trace, payload
}

func assertTraceIdentity(t *testing.T, trace map[string]any) {
	t.Helper()

	for key, value := range expectedTraceIdentityFields() {
		if trace[key] != value {
			t.Fatalf("trace field %q mismatch: %#v", key, trace)
		}
	}

	if hash, found := trace["target_set_sha256"].(string); !found || len(hash) != 64 {
		t.Fatalf("missing target set hash: %#v", trace)
	}

	if traceID, found := trace["trace_id"].(string); !found ||
		!strings.HasPrefix(traceID, "hook-") {
		t.Fatalf("missing trace id: %#v", trace)
	}
}

func expectedTraceIdentityFields() map[string]any {
	return map[string]any{
		"provider":       providerCodex,
		"event":          eventPreToolUse,
		"status":         "blocked",
		"schema_version": float64(1),
		"tracking_id":    "deny-test-1",
		"session_id":     "session-1",
		"operation_kind": "git_status",
		"target_kind":    "repo_state",
		"risk_category":  "bypass",
		"runtime_ms":     float64(17),
	}
}

func assertTraceCommand(t *testing.T, trace map[string]any) {
	t.Helper()

	command, found := trace["command"].(map[string]any)
	if !found {
		t.Fatalf("missing command trace: %#v", trace)
	}

	if command["preview"] != gitStatusShortCommand {
		t.Fatalf("unexpected command preview: %#v", command)
	}

	if sha, found := command["sha256"].(string); !found || len(sha) != 64 {
		t.Fatalf("missing command hash: %#v", command)
	}

	if sha, found := command["shape_sha256"].(string); !found || len(sha) != 64 {
		t.Fatalf("missing command shape hash: %#v", command)
	}
}

func assertTraceDecision(t *testing.T, trace map[string]any) map[string]any {
	t.Helper()

	decision := traceMapAt(t, trace, "decisions")
	if decision["policy_id"] != customNoSubprocessGitPolicy {
		t.Fatalf("unexpected decision trace: %#v", decision)
	}

	if decision["implementation"] != "cel" ||
		decision["skill_id"] != "safe-git-workflow" ||
		decision["suggestion"] != "Use the protected Git wrapper." {
		t.Fatalf("missing decision parity metadata: %#v", decision)
	}

	if hash, found := decision["message_hash"].(string); !found || len(hash) != 64 {
		t.Fatalf("missing message variant hash: %#v", decision)
	}

	if hash, found := decision["suggestion_hash"].(string); !found || len(hash) != 64 {
		t.Fatalf("missing suggestion variant hash: %#v", decision)
	}

	if _, found := decision["principle_ids"].([]any); !found {
		t.Fatalf("missing principle ids: %#v", decision)
	}

	if keys, found := decision["evidence_keys"].([]any); !found || len(keys) == 0 {
		t.Fatalf("missing evidence key summary: %#v", decision)
	}

	return decision
}

func assertTraceRemediation(t *testing.T, trace map[string]any) {
	t.Helper()

	item := traceMapAt(t, trace, "agent_remediation")
	if item["policy_id"] != customNoSubprocessGitPolicy {
		t.Fatalf("unexpected remediation item: %#v", item)
	}

	if item["failed_action"] != toolBash {
		t.Fatalf("missing failed action: %#v", item)
	}

	finding := traceMapAt(t, trace, "findings")
	if finding["policy_id"] != customNoSubprocessGitPolicy || finding["id"] == "" {
		t.Fatalf("unexpected normalized finding: %#v", finding)
	}

	event := traceMapAt(t, trace, "remediation_events")
	if event["finding_id"] != finding["id"] || event["event"] != "suggested" {
		t.Fatalf("unexpected remediation event: %#v", event)
	}
}

func assertTraceHidesRawProviderInput(t *testing.T, payload []byte) {
	t.Helper()

	raw := string(payload)
	if strings.Contains(raw, "tool_input") || strings.Contains(raw, "ToolInput") {
		t.Fatalf("trace must not dump raw provider input:\n%s", raw)
	}
}

func TestWriteAgentHookTraceUsesUniqueTraceIDForRepeatedViolations(t *testing.T) {
	t.Parallel()

	event := Event{
		HookEventName: eventPreToolUse,
		Source:        providerCodex,
		ToolName:      toolBash,
		Cwd:           "/repo",
		ToolInput: map[string]any{
			"command": "git commit --no-verify -m test",
		},
	}
	result := Result{
		Event:    eventPreToolUse,
		Provider: providerCodex,
		Status:   "blocked",
		Tool:     toolBash,
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
		t.Fatalf(
			"repeated hook traces reused trace_id: first=%#v second=%#v",
			first,
			second,
		)
	}

	firstEvent := traceMapAt(t, first, "remediation_events")

	secondEvent := traceMapAt(t, second, "remediation_events")
	if firstEvent["id"] == secondEvent["id"] {
		t.Fatalf(
			"repeated hook traces reused remediation event id: first=%#v second=%#v",
			firstEvent,
			secondEvent,
		)
	}

	firstFinding := traceMapAt(t, first, "findings")

	secondFinding := traceMapAt(t, second, "findings")
	if firstFinding["id"] != secondFinding["id"] {
		t.Fatalf(
			"finding identity should remain stable: first=%#v second=%#v",
			firstFinding,
			secondFinding,
		)
	}
}

func traceMapAt(
	t *testing.T,
	payload map[string]any,
	key string,
) map[string]any {
	t.Helper()

	values, found := payload[key].([]any)
	if !found {
		t.Fatalf("%s = %#v, want array", key, payload[key])
	}

	if len(values) == 0 {
		t.Fatalf("%s empty array", key)
	}

	value, found := values[0].(map[string]any)
	if !found {
		t.Fatalf("%s[0] = %#v, want object", key, values[0])
	}

	return value
}

func writeTraceAndReadMap(t *testing.T, event Event, result Result) map[string]any {
	t.Helper()

	runDir := t.TempDir()

	inlineErr1 := WriteAgentHookTrace(runDir, event, result)
	if inlineErr1 != nil {
		t.Fatalf("write hook trace: %v", inlineErr1)
	}

	payload, err := os.ReadFile(filepath.Join(runDir, "event.json"))
	if err != nil {
		t.Fatalf("read hook trace: %v", err)
	}

	var trace map[string]any

	inlineErr2 := json.Unmarshal(payload, &trace)
	if inlineErr2 != nil {
		t.Fatalf("parse hook trace: %v\n%s", inlineErr2, payload)
	}

	return trace
}
