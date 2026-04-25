// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "blackcat.ca/coding-ethos/go/internal/policy"

type Result struct {
	Decisions []policy.Decision `json:"decisions,omitempty"`
	Event     string            `json:"event"`
	Status    string            `json:"status"`
	Tool      string            `json:"tool,omitempty"`
}

func (result Result) Blocked() bool {
	return result.Status == "blocked"
}
