// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics

import "strings"

func Enrich(items []Diagnostic, evidenceMaps []EvidenceMap) []Diagnostic {
	if len(items) == 0 || len(evidenceMaps) == 0 {
		return items
	}

	enriched := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		mapping, ok := evidenceMapForDiagnostic(item, evidenceMaps)
		if ok {
			item.PolicyID = mapping.PolicyID
			item.PrincipleIDs = append([]string(nil), mapping.PrincipleIDs...)
			item.Confidence = mapping.Confidence
			item.Meaning = mapping.Meaning
			item.Advice = mapping.Advice.Summary
			item.AdviceSteps = append([]string(nil), mapping.Advice.Steps...)
			item.Rerun = append([]string(nil), mapping.Advice.Rerun...)
		}

		enriched = append(enriched, item)
	}

	return enriched
}

func evidenceMapForDiagnostic(
	item Diagnostic,
	evidenceMaps []EvidenceMap,
) (EvidenceMap, bool) {
	for _, mapping := range evidenceMaps {
		if !strings.EqualFold(strings.TrimSpace(mapping.Source), item.Tool) {
			continue
		}

		for _, code := range mapping.Codes {
			if diagnosticCodeMatches(code, item.Code) {
				return mapping, true
			}
		}
	}

	return EvidenceMap{}, false
}

func diagnosticCodeMatches(pattern string, code string) bool {
	normalizedPattern := strings.ToLower(strings.TrimSpace(pattern))
	normalizedCode := strings.ToLower(strings.TrimSpace(code))

	if normalizedPattern == "" || normalizedCode == "" {
		return false
	}

	if normalizedPattern == "*" {
		return true
	}

	if prefix, wildcard := strings.CutSuffix(normalizedPattern, "*"); wildcard {
		return strings.HasPrefix(normalizedCode, prefix)
	}

	return normalizedPattern == normalizedCode
}
