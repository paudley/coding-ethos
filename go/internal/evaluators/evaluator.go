// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import "blackcat.ca/coding-ethos/go/internal/policy"

type Evaluator interface {
	Evaluate(policyDef policy.Policy, context Context) ([]policy.Decision, error)
}

type EvaluatorFunc func(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error)

func (fn EvaluatorFunc) Evaluate(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return fn(policyDef, context)
}
