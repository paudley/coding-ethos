// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
)

func TestDecodeEventNormalizesNamespacedCodexShellTool(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"provider": "codex",
		"event": "PreToolUse",
		"tool": "functions.exec_command",
		"input": {"cmd": "git status --short"}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	if event.ToolName != toolBash || event.Command() != "git status --short" {
		t.Fatalf("event mismatch: %#v", event)
	}
}

func TestDecodeEventNormalizesCodexWriteStdinAsShellTool(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"provider": "codex",
		"event": "PreToolUse",
		"tool": "functions.write_stdin",
		"input": {"session_id": 7, "chars": "git status --short\n"}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	if event.ToolName != toolBash || event.Command() != "git status --short\n" {
		t.Fatalf("event mismatch: %#v", event)
	}
}

func TestDecodeEventNormalizesParallelNestedCodexTool(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"provider": "codex",
		"event": "PreToolUse",
		"tool": "multi_tool_use.parallel",
		"input": {
			"tool_uses": [
				{
					"recipient_name": "functions.exec_command",
					"parameters": {"cmd": "git status --short"}
				}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	if event.ToolName != toolBash || event.Command() != "git status --short" {
		t.Fatalf("event mismatch: %#v", event)
	}
}

func TestDecodeEventKeepsMultiActionParallelBatchForPolicy(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"provider": "codex",
		"event": "PreToolUse",
		"tool": "multi_tool_use.parallel",
		"input": {
			"tool_uses": [
				{
					"recipient_name": "functions.exec_command",
					"parameters": {"cmd": "git status --short"}
				},
				{
					"recipient_name": "functions.exec_command",
					"parameters": {"cmd": "git log --oneline -1"}
				}
			]
		}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	toolUses, ok := event.ToolInput["tool_uses"].([]any)
	if !ok {
		t.Fatalf("tool_uses = %#v, want array", event.ToolInput["tool_uses"])
	}

	if event.ToolName != toolBash ||
		event.ToolInput["__coding_ethos_parallel_batch"] != true ||
		len(toolUses) != 2 {
		t.Fatalf("event mismatch: %#v", event)
	}
}
