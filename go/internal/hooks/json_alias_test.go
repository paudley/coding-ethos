// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"strings"
	"testing"
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

	if event.ToolName != "Bash" || event.Command() != "git status --short" {
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

	if event.ToolName != "Bash" || event.Command() != "git status --short\n" {
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

	if event.ToolName != "Bash" || event.Command() != "git status --short" {
		t.Fatalf("event mismatch: %#v", event)
	}
}
