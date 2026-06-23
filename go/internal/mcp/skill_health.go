// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func (server Server) codeIntelSkillHealth(args json.RawMessage) (any, error) {
	var input codeIntelSkillHealthInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse code intelligence skill health arguments: %w", err)
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	defer closeStore()

	report, err := store.SkillHealth(argsContext(), codeintel.SkillHealthQuery{
		SkillID:     input.SkillID,
		KnownSkills: server.skillHealthProvenance(),
		Limit:       input.Limit,
		StaleDays:   input.StaleDays,
	})
	if err != nil {
		return nil, fmt.Errorf("query skill health: %w", err)
	}

	result := map[string]any{
		"kind":             report.Kind,
		"generated_at_utc": report.GeneratedAtUTC,
		"promotion_policy": report.PromotionPolicy,
		"summary":          report.Summary,
		"windows":          report.Windows,
		"skills":           report.Skills,
	}
	if strings.EqualFold(strings.TrimSpace(input.Format), "toon") {
		result["content"] = codeintel.FormatSkillHealthTOON(report)
	}

	return result, nil
}

func (server Server) recordSkillLookupObservation(input skillLookupInput) error {
	return server.recordSkillObservation(codeintel.SkillObservation{
		SkillID:  input.SkillID,
		Provider: "mcp",
		Tool:     "skill_lookup",
		Surface:  "mcp.skill_lookup",
		Outcome:  "unknown",
		Trigger:  "lookup",
	})
}

func (server Server) recordSkillRecommendationObservations(
	input skillRecommendInput,
	recommendations []map[string]any,
) error {
	if len(recommendations) == 0 || strings.TrimSpace(server.codeIntelRoot()) == "" {
		return nil
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return fmt.Errorf("open code intelligence store for skill observations: %w", err)
	}
	defer closeStore()

	enriched := server.enrichedDiagnostic(input.Diagnostic)

	for _, recommendation := range recommendations {
		skillID, ok := recommendation["id"].(string)
		if !ok {
			return apperror.StaticError("skill recommendation missing id")
		}

		err = store.RecordSkillObservation(argsContext(), codeintel.SkillObservation{
			SkillID:  skillID,
			PolicyID: enriched.PolicyID,
			Path: firstNonEmpty(
				input.Path,
				input.Diagnostic.File,
				enriched.File,
			),
			Provider: "mcp",
			Tool:     "skill_recommend",
			Surface:  "mcp.skill_recommend",
			Outcome:  "unknown",
			Trigger: strings.Join(compactMCPStrings([]string{
				input.Intent,
				input.Command,
				input.Diagnostic.Tool,
				input.Diagnostic.Code,
				input.Diagnostic.Message,
			}), " "),
		})
		if err != nil {
			return fmt.Errorf("record skill observation: %w", err)
		}
	}

	return nil
}

func (server Server) recordSkillObservation(
	observation codeintel.SkillObservation,
) error {
	if strings.TrimSpace(server.codeIntelRoot()) == "" {
		return nil
	}

	store, closeStore, err := server.openCodeIntelStore()
	if err != nil {
		return fmt.Errorf("open code intelligence store for skill observation: %w", err)
	}
	defer closeStore()

	err = store.RecordSkillObservation(argsContext(), observation)
	if err != nil {
		return fmt.Errorf("record skill observation: %w", err)
	}

	return nil
}

func (server Server) skillHealthProvenance() []codeintel.SkillProvenance {
	skills := make([]policy.Skill, 0, len(server.bundle.Skills))
	for _, skill := range server.bundle.Skills {
		skills = append(skills, skill)
	}

	slices.SortFunc(skills, func(left, right policy.Skill) int {
		return strings.Compare(left.ID, right.ID)
	})

	provenance := make([]codeintel.SkillProvenance, 0, len(skills))
	for _, skill := range skills {
		provenance = append(provenance, codeintel.SkillProvenance{
			ID:         skill.ID,
			Title:      skill.Title,
			Source:     skill.Source.File,
			SourcePath: skill.Source.Path,
			Generated:  true,
		})
	}

	return provenance
}

func compactMCPStrings(values []string) []string {
	result := []string{}

	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}

	return result
}
