// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package gitwrap

import "blackcat.ca/coding-ethos/go/internal/policy"

type Result struct {
	Operation string            `json:"operation,omitempty"`
	Status    string            `json:"status"`
	Decisions []policy.Decision `json:"decisions,omitempty"`
	Argv      []string          `json:"argv"`
}

func (result Result) Blocked() bool {
	return result.Status == "blocked"
}
