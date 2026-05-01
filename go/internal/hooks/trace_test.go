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
			PolicyID: "git.wrapper_required",
			Decision: "block",
			Severity: "block",
			Message:  "Use the managed wrapper.",
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
	if !ok || decision["policy_id"] != "git.wrapper_required" {
		t.Fatalf("unexpected decision trace: %#v", decisions)
	}

	raw := string(payload)
	if strings.Contains(raw, "tool_input") || strings.Contains(raw, "ToolInput") {
		t.Fatalf("trace must not dump raw provider input:\n%s", raw)
	}
}
