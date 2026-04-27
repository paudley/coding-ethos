// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // Claude hook output contract uses camelCase fields.
package hooks

import "blackcat.ca/coding-ethos/go/internal/policy"

type Result struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
	Event              string              `json:"event"`
	Status             string              `json:"status"`
	Tool               string              `json:"tool,omitempty"`
	Decisions          []policy.Decision   `json:"decisions,omitempty"`
}

func (result Result) Blocked() bool {
	return result.Status == statusBlocked
}

type HookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext,omitempty"`
}
