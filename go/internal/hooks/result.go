// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // Claude hook output contract uses camelCase fields.
package hooks

import "blackcat.ca/coding-ethos/go/internal/policy"

type Result struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
	Event              string              `json:"event"`
	Advice             policy.Advice       `json:"advice,omitempty"`
	Provider           string              `json:"provider,omitempty"`
	Status             string              `json:"status"`
	TrackingID         string              `json:"trackingID,omitempty"`
	Tool               string              `json:"tool,omitempty"`
	Decisions          []policy.Decision   `json:"decisions,omitempty"`
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
