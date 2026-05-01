// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"github.com/google/cel-go/cel"
)

func EvaluateCELExpression(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	source := strings.TrimSpace(stringOption(context.EvaluatorOptions, "when", ""))
	if source == "" {
		return nil, fmt.Errorf("CEL expression policy %q missing when", policyDef.ID)
	}

	env, err := celexpr.Environment()
	if err != nil {
		return nil, fmt.Errorf("prepare CEL environment: %w", err)
	}

	ast, issues := env.Compile(source)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile CEL expression: %w", issues.Err())
	}
	if !ast.OutputType().IsExactType(cel.BoolType) {
		return nil, fmt.Errorf("CEL expression must return bool, got %s", ast.OutputType())
	}

	program, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("prepare CEL program: %w", err)
	}

	output, _, err := program.Eval(celActivation(context))
	if err != nil {
		return nil, fmt.Errorf("evaluate CEL expression: %w", err)
	}

	matched, ok := output.Value().(bool)
	if !ok || !matched {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"argv":    append([]string(nil), context.Argv...),
		"command": context.Command,
		"files":   append([]string(nil), context.Files...),
		"scope":   context.Scope,
		"when":    source,
	}
	if skillID := stringOption(context.EvaluatorOptions, "skill_id", ""); skillID != "" {
		decision.Evidence["skill_id"] = skillID
	}
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:         "policy",
		Severity:     blockDecision,
		PolicyID:     policyDef.ID,
		SkillID:      stringOption(context.EvaluatorOptions, "skill_id", ""),
		Message:      policyDef.Message,
		Advice:       policyDef.Suggestion,
		PrincipleIDs: append([]string(nil), policyDef.PrincipleIDs...),
	}}
	if len(context.Files) == 1 {
		decision.Diagnostics[0].File = context.Files[0]
	}

	return []policy.Decision{decision}, nil
}

func celActivation(context Context) map[string]any {
	return celexpr.Activation(celexpr.ActivationInput{
		Argv:          context.Argv,
		Command:       context.Command,
		Cwd:           context.Cwd,
		Files:         context.Files,
		Scope:         context.Scope,
		Tool:          context.Tool,
		AdminApproved: context.AdminApproved,
		SourceRoots:   stringSliceOption(context.EvaluatorOptions, "source_roots", nil),
		PythonVersion: stringOption(context.EvaluatorOptions, "python_version", ""),
	})
}
