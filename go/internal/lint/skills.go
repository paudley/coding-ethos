// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"maps"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EnrichResultWithSkills(result Result, skills map[string]policy.Skill) Result {
	if len(skills) == 0 {
		return result
	}

	result.Diagnostics = enrichDiagnosticsWithSkills(result.Diagnostics, skills)
	result.Findings = enrichFindingsWithSkills(result.Findings, skills)
	result.Decisions = enrichDecisionsWithSkills(result.Decisions, skills)
	result.SkillHints = SkillHintsForDiagnostics(OutputDiagnostics(result), skills)

	return result
}

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
		skillID := skillIDForDiagnostic(item, skills)
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

func enrichDiagnosticsWithSkills(
	items []diagnostics.Diagnostic,
	skills map[string]policy.Skill,
) []diagnostics.Diagnostic {
	if len(items) == 0 {
		return items
	}

	enriched := append([]diagnostics.Diagnostic(nil), items...)
	for index := range enriched {
		if strings.TrimSpace(enriched[index].SkillID) != "" {
			continue
		}

		enriched[index].SkillID = skillIDForDiagnostic(enriched[index], skills)
	}

	return enriched
}

func enrichFindingsWithSkills(
	findings []Finding,
	skills map[string]policy.Skill,
) []Finding {
	if len(findings) == 0 {
		return findings
	}

	enriched := append([]Finding(nil), findings...)
	for index := range enriched {
		if strings.TrimSpace(enriched[index].SkillID) != "" {
			continue
		}

		enriched[index].SkillID = skillIDForFinding(enriched[index], skills)
	}

	return enriched
}

func enrichDecisionsWithSkills(
	decisions []policy.Decision,
	skills map[string]policy.Skill,
) []policy.Decision {
	if len(decisions) == 0 {
		return decisions
	}

	enriched := append([]policy.Decision(nil), decisions...)
	for index := range enriched {
		enriched[index].Diagnostics = enrichDiagnosticsWithSkills(
			enriched[index].Diagnostics,
			skills,
		)
		if stringEvidence(enriched[index].Evidence, "skill_id") != "" {
			continue
		}

		skillID := skillIDForDecision(enriched[index], skills)
		if skillID == "" {
			continue
		}

		evidence := make(map[string]any)
		maps.Copy(evidence, enriched[index].Evidence)

		evidence["skill_id"] = skillID
		enriched[index].Evidence = evidence
	}

	return enriched
}

func skillIDForFinding(finding Finding, skills map[string]policy.Skill) string {
	if strings.TrimSpace(finding.SkillID) != "" {
		return strings.TrimSpace(finding.SkillID)
	}

	return skillIDForSignals(
		finding.EthosIDs,
		[]string{
			finding.CheckID,
			finding.PolicyID,
			finding.SourceTool,
			finding.Code,
			finding.Message,
			finding.Advice,
		},
		skills,
	)
}

func skillIDForDecision(
	decision policy.Decision,
	skills map[string]policy.Skill,
) string {
	if skillID := stringEvidence(decision.Evidence, "skill_id"); skillID != "" {
		return skillID
	}

	return skillIDForSignals(
		decision.PrincipleIDs,
		[]string{
			decision.PolicyID,
			decision.Message,
			decision.Suggestion,
		},
		skills,
	)
}

func skillIDForDiagnostic(
	item diagnostics.Diagnostic,
	skills map[string]policy.Skill,
) string {
	if strings.TrimSpace(item.SkillID) != "" {
		return strings.TrimSpace(item.SkillID)
	}

	return skillIDForSignals(
		item.PrincipleIDs,
		[]string{
			item.Tool,
			item.PolicyID,
			item.Code,
			item.Message,
			item.Advice,
			item.Detail,
		},
		skills,
	)
}

func skillIDForSignals(
	principleIDs []string,
	signals []string,
	skills map[string]policy.Skill,
) string {
	if skillID := skillIDByPrincipleOverlap(principleIDs, skills); skillID != "" {
		return skillID
	}

	return skillIDByTriggerSignal(signals, skills)
}

func skillIDByPrincipleOverlap(
	principleIDs []string,
	skills map[string]policy.Skill,
) string {
	bestID := ""
	bestScore := 0

	for skillID, skill := range skills {
		score := skillPrincipleOverlap(principleIDs, skill.PrincipleIDs)
		if score == 0 {
			continue
		}

		if score > bestScore || (score == bestScore && skillID < bestID) {
			bestID = skillID
			bestScore = score
		}
	}

	return bestID
}

func skillPrincipleOverlap(left, right []string) int {
	score := 0

	for _, principleID := range left {
		normalized := strings.TrimSpace(principleID)
		if normalized == "" {
			continue
		}

		if slices.Contains(right, normalized) {
			score++
		}
	}

	return score
}

func skillIDByTriggerSignal(signals []string, skills map[string]policy.Skill) string {
	bestID := ""

	for skillID, skill := range skills {
		if !skillMatchesSignals(skill, signals) {
			continue
		}

		if bestID == "" || skillID < bestID {
			bestID = skillID
		}
	}

	return bestID
}

func skillMatchesSignals(skill policy.Skill, signals []string) bool {
	for _, trigger := range skill.TriggerTerms {
		normalizedTrigger := strings.ToLower(strings.TrimSpace(trigger))
		if normalizedTrigger == "" {
			continue
		}

		for _, signal := range signals {
			if strings.Contains(
				strings.ToLower(strings.TrimSpace(signal)),
				normalizedTrigger,
			) {
				return true
			}
		}
	}

	return false
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
