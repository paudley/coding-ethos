// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func SkillHintsForDiagnostics(
	items []diagnostics.Diagnostic,
	skills map[string]policy.Skill,
) []SkillHint {
	if len(items) == 0 || len(skills) == 0 {
		return nil
	}

	hints := []SkillHint{}
	seen := map[string]bool{}
	for _, item := range items {
		skillID := strings.TrimSpace(item.SkillID)
		if skillID == "" || seen[skillID] {
			continue
		}
		skill, ok := skills[skillID]
		if !ok {
			continue
		}

		hint := SkillHint{
			PrincipleID: firstSkillHintNonEmpty(
				firstSkillHintPrinciple(item.PrincipleIDs),
				firstSkillHintPrinciple(skill.PrincipleIDs),
			),
			SkillID: skillID,
			Message: firstSkillHintNonEmpty(
				skill.ShortHint,
				skill.Description,
			),
			Next: "Load the " + skillID + " skill for the remediation playbook.",
		}
		if hint.Message == "" {
			continue
		}

		hints = append(hints, hint)
		seen[skillID] = true
	}

	return hints
}

func firstSkillHintPrinciple(principles []string) string {
	for _, principle := range principles {
		if strings.TrimSpace(principle) != "" {
			return principle
		}
	}

	return ""
}

func firstSkillHintNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
