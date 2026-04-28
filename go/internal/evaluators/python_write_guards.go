// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"regexp"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

var (
	bareExceptPattern = regexp.MustCompile(`(?m)^\s*except\s*:\s*$`)
	typeIgnorePattern = regexp.MustCompile(`(?m)#\s*type:\s*ignore\s*$`)
)

func EvaluatePythonBareExcept(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if !bareExceptPattern.MatchString(context.Content) {
		return nil, nil
	}

	return []policy.Decision{pythonContentDecision(policyDef, context)}, nil
}

func EvaluatePythonUnexplainedTypeIgnore(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if !typeIgnorePattern.MatchString(context.Content) {
		return nil, nil
	}

	return []policy.Decision{pythonContentDecision(policyDef, context)}, nil
}

func pythonContentDecision(policyDef policy.Policy, context Context) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     policyDef.ID,
		File:     firstFile(context.Files),
		Severity: blockDecision,
		PolicyID: policyDef.ID,
		Message:  policyDef.Message,
		Advice:   policyDef.Suggestion,
	}}
	if len(context.Files) > 0 {
		decision.Evidence = map[string]any{"files": append([]string(nil), context.Files...)}
	}

	return decision
}
