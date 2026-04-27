// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	modeAdvise       = "advise"
	modeAnnotate     = "annotate"
	modeBlock        = "block"
	modeRecord       = "record"
	statusAllowed    = "allowed"
	statusBlocked    = "blocked"
	hookDividerWidth = 50
)

var (
	errUnknownHookPolicy     = errors.New("hook dispatch references unknown policy")
	errUnregisteredEvaluator = errors.New("policy references unregistered evaluator")
	errInvalidPathPattern    = errors.New("invalid path pattern")
)

type Options struct {
	Event Event
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
	return RunWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func RunWithRegistry(
	bundle policy.Bundle,
	options Options,
	registry evaluators.Registry,
) (Result, error) {
	event := options.Event
	entries := bundle.Dispatch.Hooks[event.HookEventName][event.ToolName]
	decisions := make([]policy.Decision, 0, len(entries))

	for _, entry := range entries {
		policyDef, ok := bundle.Policies[entry.PolicyID]
		if !ok {
			return Result{}, fmt.Errorf(
				"%w: %q",
				errUnknownHookPolicy,
				entry.PolicyID,
			)
		}

		evaluated, err := evaluateHookPolicy(policyDef, entry, event, registry)
		if err != nil {
			return Result{}, err
		}

		decisions = append(decisions, evaluated...)
		if resultStatus(decisions) == statusBlocked {
			break
		}
	}

	return Result{
		Event:              event.HookEventName,
		Tool:               event.ToolName,
		Status:             resultStatus(decisions),
		Decisions:          decisions,
		HookSpecificOutput: hookSpecificOutput(event),
	}, nil
}

func hookSpecificOutput(event Event) *HookSpecificOutput {
	if event.HookEventName != "PostToolUse" || event.ToolName != "Bash" {
		return nil
	}

	command := event.Command()
	output := event.ToolOutput()

	if !isGitHookCommand(command) || !hasHookOutputKeywords(output) {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: buildHookOutputContext(command, output, event.ReturnCode()),
	}
}

func isGitHookCommand(command string) bool {
	lower := strings.ToLower(command)

	return strings.Contains(lower, "git commit") ||
		strings.Contains(lower, "git push") ||
		strings.Contains(lower, "pre-commit")
}

func hasHookOutputKeywords(output string) bool {
	lower := strings.ToLower(output)
	for _, keyword := range []string{
		"passed",
		"failed",
		"skipped",
		"pre-commit",
		"pre-push",
		"hook",
		"ruff",
		"mypy",
		"pyright",
		"pylint",
	} {
		if strings.Contains(lower, keyword) {
			return true
		}
	}

	return false
}

func buildHookOutputContext(command string, output string, returnCode int) string {
	hookType := "PRE-COMMIT"
	operation := "commit"

	if strings.Contains(strings.ToLower(command), "git push") {
		hookType = "PRE-PUSH"
		operation = "push"
	}

	outcome := "The " + operation + " succeeded."
	if returnCode != 0 {
		outcome = "The " + operation + " was blocked by hooks."
	}

	return hookType + " OUTPUT\n" +
		strings.Repeat("=", hookDividerWidth) + "\n\n" +
		outcome + "\n\n" +
		"<hook-output>\n" + output + "\n</hook-output>\n\n" +
		"Summarize failed hooks, modified files, warnings, and required fixes. " +
		"Treat linter output as important and fix findings structurally."
}

func evaluateHookPolicy(
	policyDef policy.Policy,
	entry policy.HookDispatchEntry,
	event Event,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	if !matchesCommandPatterns(event.Command(), entry.CommandPatterns) {
		return nil, nil
	}

	if !matchesPathPatterns(event.Files(), entry.PathPatterns) {
		return nil, nil
	}

	context := evaluators.Context{
		Scope:   event.HookEventName,
		Argv:    commandArgv(event.Command()),
		Command: event.Command(),
		Content: event.Content(),
		Cwd:     event.Cwd,
		Files:   event.Files(),
	}

	for _, evaluatorSpec := range policyDef.Evaluators {
		evaluator, ok := registry.Lookup(evaluatorSpec.Name)
		if !ok {
			return nil, fmt.Errorf(
				"%w: policy %q evaluator %q",
				errUnregisteredEvaluator,
				policyDef.ID,
				evaluatorSpec.Name,
			)
		}

		decisions, err := evaluator.Evaluate(policyDef, context)
		if err != nil {
			return nil, fmt.Errorf("evaluate policy %q: %w", policyDef.ID, err)
		}

		if len(decisions) > 0 {
			return applyDispatchMode(decisions, entry.Mode), nil
		}
	}

	if entry.Mode == modeAdvise || entry.Mode == modeRecord || entry.Mode == modeAnnotate {
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

func applyDispatchMode(decisions []policy.Decision, mode string) []policy.Decision {
	if mode == "" || mode == modeBlock {
		return decisions
	}

	applied := make([]policy.Decision, 0, len(decisions))
	for _, decision := range decisions {
		decision.Decision = mode
		decision.Severity = mode
		applied = append(applied, decision)
	}

	return applied
}

func commandArgv(command string) []string {
	if command == "" {
		return nil
	}

	return shellFields(command)
}

func matchesCommandPatterns(command string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}

	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(command, pattern) {
			return true
		}
	}

	return false
}

func matchesPathPatterns(files []string, patterns []string) bool {
	if len(patterns) == 0 {
		return true
	}

	if len(files) == 0 {
		return false
	}

	for _, file := range files {
		for _, pattern := range patterns {
			if matchesPathPattern(file, pattern) {
				return true
			}
		}
	}

	return false
}

func matchesPathPattern(file string, pattern string) bool {
	normalizedFile := strings.TrimPrefix(filepath.ToSlash(file), "./")

	normalizedPattern := strings.TrimPrefix(filepath.ToSlash(pattern), "./")
	if normalizedPattern == "" {
		return false
	}

	if normalizedPattern == normalizedFile {
		return true
	}

	if after, ok := strings.CutPrefix(normalizedPattern, "**/"); ok {
		suffix := after

		matched, err := pathMatches(suffix, filepath.Base(normalizedFile))
		if err != nil {
			return false
		}

		if matched {
			return true
		}

		return strings.HasSuffix(normalizedFile, "/"+suffix)
	}

	matched, err := pathMatches(normalizedPattern, normalizedFile)
	if err != nil {
		return false
	}

	if matched {
		return true
	}

	if !strings.Contains(normalizedPattern, "/") {
		matched, err := pathMatches(normalizedPattern, filepath.Base(normalizedFile))
		if err != nil {
			return false
		}

		return matched
	}

	return false
}

func pathMatches(pattern string, name string) (bool, error) {
	matched, err := filepath.Match(pattern, name)
	if err != nil {
		return false, fmt.Errorf("%w: %q", errInvalidPathPattern, pattern)
	}

	return matched, nil
}

func resultStatus(decisions []policy.Decision) string {
	for _, decision := range decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			return statusBlocked
		}
	}

	return statusAllowed
}
