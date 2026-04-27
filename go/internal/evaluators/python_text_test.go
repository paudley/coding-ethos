// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluatePythonConditionalImports(t *testing.T) {
	decision := evaluatePythonPolicy(t, "python.conditional_imports", "try:\n    import missing\nexcept ImportError:\n    missing = None\n")
	if decision.PolicyID != "python.conditional_imports" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonOptionalReturns(t *testing.T) {
	decision := evaluatePythonPolicy(t, "python.optional_returns", "def dependency() -> Service | None:\n    return None\n")
	if decision.PolicyID != "python.optional_returns" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonCatchAndSilence(t *testing.T) {
	decision := evaluatePythonPolicy(t, "python.catch_and_silence", "try:\n    run()\nexcept Exception:\n    pass\n")
	if decision.PolicyID != "python.catch_and_silence" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonStructuredLogging(t *testing.T) {
	decision := evaluatePythonPolicy(t, "python.structured_logging", "logger.info(f'user={user_id}')\n")
	if decision.PolicyID != "python.structured_logging" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonDirectImportsAllowsPackageInternalImports(t *testing.T) {
	policyDef := compiledPythonPolicy(t, "python.direct_imports")
	decisions, err := EvaluatePythonDirectImports(policyDef, Context{
		Files:   []string{"coding_ethos/cli.py"},
		Content: "from coding_ethos.loaders import load\n",
	})
	if err != nil {
		t.Fatalf("evaluate direct imports: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected internal import allowed, got %#v", decisions)
	}
}

func evaluatePythonPolicy(t *testing.T, policyID string, content string) policy.Decision {
	t.Helper()
	policyDef := compiledPythonPolicy(t, policyID)
	evaluator, ok := DefaultRegistry().Lookup(policyID)
	if !ok {
		t.Fatalf("missing evaluator %s", policyID)
	}
	decisions, err := evaluator.Evaluate(policyDef, Context{
		Files:   []string{"src/app.py"},
		Content: content,
	})
	if err != nil {
		t.Fatalf("evaluate %s: %v", policyID, err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
	return decisions[0]
}

func compiledPythonPolicy(t *testing.T, policyID string) policy.Policy {
	t.Helper()
	bundle := policy.ExampleBundle()
	for _, id := range []string{
		"python.optional_returns",
		"python.catch_and_silence",
		"python.structured_logging",
		"python.direct_imports",
	} {
		if _, ok := bundle.Policies[id]; !ok {
			base := bundle.Policies["python.conditional_imports"]
			base.ID = id
			base.Evaluators = []policy.Evaluator{{Kind: "ast", Name: id}}
			bundle.Policies[id] = base
		}
	}
	policyDef, ok := bundle.Policies[policyID]
	if !ok {
		t.Fatalf("missing policy %s", policyID)
	}
	return policyDef
}
