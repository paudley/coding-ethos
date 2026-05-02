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
		Event:    "PreToolUse",
		Provider: "codex",
		Status:   "blocked",
		Tool:     "Bash",
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
	if _, ok := decision["principle_ids"].([]any); !ok {
		t.Fatalf("missing principle ids: %#v", decision)
	}
	if keys, ok := decision["evidence_keys"].([]any); !ok || len(keys) == 0 {
		t.Fatalf("missing evidence key summary: %#v", decision)
	}

	raw := string(payload)
	if strings.Contains(raw, "tool_input") || strings.Contains(raw, "ToolInput") {
		t.Fatalf("trace must not dump raw provider input:\n%s", raw)
	}
}
