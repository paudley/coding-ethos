// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const conditionalImportsPolicyID = "python.conditional_imports"

func TestEvaluatePythonConditionalImports(t *testing.T) {
	t.Parallel()

	decision := evaluatePythonPolicy(
		t,
		conditionalImportsPolicyID,
		"try:\n    import missing\nexcept ImportError:\n    missing = None\n",
	)
	if decision.PolicyID != conditionalImportsPolicyID {
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision := evaluatePythonPolicy(t, conditionalImportsPolicyID, content)
			if decision.PolicyID != conditionalImportsPolicyID {
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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision := evaluatePythonPolicy(t, conditionalImportsPolicyID, content)
			if decision.PolicyID != conditionalImportsPolicyID {
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

	policyDef := compiledPythonPolicy(t, conditionalImportsPolicyID)

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
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			decision := evaluatePythonPolicy(t, "python.functional_idioms", content)
			if decision.PolicyID != "python.functional_idioms" {
				t.Fatalf("policy mismatch: %#v", decision)
			}

			if len(decision.Diagnostics) != 1 || decision.Diagnostics[0].Code == "" {
				t.Fatalf(
					"expected functional idiom diagnostic code: %#v",
					decision.Diagnostics,
				)
			}
		})
	}
}

func TestEvaluateCELExpressionCanUsePythonASTFacts(t *testing.T) {
	t.Parallel()

	policyDef := compiledPythonPolicy(t, conditionalImportsPolicyID)
	policyDef.ID = "custom.python_ast_dynamic_import"
	policyDef.Evaluators = []policy.Evaluator{{
		Kind: "cel",
		Name: "cel.expression",
		Options: map[string]any{
			"when": importlibImportModuleCEL(),
		},
	}}

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Files:   []string{"src/app.py"},
		Content: "import importlib\nplugin = importlib.import_module('plugin')\n",
		EvaluatorOptions: map[string]any{
			"when": importlibImportModuleCEL(),
		},
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
}

func TestEvaluatePythonSuppressionInWriteMethod(t *testing.T) {
	t.Parallel()

	policyDef := compiledRepoBundle(t).Policies["python.suppression_in_write_method"]
	decision := evaluateCELPythonPolicy(
		t,
		policyDef,
		"src/repository.py",
		"class Repository:\n"+
			"    def write_record(self, value):\n"+
			"        self.backend.write(value)  # type: ignore\n",
	)

	if decision.PolicyID != "python.suppression_in_write_method" {
		t.Fatalf("policy mismatch: %#v", decision)
	}

	if len(decision.Diagnostics) != 1 ||
		decision.Diagnostics[0].Line != 3 ||
		decision.Diagnostics[0].Code != "type: ignore" ||
		decision.Diagnostics[0].Metadata["enclosing_function"] != "write_record" {
		t.Fatalf("unexpected suppression diagnostic: %#v", decision.Diagnostics)
	}
}

func TestEvaluatePythonSuppressionInReadMethodAllowed(t *testing.T) {
	t.Parallel()

	policyDef := compiledRepoBundle(t).Policies["python.suppression_in_write_method"]
	decisions, err := EvaluateCELExpression(policyDef, Context{
		Files: []string{"src/repository.py"},
		Content: "class Repository:\n" +
			"    def read_record(self, value):\n" +
			"        return self.backend.read(value)  # type: ignore\n",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate suppression policy: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("read method suppression should not match: %#v", decisions)
	}
}

func importlibImportModuleCEL() string {
	return "python_ast.exists(fact, fact.is_dynamic_import && " +
		"fact.call_name == 'importlib.import_module')"
}

func evaluateCELPythonPolicy(
	t *testing.T,
	policyDef policy.Policy,
	path string,
	content string,
) policy.Decision {
	t.Helper()

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Files:            []string{path},
		Content:          content,
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate %s: %v", policyDef.ID, err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	return decisions[0]
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

func TestPythonTextEvaluatorsReadOnlyPythonFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pythonFile := filepath.Join(dir, "app.py")
	textFile := filepath.Join(dir, "notes.txt")
	missingFile := filepath.Join(dir, "missing.py")

	inlineErr0 := os.WriteFile(
		pythonFile,
		[]byte("def dependency() -> Optional[Service]:\n    return None\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write python file: %v", inlineErr0)
	}

	inlineErr1 := os.WriteFile(
		textFile,
		[]byte("def ignored() -> Optional[str]:\n"),
		0o600,
	)
	if inlineErr1 != nil {
		t.Fatalf("write text file: %v", inlineErr1)
	}

	policyDef := compiledPythonPolicy(t, "python.optional_returns")

	decisions, err := EvaluatePythonOptionalReturns(policyDef, Context{
		Files: []string{textFile, missingFile, pythonFile},
	})
	if err != nil {
		t.Fatalf("evaluate file-backed optional returns: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	diagnostic := decisions[0].Diagnostics[0]
	if diagnostic.File != pythonFile || diagnostic.Line != 1 {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestPythonSourcesReturnReadErrorsForUnreadablePythonPaths(t *testing.T) {
	t.Parallel()

	dirPath := filepath.Join(t.TempDir(), "package.py")

	inlineErr2 := os.Mkdir(dirPath, 0o700)
	if inlineErr2 != nil {
		t.Fatalf("create directory with .py suffix: %v", inlineErr2)
	}

	policyDef := compiledPythonPolicy(t, "python.optional_returns")

	_, err := EvaluatePythonOptionalReturns(policyDef, Context{
		Files: []string{dirPath},
	})
	if err == nil {
		t.Fatal("expected read error for directory with .py suffix")
	}
}

func TestPythonTextEvaluatorsAllowNonViolatingCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		policyID string
		run      func(policy.Policy, Context) ([]policy.Decision, error)
		content  string
	}{
		{
			name:     "optional exit method",
			policyID: "python.optional_returns",
			run:      EvaluatePythonOptionalReturns,
			content:  "def __exit__(self) -> bool | None:\n    return None\n",
		},
		{
			name:     "non silent exception",
			policyID: "python.catch_and_silence",
			run:      EvaluatePythonCatchAndSilence,
			content:  "try:\n    run()\nexcept Exception:\n    raise\n",
		},
		{
			name:     "structured logging args",
			policyID: "python.structured_logging",
			run:      EvaluatePythonStructuredLogging,
			content:  "logger.info('user logged in', extra={'user_id': user_id})\n",
		},
		{
			name:     "direct imports outside target package",
			policyID: "python.direct_imports",
			run:      EvaluatePythonDirectImports,
			content:  "from other_package.loaders import load\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decisions, err := test.run(compiledPythonPolicy(t, test.policyID), Context{
				Files:   []string{"src/app.py"},
				Content: test.content,
			})
			if err != nil {
				t.Fatalf("evaluate %s: %v", test.policyID, err)
			}

			if len(decisions) != 0 {
				t.Fatalf("expected no decisions, got %#v", decisions)
			}
		})
	}
}

func TestEvaluatePythonDirectImportsBlocksExternalPackageUse(t *testing.T) {
	t.Parallel()

	decision := evaluatePythonPolicy(
		t,
		"python.direct_imports",
		"from coding_ethos.loaders import load\n",
	)
	if decision.PolicyID != "python.direct_imports" {
		t.Fatalf("policy mismatch: %#v", decision)
	}

	if decision.Diagnostics[0].File != "src/app.py" {
		t.Fatalf("diagnostic = %#v", decision.Diagnostics[0])
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
			base := bundle.Policies[conditionalImportsPolicyID]
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
