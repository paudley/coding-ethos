// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"fmt"
	"slices"

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
	Files []string
	Scope string
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
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
		decision := policy.NewDecision("record", policyDef)
		decision.Severity = "record"
		decision.Evidence = map[string]any{
			"scope": scope,
		}
		if len(options.Files) > 0 {
			decision.Evidence["files"] = append([]string(nil), options.Files...)
		}
		decisions = append(decisions, decision)
	}

	return Result{
		Scope:     scope,
		Files:     append([]string(nil), options.Files...),
		Status:    "resolved",
		Decisions: decisions,
	}, nil
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
