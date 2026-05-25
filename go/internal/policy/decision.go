// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
)

type Decision struct {
	Evidence     map[string]any           `json:"evidence,omitempty"`
	Decision     string                   `json:"decision"`
	Message      string                   `json:"message"`
	PolicyID     string                   `json:"policy_id"`
	Severity     string                   `json:"severity"`
	Suggestion   string                   `json:"suggestion,omitempty"`
	Diagnostics  []diagnostics.Diagnostic `json:"diagnostics,omitempty"`
	PrincipleIDs []string                 `json:"principle_ids,omitempty"`
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

func (decision Decision) EvidenceFiles() []string {
	if files := evidenceStringList(decision.Evidence, "files"); len(files) > 0 {
		return files
	}

	return evidenceStringList(decision.Evidence, "staged_files")
}

func evidenceStringList(evidence map[string]any, key string) []string {
	if len(evidence) == 0 {
		return nil
	}

	value, found := evidence[key]
	if !found {
		return nil
	}

	switch typed := value.(type) {
	case []string:
		return normalizedEvidenceStrings(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if ok {
				values = append(values, text)
			}
		}

		return normalizedEvidenceStrings(values)
	default:
		return nil
	}
}

func normalizedEvidenceStrings(values []string) []string {
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			normalized = append(normalized, text)
		}
	}

	return normalized
}
