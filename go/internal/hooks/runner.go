// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
	route := routeToolUse(event)
	decisions, err := evaluateDispatchedPolicies(bundle, event, registry)
	if err != nil {
		return Result{}, err
	}

	decisions = appendRouteBlockDecision(bundle, route, decisions)
	status := resultStatus(decisions)
	if status == statusBlocked {
		route = gitWrapperRoute{}
	}

	return buildResult(bundle, event, route, decisions, status), nil
}

func routeToolUse(event Event) gitWrapperRoute {
	for _, routeFor := range []func(Event) gitWrapperRoute{
		gitWrapperRouteFor,
		lintToolRouteFor,
		pythonRuntimeRouteFor,
	} {
		route := routeFor(event)
		if route.Block || route.Rewrite {
			return route
		}
	}

	return gitWrapperRoute{}
}

func evaluateDispatchedPolicies(
	bundle policy.Bundle,
	event Event,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	entries := bundle.Dispatch.Hooks[event.HookEventName][event.ToolName]
	decisions := make([]policy.Decision, 0, len(entries))
	for _, entry := range entries {
		policyDef, ok := bundle.Policies[entry.PolicyID]
		if !ok {
			return nil, fmt.Errorf(
				"%w: %q",
				errUnknownHookPolicy,
				entry.PolicyID,
			)
		}

		evaluated, err := evaluateHookPolicy(policyDef, entry, event, registry)
		if err != nil {
			return nil, err
		}

		decisions = append(decisions, evaluated...)
		if resultStatus(decisions) == statusBlocked {
			break
		}
	}

	return decisions, nil
}

func appendRouteBlockDecision(
	bundle policy.Bundle,
	route gitWrapperRoute,
	decisions []policy.Decision,
) []policy.Decision {
	if route.Block && resultStatus(decisions) != statusBlocked {
		return append(
			append([]policy.Decision(nil), decisions...),
			routeBlockDecision(bundle, route.BlockPolicyID, route.Reason),
		)
	}

	return decisions
}

func buildResult(
	bundle policy.Bundle,
	event Event,
	route gitWrapperRoute,
	decisions []policy.Decision,
	status string,
) Result {
	return Result{
		Event:              event.HookEventName,
		Advice:             bundle.Advice,
		Provider:           event.Provider(),
		Tool:               event.ToolName,
		Status:             status,
		Decisions:          decisions,
		HookSpecificOutput: hookSpecificOutput(bundle, event, route),
	}
}

func hookSpecificOutput(
	bundle policy.Bundle,
	event Event,
	route gitWrapperRoute,
) *HookSpecificOutput {
	if route.Rewrite {
		if event.Provider() != "claude" {
			return nil
		}

		return &HookSpecificOutput{
			HookEventName:            event.HookEventName,
			PermissionDecision:       permissionAllow,
			PermissionDecisionReason: route.Reason,
			UpdatedInput:             route.UpdatedInput,
		}
	}

	if output := continuationOutput(event); output != nil {
		return output
	}

	if output := lifecycleOutput(event); output != nil {
		return output
	}

	if output := postEditOutput(bundle, event); output != nil {
		return output
	}

	if event.HookEventName != "PostToolUse" || event.ToolName != "Bash" {
		return nil
	}

	command := event.Command()
	output := event.ToolOutput()

	if !isGitHookCommand(command) || !hasHookOutputKeywords(output) {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName: event.HookEventName,
		AdditionalContext: buildHookOutputContext(
			command,
			output,
			event.ReturnCode(),
			selectedOutputFormat(),
			event.Cwd,
		),
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

func buildHookOutputContext(
	command string,
	output string,
	returnCode int,
	format string,
	cwd string,
) string {
	operation := hookOperation(command)
	status := hookOutputStatus(returnCode)
	normalizer := hookOutputNormalizer(cwd)
	command = normalizer.compact(command)
	output = normalizer.preserveLines(output)

	switch format {
	case outputFormatJSON:
		return buildHookOutputContextJSON(operation, status, command, output, returnCode)
	case outputFormatTOON:
		return buildHookOutputContextTOON(operation, status, command, output, returnCode)
	default:
		return buildHookOutputContextHuman(operation, status, output)
	}
}

func hookOperation(command string) string {
	operation := "commit"

	if strings.Contains(strings.ToLower(command), "git push") {
		operation = "push"
	}

	if strings.Contains(strings.ToLower(command), "pre-commit") {
		operation = "pre-commit"
	}

	return operation
}

func hookOutputStatus(returnCode int) string {
	if returnCode == 0 {
		return statusAllowed
	}

	return statusBlocked
}

func buildHookOutputContextHuman(
	operation string,
	status string,
	output string,
) string {
	hookType := "PRE-COMMIT"
	if operation == "push" {
		hookType = "PRE-PUSH"
	}

	outcome := "The " + operation + " succeeded."
	if status == statusBlocked {
		outcome = "The " + operation + " was blocked by hooks."
	}

	return hookType + " OUTPUT\n" +
		strings.Repeat("=", hookDividerWidth) + "\n\n" +
		outcome + "\n\n" +
		"<hook-output>\n" + output + "\n</hook-output>\n\n" +
		"Summarize failed hooks, modified files, warnings, and required fixes. " +
		"Treat linter output as important and fix findings structurally."
}

func buildHookOutputContextTOON(
	operation string,
	status string,
	command string,
	output string,
	returnCode int,
) string {
	lines := []string{
		"format: toon",
		"event: PostToolUse",
		"tool: Bash",
		"operation: " + toonCell(operation),
		"status: " + toonCell(status),
		fmt.Sprintf("return_code: %d", returnCode),
		"command: " + toonCell(command),
		"summary: " + toonCell(hookOutputSummary(operation, status)),
		"guidance: " + toonCell(
			"Summarize failed hooks, modified files, warnings, and required fixes. "+
				"Treat linter output as important and fix findings structurally.",
		),
	}
	outputLines := compactHookOutputLines(output)
	if len(outputLines) > 0 {
		lines = append(lines, fmt.Sprintf("hook_output[%d]{line}:", len(outputLines)))
		for _, line := range outputLines {
			lines = append(lines, "  "+toonCell(line))
		}
	}

	return strings.Join(lines, "\n")
}

func buildHookOutputContextJSON(
	operation string,
	status string,
	command string,
	output string,
	returnCode int,
) string {
	payload := map[string]any{
		"format":      outputFormatJSON,
		"event":       "PostToolUse",
		"tool":        "Bash",
		"operation":   operation,
		"status":      status,
		"return_code": returnCode,
		"command":     command,
		"summary":     hookOutputSummary(operation, status),
		"hook_output": output,
		"guidance": sentence(
			"Summarize failed hooks, modified files, warnings, and required fixes.",
			"Treat linter output as important and fix findings structurally.",
		),
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return buildHookOutputContextTOON(operation, status, command, output, returnCode)
	}

	return string(encoded)
}

func hookOutputSummary(operation string, status string) string {
	if status == statusBlocked {
		return operation + " was blocked by hooks"
	}

	return operation + " hooks completed successfully"
}

type hookTextNormalizer struct {
	replacements []hookTextReplacement
}

type hookTextReplacement struct {
	Old string
	New string
}

func hookOutputNormalizer(cwd string) hookTextNormalizer {
	roots := []hookTextReplacement{{Old: cwd, New: "<repo>"}}
	if cwd == "" {
		if current, err := os.Getwd(); err == nil {
			roots = append(roots, hookTextReplacement{Old: current, New: "<repo>"})
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		roots = append(roots, hookTextReplacement{Old: home, New: "<home>"})
	}
	roots = append(roots, hookTextReplacement{Old: os.TempDir(), New: "<tmp>"})

	replacements := []hookTextReplacement{}
	for _, root := range roots {
		cleaned := strings.TrimRight(filepath.Clean(root.Old), string(filepath.Separator))
		if cleaned == "." || cleaned == "" {
			continue
		}
		replacements = append(replacements, hookTextReplacement{
			Old: filepath.ToSlash(cleaned),
			New: root.New,
		})
	}
	slices.SortFunc(replacements, func(left hookTextReplacement, right hookTextReplacement) int {
		return cmp.Compare(len(right.Old), len(left.Old))
	})

	return hookTextNormalizer{replacements: replacements}
}

func (normalizer hookTextNormalizer) compact(value string) string {
	return strings.Join(strings.Fields(normalizer.preserveLines(value)), " ")
}

func (normalizer hookTextNormalizer) preserveLines(value string) string {
	normalized := filepath.ToSlash(value)
	for _, replacement := range normalizer.replacements {
		normalized = strings.ReplaceAll(normalized, replacement.Old, replacement.New)
	}

	return normalized
}

func compactHookOutputLines(output string) []string {
	lines := []string{}
	for line := range strings.SplitSeq(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lines = append(lines, trimmed)
	}

	return lines
}

func sentence(parts ...string) string {
	return strings.Join(parts, " ")
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
		Tool:    event.ToolName,
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

		context.EvaluatorOptions = evaluatorSpec.Options

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
