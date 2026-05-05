// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluatePythonConditionalImports(t *testing.T) {
	t.Parallel()

	decision := evaluatePythonPolicy(
		t,
		"python.conditional_imports",
		"try:\n    import missing\nexcept ImportError:\n    missing = None\n",
	)
	if decision.PolicyID != "python.conditional_imports" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonConditionalImportsCatchesRuntimeWorkarounds(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"local_import": `def load_plugin():
    import plugin
    return plugin
`,
		"type_checking_import": `from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from service import Service
`,
		"dunder_getattr": `def __getattr__(name):
    import plugin
    return getattr(plugin, name)
`,
		"dynamic_import": `import importlib

plugin = importlib.import_module("plugin")
`,
	}

	for name, content := range cases {
		content := content
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision := evaluatePythonPolicy(t, "python.conditional_imports", content)
			if decision.PolicyID != "python.conditional_imports" {
				t.Fatalf("policy mismatch: %#v", decision)
			}
			if len(decision.Diagnostics) != 1 || decision.Diagnostics[0].Code == "" {
				t.Fatalf("expected AST diagnostic code: %#v", decision.Diagnostics)
			}
		})
	}
}

func TestEvaluatePythonConditionalImportsCatchesMalformedEditSnippets(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"indented_import":       "    import plugin\n",
		"import_error_fallback": "except ImportError:\n    plugin = None\n",
	}

	for name, content := range cases {
		content := content
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision := evaluatePythonPolicy(t, "python.conditional_imports", content)
			if decision.PolicyID != "python.conditional_imports" {
				t.Fatalf("policy mismatch: %#v", decision)
			}
			if len(decision.Diagnostics) != 1 || decision.Diagnostics[0].Code == "" {
				t.Fatalf("expected snippet diagnostic code: %#v", decision.Diagnostics)
			}
		})
	}
}

func TestEvaluatePythonConditionalImportsAllowsModuleImports(t *testing.T) {
	t.Parallel()

	policyDef := compiledPythonPolicy(t, "python.conditional_imports")

	decisions, err := EvaluatePythonConditionalImports(policyDef, Context{
		Files:   []string{"src/app.py"},
		Content: "import os\nfrom pathlib import Path\n",
	})
	if err != nil {
		t.Fatalf("evaluate conditional imports: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected module imports allowed, got %#v", decisions)
	}
}

func TestEvaluatePythonFunctionalIdioms(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"assigned_lambda": `normalize = lambda value: value.strip().lower()
`,
		"closure_factory": `def make_handler(prefix):
    def handler(value):
        return prefix + value
    return handler
`,
	}

	for name, content := range cases {
		content := content
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision := evaluatePythonPolicy(t, "python.functional_idioms", content)
			if decision.PolicyID != "python.functional_idioms" {
				t.Fatalf("policy mismatch: %#v", decision)
			}
			if len(decision.Diagnostics) != 1 || decision.Diagnostics[0].Code == "" {
				t.Fatalf("expected functional idiom diagnostic code: %#v", decision.Diagnostics)
			}
		})
	}
}

func TestEvaluateCELExpressionCanUsePythonASTFacts(t *testing.T) {
	t.Parallel()

	policyDef := compiledPythonPolicy(t, "python.conditional_imports")
	policyDef.ID = "custom.python_ast_dynamic_import"
	policyDef.Evaluators = []policy.Evaluator{{
		Kind: "cel",
		Name: "cel.expression",
		Options: map[string]any{
			"when": "python_ast.exists(fact, fact.is_dynamic_import && fact.call_name == 'importlib.import_module')",
		},
	}}

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Files:   []string{"src/app.py"},
		Content: "import importlib\nplugin = importlib.import_module('plugin')\n",
		EvaluatorOptions: map[string]any{
			"when": "python_ast.exists(fact, fact.is_dynamic_import && fact.call_name == 'importlib.import_module')",
		},
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
}

func TestEvaluatePythonOptionalReturns(t *testing.T) {
	t.Parallel()

	decision := evaluatePythonPolicy(
		t,
		"python.optional_returns",
		"def dependency() -> Service | None:\n    return None\n",
	)
	if decision.PolicyID != "python.optional_returns" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonCatchAndSilence(t *testing.T) {
	t.Parallel()

	decision := evaluatePythonPolicy(
		t,
		"python.catch_and_silence",
		"try:\n    run()\nexcept Exception:\n    pass\n",
	)
	if decision.PolicyID != "python.catch_and_silence" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonStructuredLogging(t *testing.T) {
	t.Parallel()

	decision := evaluatePythonPolicy(
		t,
		"python.structured_logging",
		"logger.info(f'user={user_id}')\n",
	)
	if decision.PolicyID != "python.structured_logging" {
		t.Fatalf("policy mismatch: %#v", decision)
	}
}

func TestEvaluatePythonDirectImportsAllowsPackageInternalImports(t *testing.T) {
	t.Parallel()

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

func evaluatePythonPolicy(
	t *testing.T,
	policyID string,
	content string,
) policy.Decision {
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
	for _, policyName := range []string{
		"python.functional_idioms",
		"python.optional_returns",
		"python.catch_and_silence",
		"python.structured_logging",
		"python.direct_imports",
	} {
		if _, ok := bundle.Policies[policyName]; !ok {
			base := bundle.Policies["python.conditional_imports"]
			base.ID = policyName
			base.Evaluators = []policy.Evaluator{{Kind: "ast", Name: policyName}}
			bundle.Policies[policyName] = base
		}
	}

	policyDef, ok := bundle.Policies[policyID]
	if !ok {
		t.Fatalf("missing policy %s", policyID)
	}

	return policyDef
}
