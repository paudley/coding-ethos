// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateCELExpressionBlocksMatchingCommand(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Command: "python -c 'import subprocess; subprocess.run([\"git\"] )'",
			Argv:    []string{"python", "-c", "import subprocess"},
			Scope:   "files",
			EvaluatorOptions: map[string]any{
				"skill_id": "safe-git-workflow",
				"when":     `command.contains("subprocess") && command.contains("git")`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 1 || decisions[0].PolicyID != "custom.no_subprocess_git" {
		t.Fatalf("decisions = %#v", decisions)
	}
	if decisions[0].Diagnostics[0].SkillID != "safe-git-workflow" {
		t.Fatalf("diagnostic = %#v", decisions[0].Diagnostics[0])
	}
}

func TestEvaluateCELExpressionUsesPolicyDefaultSeverity(t *testing.T) {
	t.Parallel()

	policyDef := celExpressionPolicy()
	policyDef.DefaultSeverity = "record"
	policyDef.SupportedModes = []string{"block", "record"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Command: "python -c 'import subprocess; subprocess.run([\"git\"] )'",
			EvaluatorOptions: map[string]any{
				"when": `command.contains("subprocess") && command.contains("git")`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one", decisions)
	}
	if decisions[0].Decision != "record" || decisions[0].Severity != "record" {
		t.Fatalf("decision = %#v, want record severity", decisions[0])
	}
	if decisions[0].Diagnostics[0].Severity != "record" {
		t.Fatalf("diagnostic = %#v, want record severity", decisions[0].Diagnostics[0])
	}
}

func TestEvaluateCELExpressionIgnoresNonMatchingCommand(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Command: "python -m pytest",
			Scope:   "files",
			EvaluatorOptions: map[string]any{
				"when": `command.contains("subprocess") && command.contains("git")`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateCELExpressionBlocksMatchingPathScope(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Cwd:   "/repo",
			Files: []string{"src/tests/test_policy.py"},
			Scope: "files",
			Tool:  "ruff",
			EvaluatorOptions: map[string]any{
				"source_roots":   []string{"src"},
				"python_version": "3.13",
				"skill_id":       "lint-remediation",
				"when": `
					has_suffix(path.file, ".py") &&
					is_test_path(path.file) &&
					in_source_root(path.file, repo.source_roots) &&
					list_contains(files, path.file) &&
					diagnostic.tool == "ruff" &&
					finding.file == path.file &&
					repo.python_version == "3.13"
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one decision", decisions)
	}
	if decisions[0].Diagnostics[0].File != "src/tests/test_policy.py" {
		t.Fatalf("diagnostic = %#v", decisions[0].Diagnostics[0])
	}
}

func TestEvaluateCELExpressionUsesExplicitPathCollection(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Cwd:   "/repo",
			Files: []string{"src/app.py", "tests/test_policy.py"},
			Scope: "files",
			EvaluatorOptions: map[string]any{
				"source_roots": []string{"src"},
				"when": `
					path.file == "" &&
					paths.exists(item, item.file == "src/app.py" && item.in_source_root) &&
					paths.exists(item, item.file == "tests/test_policy.py" && item.is_test)
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one decision", decisions)
	}
	if decisions[0].Diagnostics[0].File != "" {
		t.Fatalf("diagnostic = %#v, want no implicit first-file location", decisions[0].Diagnostics[0])
	}
}

func celExpressionPolicy() policy.Policy {
	return policy.Policy{
		ID:              "custom.no_subprocess_git",
		Category:        "expression",
		Source:          policy.SourceRef{File: "config.yaml", Path: "policy.expressions"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Git subprocesses are forbidden.",
		Suggestion:      "Use the protected Git wrapper.",
		DefenseLayers:   policy.CodeDefenseLayers(),
		Evaluators: []policy.Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
		}},
		PrincipleIDs: []string{"one-path-for-critical-operations"},
	}
}
