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
