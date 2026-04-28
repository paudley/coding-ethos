// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"blackcat.ca/coding-ethos/go/internal/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type Result struct {
	Scope       string                   `json:"scope"`
	Status      string                   `json:"status"`
	Decisions   []policy.Decision        `json:"decisions"`
	Diagnostics []diagnostics.Diagnostic `json:"diagnostics,omitempty"`
	Files       []string                 `json:"files,omitempty"`
}

func (result Result) Blocked() bool {
	return result.Status == "blocked"
}
