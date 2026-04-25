// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type Options struct {
	Event Event
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
	return RunWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func RunWithRegistry(bundle policy.Bundle, options Options, registry evaluators.Registry) (Result, error) {
	event := options.Event
	entries := bundle.Dispatch.Hooks[event.HookEventName][event.ToolName]
	decisions := make([]policy.Decision, 0, len(entries))

	for _, entry := range entries {
		policyDef, ok := bundle.Policies[entry.PolicyID]
		if !ok {
			return Result{}, fmt.Errorf("hook dispatch references unknown policy %q", entry.PolicyID)
		}
		evaluated, err := evaluateHookPolicy(policyDef, entry, event, registry)
		if err != nil {
			return Result{}, err
		}
		decisions = append(decisions, evaluated...)
	}

	return Result{
		Event:     event.HookEventName,
		Tool:      event.ToolName,
		Status:    resultStatus(decisions),
		Decisions: decisions,
	}, nil
}

func evaluateHookPolicy(
	policyDef policy.Policy,
	entry policy.HookDispatchEntry,
	event Event,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	context := evaluators.Context{
		Scope: event.HookEventName,
		Argv:  commandArgv(event.Command()),
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
	if entry.Mode == "advise" || entry.Mode == "record" || entry.Mode == "annotate" {
		decision := policy.NewDecision(entry.Mode, policyDef)
		decision.Severity = entry.Mode
		decision.Evidence = map[string]any{
			"event": event.HookEventName,
			"tool":  event.ToolName,
		}
		if command := event.Command(); command != "" {
			decision.Evidence["command"] = command
		}
		return []policy.Decision{decision}, nil
	}
	return nil, nil
}

func commandArgv(command string) []string {
	if command == "" {
		return nil
	}
	return strings.Fields(command)
}

func resultStatus(decisions []policy.Decision) string {
	for _, decision := range decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			return "blocked"
		}
	}
	return "allowed"
}
