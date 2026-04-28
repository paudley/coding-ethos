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
	for _, protectedPath := range protectedPaths(context) {
		if strings.Contains(context.Command, protectedPath) {
			return blockProtectedPathDecision(policyDef, protectedPath), nil
		}

		for _, file := range context.Files {
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
		[]string{"/coding-ethos-hooks/coding-ethos-git-hook"},
	)
}

func protectedPathMatches(file string, protectedPath string) bool {
	cleanFile := strings.Trim(strings.ReplaceAll(file, "\\", "/"), "/")
	cleanProtected := strings.Trim(strings.ReplaceAll(protectedPath, "\\", "/"), "/")
	if cleanFile == cleanProtected {
		return true
	}

	if strings.HasPrefix(cleanProtected, "coding-ethos-hooks/") &&
		strings.HasSuffix(cleanFile, cleanProtected) {
		return true
	}

	return strings.HasSuffix(protectedPath, "/") &&
		strings.HasPrefix(cleanFile, cleanProtected+"/")
}

func blockProtectedPathDecision(
	policyDef policy.Policy,
	protectedPath string,
) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{"path": protectedPath}

	return []policy.Decision{decision}
}
