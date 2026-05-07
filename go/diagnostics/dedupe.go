// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics

import (
	"slices"
	"strconv"
	"strings"
)

const dedupeMinimumItems = 2

func Dedupe(items []Diagnostic) []Diagnostic {
	if len(items) < dedupeMinimumItems {
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
	mergeDiagnosticScalars(&primary, duplicate)

	primary.PrincipleIDs = appendUnique(primary.PrincipleIDs, duplicate.PrincipleIDs...)
	primary.AdviceSteps = appendUnique(primary.AdviceSteps, duplicate.AdviceSteps...)
	primary.Rerun = appendUnique(primary.Rerun, duplicate.Rerun...)
	primary.Tags = appendUnique(primary.Tags, duplicate.Tags...)
	primary.Detail = mergedDiagnosticDetail(primary, duplicate)

	return primary
}

func mergeDiagnosticScalars(primary *Diagnostic, duplicate Diagnostic) {
	fillString(&primary.PolicyID, duplicate.PolicyID)
	fillString(&primary.SkillID, duplicate.SkillID)
	fillString(&primary.Advice, duplicate.Advice)
	fillString(&primary.Meaning, duplicate.Meaning)
	fillString(&primary.Confidence, duplicate.Confidence)
	fillString(&primary.Code, duplicate.Code)
	fillString(&primary.Severity, duplicate.Severity)
}

func fillString(target *string, value string) {
	if *target == "" && value != "" {
		*target = value
	}
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
