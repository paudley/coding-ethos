// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:tagliatelle // Claude hook output contract uses camelCase fields.
package hooks

import (
	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

// AgentHookBlockedExitCode is the historical hook CLI exit code for a denied
// action. Provider-native adapters may override it when their hook protocol
// assigns a different blocking status.
const (
	AgentHookBlockedExitCode = 1
	kimiBlockedExitCode      = 2
)

// AgentHookBlockedExitCodeForProvider returns the provider-native blocking exit
// status while preserving the historical standalone status for existing hooks.
func AgentHookBlockedExitCodeForProvider(provider string) int {
	if provider == providerKimi {
		return kimiBlockedExitCode
	}

	return AgentHookBlockedExitCode
}

type Result struct {
	HookSpecificOutput *HookSpecificOutput        `json:"hookSpecificOutput,omitempty"`
	ProxyEvents        []agentproxy.ProviderEvent `json:"-"`
	CorrelationID      string                     `json:"correlation_id,omitempty"`
	Event              string                     `json:"event"`
	Provider           string                     `json:"provider,omitempty"`
	Status             string                     `json:"status"`
	TrackingID         string                     `json:"trackingID,omitempty"`
	Tool               string                     `json:"tool,omitempty"`
	Decisions          []policy.Decision          `json:"decisions,omitempty"`
	Advice             policy.Advice              `json:"advice,omitzero"`
	RuntimeMS          int64                      `json:"runtime_ms,omitempty"`
}

func (result Result) Blocked() bool {
	return result.Status == statusBlocked
}

type HookSpecificOutput struct {
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
	HookEventName            string         `json:"hookEventName"`
	AdditionalContext        string         `json:"additionalContext,omitempty"`
	PermissionDecision       string         `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string         `json:"permissionDecisionReason,omitempty"`
}
