// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"fmt"
	"slices"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	ScopeFiles   = "files"
	ScopeChanged = "changed"
	ScopeStaged  = "staged"
	ScopeSmoke   = "smoke"
	ScopeFull    = "full"
)

type Options struct {
	Files   []string
	Argv    []string
	Command string
	Scope   string
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
	return RunWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func RunWithRegistry(bundle policy.Bundle, options Options, registry evaluators.Registry) (Result, error) {
	scope := options.Scope
	if scope == "" {
		scope = ScopeFiles
	}
	policyIDs, err := policyIDsForScope(bundle, scope)
	if err != nil {
		return Result{}, err
	}

	decisions := make([]policy.Decision, 0, len(policyIDs))
	for _, policyID := range policyIDs {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return Result{}, fmt.Errorf("scope %q references unknown policy %q", scope, policyID)
		}
		evaluated, err := evaluatePolicy(policyDef, scope, options, registry)
		if err != nil {
			return Result{}, err
		}
		decisions = append(decisions, evaluated...)
	}

	return Result{
		Scope:     scope,
		Files:     append([]string(nil), options.Files...),
		Status:    resultStatus(decisions),
		Decisions: decisions,
	}, nil
}

func evaluatePolicy(
	policyDef policy.Policy,
	scope string,
	options Options,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	context := evaluators.Context{
		Scope:   scope,
		Files:   append([]string(nil), options.Files...),
		Argv:    append([]string(nil), options.Argv...),
		Command: options.Command,
	}
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
	return []policy.Decision{recordDecision(policyDef, scope, options)}, nil
}

func recordDecision(policyDef policy.Policy, scope string, options Options) policy.Decision {
	decision := policy.NewDecision("record", policyDef)
	decision.Severity = "record"
	decision.Evidence = map[string]any{
		"scope": scope,
	}
	if len(options.Files) > 0 {
		decision.Evidence["files"] = append([]string(nil), options.Files...)
	}
	if len(options.Argv) > 0 {
		decision.Evidence["argv"] = append([]string(nil), options.Argv...)
	}
	if options.Command != "" {
		decision.Evidence["command"] = options.Command
	}
	return decision
}

func resultStatus(decisions []policy.Decision) string {
	for _, decision := range decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			return "blocked"
		}
	}
	return "resolved"
}

func policyIDsForScope(bundle policy.Bundle, scope string) ([]string, error) {
	if scope == ScopeChanged {
		scope = ScopeFiles
	}
	allowedScopes := []string{ScopeFiles, ScopeStaged, ScopeSmoke, ScopeFull}
	if !slices.Contains(allowedScopes, scope) {
		return nil, fmt.Errorf("unsupported lint scope %q", scope)
	}
	policyIDs, ok := bundle.Dispatch.Linter[scope]
	if !ok {
		return nil, nil
	}
	return append([]string(nil), policyIDs...), nil
}
