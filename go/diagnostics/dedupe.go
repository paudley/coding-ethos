// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics

import (
	"slices"
	"strconv"
	"strings"
)

func Dedupe(items []Diagnostic) []Diagnostic {
	if len(items) < 2 {
		return items
	}

	deduped := make([]Diagnostic, 0, len(items))
	seen := map[string]int{}

	for _, item := range items {
		key := dedupeKey(item)
		if key == "" {
			deduped = append(deduped, item)

			continue
		}

		index, ok := seen[key]
		if !ok {
			seen[key] = len(deduped)
			deduped = append(deduped, item)

			continue
		}

		deduped[index] = mergeDiagnostic(deduped[index], item)
	}

	return deduped
}

func dedupeKey(item Diagnostic) string {
	file := strings.TrimSpace(item.File)
	if file == "" {
		return ""
	}

	class := strings.TrimSpace(item.PolicyID)
	if class == "" {
		class = strings.TrimSpace(item.Tool) + ":" + strings.TrimSpace(item.Code)
	}

	if class == ":" || class == "" {
		class = strings.TrimSpace(item.Message)
	}

	if class == "" {
		return ""
	}

	location := file
	if item.Line > 0 {
		location += ":" + strconv.Itoa(item.Line)
	}

	if item.Column > 0 {
		location += ":" + strconv.Itoa(item.Column)
	}

	return strings.ToLower(class + "|" + location)
}

func mergeDiagnostic(primary, duplicate Diagnostic) Diagnostic {
	if primary.PolicyID == "" && duplicate.PolicyID != "" {
		primary.PolicyID = duplicate.PolicyID
	}

	if primary.SkillID == "" && duplicate.SkillID != "" {
		primary.SkillID = duplicate.SkillID
	}

	if primary.Advice == "" && duplicate.Advice != "" {
		primary.Advice = duplicate.Advice
	}

	if primary.Meaning == "" && duplicate.Meaning != "" {
		primary.Meaning = duplicate.Meaning
	}

	if primary.Confidence == "" && duplicate.Confidence != "" {
		primary.Confidence = duplicate.Confidence
	}

	if primary.Code == "" && duplicate.Code != "" {
		primary.Code = duplicate.Code
	}

	if primary.Severity == "" && duplicate.Severity != "" {
		primary.Severity = duplicate.Severity
	}

	primary.PrincipleIDs = appendUnique(primary.PrincipleIDs, duplicate.PrincipleIDs...)
	primary.AdviceSteps = appendUnique(primary.AdviceSteps, duplicate.AdviceSteps...)
	primary.Rerun = appendUnique(primary.Rerun, duplicate.Rerun...)
	primary.Tags = appendUnique(primary.Tags, duplicate.Tags...)
	primary.Detail = mergedDiagnosticDetail(primary, duplicate)

	return primary
}

func mergedDiagnosticDetail(primary, duplicate Diagnostic) string {
	parts := []string{}
	if strings.TrimSpace(primary.Detail) != "" {
		parts = append(parts, primary.Detail)
	}

	also := duplicate.Tool
	if duplicate.Code != "" {
		also += ":" + duplicate.Code
	}

	if also != "" && !strings.Contains(strings.Join(parts, " "), also) {
		parts = append(parts, "also reported by "+also)
	}

	return strings.Join(parts, "; ")
}

func appendUnique(values []string, extra ...string) []string {
	for _, value := range extra {
		value = strings.TrimSpace(value)
		if value == "" || slices.Contains(values, value) {
			continue
		}

		values = append(values, value)
	}

	return values
}
