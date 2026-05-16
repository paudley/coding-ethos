// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
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
	errUnknownHookPolicy = apperror.StaticError(
		"hook dispatch references unknown policy",
	)
	errUnregisteredEvaluator = apperror.StaticError(
		"policy references unregistered evaluator",
	)
	errInvalidPathPattern = apperror.StaticError("invalid path pattern")
)

type Options struct {
	AdminApproved func(string) bool
	Event         Event
}

func Run(bundle policy.Bundle, options Options) (Result, error) {
	return RunWithRegistry(bundle, options, evaluators.DefaultRegistry())
}

func RunWithRegistry(
	bundle policy.Bundle,
	options Options,
	registry evaluators.Registry,
) (Result, error) {
	ctx := collectInspectionContext(options.Event, options.AdminApproved)
	if ctx.SkipNestedHook || ctx.ReadOnlyInspection {
		return ctx.allowedResult(), nil
	}

	decision, err := evaluateInspection(bundle, ctx, registry)
	if err != nil {
		return Result{}, err
	}

	return buildResult(bundle, ctx.Event, decision), nil
}

func evaluateInspection(
	bundle policy.Bundle,
	ctx InspectionContext,
	registry evaluators.Registry,
) (InspectionDecision, error) {
	route := routeToolUse(ctx)

	decisions, err := evaluateDispatchedPolicies(bundle, ctx, registry)
	if err != nil {
		return InspectionDecision{}, err
	}

	return decideInspection(bundle, ctx, decisions, route), nil
}

func routeToolUse(ctx InspectionContext) InspectionRoute {
	for _, routeFor := range []func(Event) InspectionRoute{
		parallelToolBatchRouteFor,
		malformedShellRouteFor,
		gitWrapperRouteFor,
		lintToolRouteFor,
		pythonRuntimeRouteFor,
	} {
		route := routeFor(ctx.Event)
		if route.Block || route.Rewrite {
			return route
		}
	}

	return InspectionRoute{}
}

func evaluateDispatchedPolicies(
	bundle policy.Bundle,
	ctx InspectionContext,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	event := ctx.Event
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

		evaluated, err := evaluateHookPolicy(policyDef, entry, ctx, registry)
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

func buildResult(
	bundle policy.Bundle,
	event Event,
	decision InspectionDecision,
) Result {
	hookOutput, proxyEvents := hookSpecificOutput(bundle, event, decision.Route)

	result := Result{
		Event:              event.HookEventName,
		Advice:             bundle.Advice,
		Provider:           event.Provider(),
		Tool:               event.ToolName,
		Status:             decision.Status,
		Decisions:          decision.Policies,
		HookSpecificOutput: hookOutput,
		ProxyEvents:        proxyEvents,
	}
	if result.Blocked() {
		result.TrackingID = newDenialTrackingID(event, decision.Policies)
	}

	if result.HookSpecificOutput == nil {
		result.HookSpecificOutput = blockedHookSpecificOutput(result)
	}

	return result
}

func blockedHookSpecificOutput(result Result) *HookSpecificOutput {
	if !result.Blocked() || result.Event != eventPreToolUse || result.Provider != "" {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName:            result.Event,
		PermissionDecision:       "deny",
		PermissionDecisionReason: ProviderBlockMessage(result),
	}
}

func hookSpecificOutput(
	bundle policy.Bundle,
	event Event,
	route InspectionRoute,
) (*HookSpecificOutput, []agentproxy.ProviderEvent) {
	if route.Rewrite {
		return &HookSpecificOutput{
			HookEventName:            event.HookEventName,
			PermissionDecision:       permissionAllow,
			PermissionDecisionReason: route.Reason,
			UpdatedInput:             route.UpdatedInput,
		}, nil
	}

	if output := continuationOutput(event); output != nil {
		return output, nil
	}

	if output := lifecycleOutput(event); output != nil {
		return output, nil
	}

	if output := postEditOutput(bundle, event); output != nil {
		return output, nil
	}

	if event.HookEventName != eventPostToolUse || event.ToolName != toolBash {
		return nil, nil
	}

	command := event.Command()
	output := event.ToolOutput()
	proxiedOutput := proxyPostToolOutput(event, output)

	if !shouldEmitPostToolBashContext(event, command, output, proxiedOutput) {
		return nil, proxiedOutput.Events
	}

	return &HookSpecificOutput{
		HookEventName: event.HookEventName,
		AdditionalContext: buildHookOutputContext(
			command,
			proxiedOutput.Text,
			event.ReturnCode(),
			selectedOutputFormat(),
			event.Cwd,
			postToolReminder(bundle, event),
		),
	}, proxiedOutput.Events
}

func shouldEmitPostToolBashContext(
	event Event,
	command string,
	output string,
	proxiedOutput proxiedToolOutput,
) bool {
	if hasDirectoryAnatomyTransform(proxiedOutput.Records) {
		return true
	}

	if hasFileReadTransformChange(proxiedOutput.Records) {
		return true
	}

	if isLintCommand(command) {
		return true
	}

	if event.ReturnCode() != 0 && inferDiagnosticTool(command) != "" {
		return true
	}

	return event.ReturnCode() != 0 &&
		isGitHookCommand(command) &&
		hasHookOutputKeywords(output)
}

func hasDirectoryAnatomyTransform(records []agentproxy.TransformRecord) bool {
	for _, record := range records {
		if record.Name == codeintel.DirectoryAnatomyTransformName &&
			record.Decision == proxyDecisionInject {
			return true
		}
	}

	return false
}

func hasFilePaginationTransform(records []agentproxy.TransformRecord) bool {
	for _, record := range records {
		if isFilePaginationTransform(record) &&
			record.Decision == proxyDecisionTruncate {
			return true
		}
	}

	return false
}

func hasFileReadPaginationRecord(records []agentproxy.TransformRecord) bool {
	return slices.ContainsFunc(records, isFilePaginationTransform)
}

func hasFileReadTransformChange(records []agentproxy.TransformRecord) bool {
	if !hasFileReadPaginationRecord(records) {
		return false
	}

	return slices.ContainsFunc(records, func(record agentproxy.TransformRecord) bool {
		return record.Decision == proxyDecisionTruncate
	})
}

func isFilePaginationTransform(record agentproxy.TransformRecord) bool {
	return record.Name == agentproxy.FileReadPaginationTransformName
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

func postToolReminder(
	bundle policy.Bundle,
	event Event,
) []renderedEthosReminder {
	return postToolEthosRemindersFor(bundle.Advice.Reminders, event)
}

func buildHookOutputContext(
	command string,
	output string,
	returnCode int,
	format string,
	cwd string,
	reminders []renderedEthosReminder,
) string {
	operation := hookOperation(command)
	status := hookOutputStatus(returnCode)
	normalizer := hookOutputNormalizer(cwd)
	command = normalizer.compact(command)

	switch format {
	case outputFormatJSON:
		return buildHookOutputContextJSON(
			operation,
			status,
			command,
			output,
			returnCode,
			reminders,
		)
	case outputFormatTOON:
		return buildHookOutputContextTOON(
			operation,
			status,
			command,
			output,
			returnCode,
			reminders,
		)
	default:
		return buildHookOutputContextHuman(operation, status, output, reminders)
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
	reminders []renderedEthosReminder,
) string {
	hookType := "PRE-COMMIT"
	if operation == "push" {
		hookType = "PRE-PUSH"
	}

	outcome := "The " + operation + " succeeded."
	if status == statusBlocked {
		outcome = "The " + operation + " was blocked by hooks."
	}

	context := hookType + " OUTPUT\n" +
		strings.Repeat("=", hookDividerWidth) + "\n\n" +
		outcome + "\n\n" +
		"<hook-output>\n" + output + "\n</hook-output>\n\n" +
		"Summarize failed hooks, modified files, warnings, and required fixes. " +
		"Treat linter output as important and fix findings structurally."
	if len(reminders) > 0 {
		context += "\n\n" + humanReminderText(reminders)
	}

	return context
}

func buildHookOutputContextTOON(
	operation string,
	status string,
	command string,
	output string,
	returnCode int,
	reminders []renderedEthosReminder,
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

	lines = appendRenderedReminders(lines, reminders)

	return strings.Join(lines, "\n")
}

func buildHookOutputContextJSON(
	operation string,
	status string,
	command string,
	output string,
	returnCode int,
	reminders []renderedEthosReminder,
) string {
	payload := map[string]any{
		"format":      outputFormatJSON,
		"event":       eventPostToolUse,
		"tool":        toolBash,
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

	if len(reminders) > 0 {
		if reminders[0].Kind == reminderKindPriority {
			payload["priority_ethos_reminders"] = reminders
		} else {
			payload["ethos_reminder"] = reminders[0]
		}
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return buildHookOutputContextTOON(
			operation,
			status,
			command,
			output,
			returnCode,
			reminders,
		)
	}

	return string(encoded)
}

func hookOutputSummary(operation, status string) string {
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
		current, inlineErrAutoA := os.Getwd()
		if inlineErrAutoA == nil {
			roots = append(roots, hookTextReplacement{Old: current, New: "<repo>"})
		}
	}

	home, inlineErrAutoB := os.UserHomeDir()
	if inlineErrAutoB == nil {
		roots = append(roots, hookTextReplacement{Old: home, New: "<home>"})
	}

	roots = append(roots, hookTextReplacement{Old: os.TempDir(), New: "<tmp>"})

	replacements := []hookTextReplacement{}

	for _, root := range roots {
		cleaned := strings.TrimRight(
			filepath.Clean(root.Old),
			string(filepath.Separator),
		)
		if cleaned == "." || cleaned == "" {
			continue
		}

		replacements = append(replacements, hookTextReplacement{
			Old: filepath.ToSlash(cleaned),
			New: root.New,
		})
	}

	slices.SortFunc(replacements, func(left, right hookTextReplacement) int {
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
	ctx InspectionContext,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	event := ctx.Event

	if !matchesCommandPatterns(event.Command(), entry.CommandPatterns) {
		return nil, nil
	}

	if !matchesPathPatterns(event.Files(), entry.PathPatterns) {
		return nil, nil
	}

	context := evaluators.Context{
		Scope:              event.HookEventName,
		EventName:          event.HookEventName,
		EventMatcher:       event.Matcher,
		EventSource:        event.Source,
		Provider:           event.Provider(),
		SessionID:          event.SessionID,
		Tool:               event.ToolName,
		ToolInputKeys:      event.ToolInputKeys(),
		ToolResponseKeys:   event.ToolResponseKeys(),
		TranscriptPath:     event.TranscriptPath,
		ReturnCode:         event.ReturnCode(),
		HasReturnCode:      event.HasReturnCode(),
		HasToolInput:       event.ToolInput != nil,
		HasToolResponse:    event.ToolResponse != nil,
		Argv:               commandArgv(event.Command()),
		Command:            event.Command(),
		Content:            event.Content(),
		OldContent:         event.OldContent(),
		Cwd:                event.Cwd,
		Files:              event.Files(),
		AdminApproved:      ctx.AdminApproved,
		ReadOnlyInspection: ctx.ReadOnlyInspection,
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

	if entry.Mode == modeAdvise || entry.Mode == modeRecord ||
		entry.Mode == modeAnnotate {
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

	argv, err := shellparse.Fields(command)
	if err != nil {
		return nil
	}

	return argv
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

func matchesPathPatterns(files, patterns []string) bool {
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

func matchesPathPattern(file, pattern string) bool {
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

func pathMatches(pattern, name string) (bool, error) {
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
