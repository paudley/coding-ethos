// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks

// Provider identifies an agent product hook surface.
type Provider string

const (
	// ProviderClaude renders Claude Code settings.local.json hook entries.
	ProviderClaude Provider = "claude"
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
		{Event: "PreCompact"},
		{Event: "SessionStart"},
	}
}

// ParseProvider validates a provider name accepted by the settings renderer.
func ParseProvider(name string) (Provider, error) {
	if name == "" {
		return ProviderClaude, nil
	}

	provider := Provider(name)
	switch provider {
	case ProviderClaude:
		return provider, nil
	default:
		return "", errUnsupportedProvider
	}
}
