// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics

type Diagnostic struct {
	Metadata     map[string]any `json:"metadata,omitempty"`
	Advice       string         `json:"advice,omitempty"`
	Confidence   string         `json:"confidence,omitempty"`
	Code         string         `json:"code,omitempty"`
	Detail       string         `json:"detail,omitempty"`
	File         string         `json:"file,omitempty"`
	Function     string         `json:"function,omitempty"`
	Group        string         `json:"group,omitempty"`
	Meaning      string         `json:"meaning,omitempty"`
	Message      string         `json:"message"`
	PolicyID     string         `json:"policy_id,omitempty"`
	Severity     string         `json:"severity,omitempty"`
	Tool         string         `json:"tool"`
	AdviceSteps  []string       `json:"advice_steps,omitempty"`
	PrincipleIDs []string       `json:"principle_ids,omitempty"`
	Rerun        []string       `json:"rerun,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Column       int            `json:"column,omitempty"`
	Line         int            `json:"line,omitempty"`
}

type EvidenceAdvice struct {
	Summary string   `json:"summary,omitempty"`
	Steps   []string `json:"steps,omitempty"`
	Rerun   []string `json:"rerun,omitempty"`
}

type EvidenceMap struct {
	Advice       EvidenceAdvice `json:"advice"`
	Source       string         `json:"source"`
	PolicyID     string         `json:"policy_id"`
	Confidence   string         `json:"confidence,omitempty"`
	Meaning      string         `json:"meaning,omitempty"`
	Codes        []string       `json:"codes,omitempty"`
	PrincipleIDs []string       `json:"principle_ids,omitempty"`
}
