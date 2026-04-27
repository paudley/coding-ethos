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
	for _, protectedPath := range protectedPaths() {
		if strings.Contains(context.Command, protectedPath) {
			return blockProtectedPathDecision(policyDef, protectedPath), nil
		}

		for _, file := range context.Files {
			if file == protectedPath || strings.TrimRight(file, "/") == protectedPath {
				return blockProtectedPathDecision(policyDef, protectedPath), nil
			}
		}
	}

	return nil, nil
}

func protectedPaths() []string {
	return []string{"/usr/bin/got"}
}

func blockProtectedPathDecision(
	policyDef policy.Policy,
	protectedPath string,
) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{"path": protectedPath}

	return []policy.Decision{decision}
}
