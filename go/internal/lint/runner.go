// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"errors"
	"fmt"
	"slices"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	decisionBlock  = "block"
	decisionRecord = "record"
	statusResolved = "resolved"
)

const (
	ScopeFiles   = "files"
	ScopeChanged = "changed"
	ScopeStaged  = "staged"
	ScopeSmoke   = "smoke"
	ScopeFull    = "full"
	ScopeCutover = "cutover"
)

var (
	errUnknownScopePolicy = errors.New("lint scope references unknown policy")
	errUnsupportedScope   = errors.New("unsupported lint scope")
	errMissingEvaluator   = errors.New("lint policy has no registered evaluator")
)

type Options struct {
	Command string
	Cwd     string
	Scope   string
	Files   []string
	Argv    []string
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
	return RunWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func RunWithRegistry(
	bundle policy.Bundle,
	options Options,
	registry evaluators.Registry,
) (Result, error) {
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
			return Result{}, fmt.Errorf(
				"%w: %q references %q",
				errUnknownScopePolicy,
				scope,
				policyID,
			)
		}

		evaluated, err := evaluatePolicy(policyDef, scope, options, registry)
		if err != nil {
			return Result{}, fmt.Errorf("evaluate policy %q: %w", policyID, err)
		}

		decisions = append(decisions, evaluated...)
	}

	decisions = enrichDecisionDiagnostics(decisions, bundle.EvidenceMaps)

	return Result{
		Scope:       scope,
		Files:       append([]string(nil), options.Files...),
		Status:      resultStatus(decisions),
		Decisions:   decisions,
		Diagnostics: diagnosticsFromDecisions(decisions),
	}, nil
}

func enrichDecisionDiagnostics(
	decisions []policy.Decision,
	evidenceMaps []diagnostics.EvidenceMap,
) []policy.Decision {
	if len(decisions) == 0 || len(evidenceMaps) == 0 {
		return decisions
	}

	enriched := make([]policy.Decision, 0, len(decisions))
	for _, decision := range decisions {
		decision.Diagnostics = diagnostics.Enrich(decision.Diagnostics, evidenceMaps)
		enriched = append(enriched, decision)
	}

	return enriched
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
		Cwd:     options.Cwd,
	}

	var registered bool

	for _, evaluatorSpec := range policyDef.Evaluators {
		evaluator, ok := registry.Lookup(evaluatorSpec.Name)
		if !ok {
			continue
		}

		context.EvaluatorOptions = evaluatorSpec.Options

		registered = true

		decisions, err := evaluator.Evaluate(policyDef, context)
		if err != nil {
			return nil, fmt.Errorf("evaluate policy %q: %w", policyDef.ID, err)
		}

		if len(decisions) > 0 {
			return decisions, nil
		}
	}

	if len(policyDef.Evaluators) > 0 && !registered {
		return nil, fmt.Errorf("%w: %q", errMissingEvaluator, policyDef.ID)
	}

	return []policy.Decision{recordDecision(policyDef, scope, options)}, nil
}

func recordDecision(
	policyDef policy.Policy,
	scope string,
	options Options,
) policy.Decision {
	decision := policy.NewDecision(decisionRecord, policyDef)
	decision.Severity = decisionRecord

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
		if decision.Decision == decisionBlock || decision.Severity == decisionBlock {
			return "blocked"
		}
	}

	return statusResolved
}

func diagnosticsFromDecisions(decisions []policy.Decision) []diagnostics.Diagnostic {
	diagnosticItems := []diagnostics.Diagnostic{}
	for _, decision := range decisions {
		diagnosticItems = append(diagnosticItems, decision.Diagnostics...)
	}

	return diagnosticItems
}

func policyIDsForScope(bundle policy.Bundle, scope string) ([]string, error) {
	if scope == ScopeChanged {
		scope = ScopeFiles
	}

	allowedScopes := []string{
		ScopeFiles,
		ScopeStaged,
		ScopeSmoke,
		ScopeFull,
		ScopeCutover,
	}
	if !slices.Contains(allowedScopes, scope) {
		return nil, fmt.Errorf("unsupported lint scope %q: %w", scope, errUnsupportedScope)
	}

	policyIDs, ok := bundle.Dispatch.Linter[scope]
	if !ok {
		return nil, nil
	}

	return append([]string(nil), policyIDs...), nil
}
