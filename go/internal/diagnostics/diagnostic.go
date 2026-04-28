// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics

type Diagnostic struct {
	Metadata     map[string]any `json:"metadata,omitempty"`
	Advice       string         `json:"advice,omitempty"`
	Code         string         `json:"code,omitempty"`
	File         string         `json:"file,omitempty"`
	Function     string         `json:"function,omitempty"`
	Group        string         `json:"group,omitempty"`
	Message      string         `json:"message"`
	PolicyID     string         `json:"policy_id,omitempty"`
	Severity     string         `json:"severity,omitempty"`
	Tool         string         `json:"tool"`
	PrincipleIDs []string       `json:"principle_ids,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Column       int            `json:"column,omitempty"`
	Line         int            `json:"line,omitempty"`
}
