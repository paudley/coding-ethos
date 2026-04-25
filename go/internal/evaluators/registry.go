// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
)

type Registry struct {
	evaluators map[string]Evaluator
}

func NewRegistry() Registry {
	return Registry{evaluators: map[string]Evaluator{}}
}

func DefaultRegistry() Registry {
	registry := NewRegistry()
	registry.Register("git.hook_bypass", EvaluatorFunc(EvaluateGitHookBypass))
	return registry
}

func (registry Registry) Register(name string, evaluator Evaluator) {
	registry.evaluators[name] = evaluator
}

func (registry Registry) Lookup(name string) (Evaluator, bool) {
	evaluator, ok := registry.evaluators[name]
	return evaluator, ok
}

func (registry Registry) Require(name string) (Evaluator, error) {
	evaluator, ok := registry.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("no evaluator registered for %q", name)
	}
	return evaluator, nil
}
