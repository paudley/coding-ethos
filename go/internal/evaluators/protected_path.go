// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateProtectedPath(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := normalizeProtectedPath(context.Command)
	files := normalizedProtectedFiles(context.Files)
	for _, protectedPath := range normalizedProtectedPaths(context) {
		if strings.Contains(command, protectedPath) {
			return blockProtectedPathDecision(policyDef, protectedPath), nil
		}

		for _, file := range files {
			if protectedPathMatches(file, protectedPath) {
				return blockProtectedPathDecision(policyDef, protectedPath), nil
			}
		}
	}

	return nil, nil
}

func protectedPaths(context Context) []string {
	return stringSliceOption(
		context.EvaluatorOptions,
		"paths",
		[]string{"coding-ethos-hooks/coding-ethos-git-hook"},
	)
}

func protectedPathMatches(file string, protectedPath string) bool {
	if file == protectedPath {
		return true
	}

	if strings.HasSuffix(file, protectedPath) {
		return true
	}

	return strings.HasPrefix(file, protectedPath+"/")
}

func normalizedProtectedPaths(context Context) []string {
	paths := protectedPaths(context)
	cleaned := make([]string, 0, len(paths))
	for _, path := range paths {
		cleaned = append(cleaned, normalizeProtectedPath(path))
	}

	return cleaned
}

func normalizedProtectedFiles(files []string) []string {
	cleaned := make([]string, 0, len(files))
	for _, file := range files {
		cleaned = append(cleaned, normalizeProtectedPath(file))
	}

	return cleaned
}

func normalizeProtectedPath(path string) string {
	return strings.Trim(strings.ReplaceAll(path, "\\", "/"), "/")
}

func blockProtectedPathDecision(
	policyDef policy.Policy,
	protectedPath string,
) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{"path": protectedPath}

	return []policy.Decision{decision}
}
