// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import "blackcat.ca/coding-ethos/go/internal/policy"

func EvaluatePythonBareExcept(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonBareExceptIssue)
}

func EvaluatePythonUnexplainedTypeIgnore(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	return evaluatePythonAST(policyDef, context, firstPythonUnexplainedTypeIgnoreIssue)
}

func firstPythonBareExceptIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if fact.IsBareExcept {
			return newPythonASTIssueFromFact(
				fact,
				"bare-except",
				"Bare except clauses hide exception types and are forbidden.",
			)
		}
	}

	return nil
}

func firstPythonUnexplainedTypeIgnoreIssue(facts []pythonASTFact) *pythonASTIssue {
	for _, fact := range facts {
		if fact.IsUnexplainedTypeIgnore {
			return newPythonASTIssueFromFact(
				fact,
				"unexplained-type-ignore",
				"Type ignore suppressions require a narrow explanation.",
			)
		}
	}

	return nil
}
