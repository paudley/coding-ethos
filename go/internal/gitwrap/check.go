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
	parsed := parseArgv(options.Argv)
	argv := parsed.Argv
	operation := parsed.Operation
	if operation == "" {
		return Result{Argv: argv, Status: "allowed"}, nil
	}

	policyIDs := gitPrePolicyIDs(bundle, operation)
	if len(policyIDs) == 0 {
		return Result{Argv: argv, Operation: operation, Status: "allowed"}, nil
	}

	decisions := make([]policy.Decision, 0, len(policyIDs))
	for _, policyID := range policyIDs {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return Result{}, fmt.Errorf("git dispatch %q references unknown policy %q", operation, policyID)
		}
		evaluated, err := evaluateGitPolicy(policyDef, argv, options.Cwd, "", registry)
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

func gitPrePolicyIDs(bundle policy.Bundle, operation string) []string {
	return appendGitPolicyIDs(nil, bundle, operation, func(dispatch policy.GitOperationDispatch) []string {
		return dispatch.Pre
	})
}

func gitPostPolicyIDs(bundle policy.Bundle, operation string) []string {
	return appendGitPolicyIDs(nil, bundle, operation, func(dispatch policy.GitOperationDispatch) []string {
		return dispatch.Post
	})
}

func appendGitPolicyIDs(
	policyIDs []string,
	bundle policy.Bundle,
	operation string,
	selectIDs func(policy.GitOperationDispatch) []string,
) []string {
	if dispatch, ok := bundle.Dispatch.Git["*"]; ok {
		policyIDs = append(policyIDs, selectIDs(dispatch)...)
	}
	if dispatch, ok := bundle.Dispatch.Git[operation]; ok {
		policyIDs = append(policyIDs, selectIDs(dispatch)...)
	}
	return dedupePolicyIDs(policyIDs)
}

func dedupePolicyIDs(policyIDs []string) []string {
	seen := map[string]bool{}
	deduped := make([]string, 0, len(policyIDs))
	for _, policyID := range policyIDs {
		if policyID == "" || seen[policyID] {
			continue
		}
		seen[policyID] = true
		deduped = append(deduped, policyID)
	}
	return deduped
}

func evaluateGitPolicy(
	policyDef policy.Policy,
	argv []string,
	cwd string,
	scope string,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	context := evaluators.Context{Argv: append([]string(nil), argv...), Cwd: cwd, Scope: scope}
	for _, evaluatorSpec := range policyDef.Evaluators {
		evaluator, ok := registry.Lookup(evaluatorSpec.Name)
		if !ok {
			return nil, fmt.Errorf("policy %q references unregistered evaluator %q", policyDef.ID, evaluatorSpec.Name)
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

func resultStatus(decisions []policy.Decision) string {
	for _, decision := range decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			return "blocked"
		}
	}
	return "allowed"
}
