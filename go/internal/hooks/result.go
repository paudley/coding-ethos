// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "blackcat.ca/coding-ethos/go/internal/policy"

type Result struct {
	Event     string            `json:"event"`
	Status    string            `json:"status"`
	Tool      string            `json:"tool,omitempty"`
	Decisions []policy.Decision `json:"decisions,omitempty"`
}

func (result Result) Blocked() bool {
	return result.Status == statusBlocked
}
