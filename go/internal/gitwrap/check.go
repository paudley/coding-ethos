// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type Options struct {
	Argv []string
	Cwd  string
}

func Check(bundle policy.Bundle, options Options) (Result, error) {
	return CheckWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func CheckWithRegistry(bundle policy.Bundle, options Options, registry evaluators.Registry) (Result, error) {
	argv := normalizeArgv(options.Argv)
	operation := gitOperation(argv)
	if operation == "" {
		return Result{Argv: argv, Status: "allowed"}, nil
	}

	dispatch, ok := bundle.Dispatch.Git[operation]
	if !ok {
		return Result{Argv: argv, Operation: operation, Status: "allowed"}, nil
	}

	decisions := make([]policy.Decision, 0, len(dispatch.Pre))
	for _, policyID := range dispatch.Pre {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return Result{}, fmt.Errorf("git dispatch %q references unknown policy %q", operation, policyID)
		}
		evaluated, err := evaluateGitPolicy(policyDef, argv, options.Cwd, registry)
		if err != nil {
			return Result{}, err
		}
		decisions = append(decisions, evaluated...)
	}

	return Result{
		Argv:      argv,
		Operation: operation,
		Status:    resultStatus(decisions),
		Decisions: decisions,
	}, nil
}

func evaluateGitPolicy(
	policyDef policy.Policy,
	argv []string,
	cwd string,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	context := evaluators.Context{Argv: append([]string(nil), argv...), Cwd: cwd}
	for _, evaluatorSpec := range policyDef.Evaluators {
		evaluator, ok := registry.Lookup(evaluatorSpec.Name)
		if !ok {
			continue
		}
		decisions, err := evaluator.Evaluate(policyDef, context)
		if err != nil {
			return nil, err
		}
		if len(decisions) > 0 {
			return decisions, nil
		}
	}
	return nil, nil
}

func normalizeArgv(argv []string) []string {
	normalized := append([]string(nil), argv...)
	if len(normalized) == 0 {
		return []string{"git"}
	}
	if normalized[0] == "git" {
		return normalized
	}
	return append([]string{"git"}, normalized...)
}

func gitOperation(argv []string) string {
	if len(argv) < 2 || argv[0] != "git" {
		return ""
	}
	return argv[1]
}

func resultStatus(decisions []policy.Decision) string {
	for _, decision := range decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			return "blocked"
		}
	}
	return "allowed"
}
