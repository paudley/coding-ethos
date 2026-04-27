// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

type Decision struct {
	Evidence     map[string]any `json:"evidence,omitempty"`
	Decision     string         `json:"decision"`
	Message      string         `json:"message"`
	PolicyID     string         `json:"policy_id"`
	Severity     string         `json:"severity"`
	Suggestion   string         `json:"suggestion,omitempty"`
	PrincipleIDs []string       `json:"principle_ids,omitempty"`
}

func NewDecision(decision string, policy Policy) Decision {
	return Decision{
		Decision:     decision,
		PolicyID:     policy.ID,
		Severity:     policy.DefaultSeverity,
		PrincipleIDs: append([]string(nil), policy.PrincipleIDs...),
		Message:      policy.Message,
		Suggestion:   policy.Suggestion,
	}
}
