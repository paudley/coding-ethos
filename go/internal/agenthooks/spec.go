// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks

// Provider identifies an agent product hook surface.
type Provider string

const (
	// ProviderClaude renders Claude Code settings.local.json hook entries.
	ProviderClaude Provider = "claude"
	// ProviderCodex renders a Codex-owned coding-ethos hook manifest.
	ProviderCodex Provider = "codex"
	// ProviderGemini renders a Gemini-owned coding-ethos hook manifest.
	ProviderGemini Provider = "gemini"
)

// HookSpec describes the provider-neutral hook surface the runtime protects.
type HookSpec struct {
	Event string
	Tool  string
}

// RuntimeHookSpecs returns the agent lifecycle events currently covered by the
// coding-ethos runtime. Empty Tool means the event is not tool-specific.
func RuntimeHookSpecs() []HookSpec {
	return []HookSpec{
		{Event: "PreToolUse", Tool: "Bash"},
		{Event: "PreToolUse", Tool: "Write"},
		{Event: "PreToolUse", Tool: "Edit"},
		{Event: "PreToolUse", Tool: "MultiEdit"},
		{Event: "PostToolUse", Tool: "Bash"},
		{Event: "PostToolUse", Tool: "Write"},
		{Event: "PostToolUse", Tool: "Edit"},
		{Event: "PostToolUse", Tool: "MultiEdit"},
		{Event: "PostToolBatch"},
		{Event: "PreCompact"},
		{Event: "SessionStart"},
		{Event: "UserPromptSubmit"},
		{Event: "Stop"},
		{Event: "SessionEnd"},
		{Event: "SubagentStart"},
		{Event: "SubagentStop"},
	}
}
