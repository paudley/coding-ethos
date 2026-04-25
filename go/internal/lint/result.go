// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import "blackcat.ca/coding-ethos/go/internal/policy"

type Result struct {
	Decisions []policy.Decision `json:"decisions"`
	Files     []string          `json:"files,omitempty"`
	Scope     string            `json:"scope"`
	Status    string            `json:"status"`
}
