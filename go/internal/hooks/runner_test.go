// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunBlocksGitHookBypass(t *testing.T) {
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git commit --no-verify -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if result.Status != "blocked" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}
	if result.Decisions[0].PolicyID != "git.hook_bypass" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestRunAllowsNormalGitCommit(t *testing.T) {
	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git commit -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}
	if result.Status != "allowed" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func TestDecodeEventReadsClaudeLikePayload(t *testing.T) {
	event, err := DecodeEvent(strings.NewReader(`{
		"hook_event_name": "PreToolUse",
		"tool_name": "Bash",
		"tool_input": {"command": "git status"}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
		t.Fatalf("event mismatch: %#v", event)
	}
	if event.Command() != "git status" {
		t.Fatalf("command mismatch: %q", event.Command())
	}
}
