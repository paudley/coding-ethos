// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/memories"
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
	operationPush    = "push"
	slowHookBudgetMS = int64(2500)
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
	startedAt := time.Now()

	activateDebugForEvent(options.Event)
	logMakeEventBoundary(options.Event)
	event, debugRequested := eventWithoutDebugFlag(options.Event)
	options.Event = event
	debuglog.Debug(
		"hook.inspection.enter",
		zap.String("event", options.Event.HookEventName),
		zap.String("tool", options.Event.ToolName),
		zap.String("provider", options.Event.Provider()),
		zap.String("cwd", options.Event.Cwd),
		zap.Strings("tool_input_keys", sortedMapKeys(options.Event.ToolInput)),
		zap.Strings("tool_response_keys", sortedMapKeys(options.Event.ToolResponse)),
		zap.Int("file_count", len(options.Event.Files())),
		zap.String("command_shape", commandShapeHash(options.Event.Command())),
		zap.Int("command_bytes", len(options.Event.Command())),
		zap.Int("command_token_estimate", debuglog.EstimateTokens(options.Event.Command())),
		zap.Int("tool_input_token_estimate", estimatedMapTokens(options.Event.ToolInput)),
		zap.Int(
			"tool_response_token_estimate",
			estimatedMapTokens(options.Event.ToolResponse),
		),
		zap.String("session_id", options.Event.SessionID),
		zap.String("matcher", options.Event.Matcher),
		zap.String("transcript_path", options.Event.TranscriptPath),
	)

	ctx := collectInspectionContext(options.Event, options.AdminApproved)
	if ctx.ReadOnlyInspection {
		debuglog.Debug(
			"hook.inspection.skip",
			zap.Bool("read_only", ctx.ReadOnlyInspection),
		)

		result := ctx.allowedResult()
		result.RuntimeMS = time.Since(startedAt).Milliseconds()
		logHookRuntime(result.RuntimeMS)

		return result, nil
	}

	decision, err := evaluateInspection(bundle, ctx, registry)
	if err != nil {
		return Result{}, err
	}

	if debugRequested && options.Event.HookEventName == eventPreToolUse {
		decision.Route = routeWithDebugEnv(options.Event, decision.Route)
	}

	result := buildResult(bundle, ctx.Event, decision)
	result.RuntimeMS = time.Since(startedAt).Milliseconds()
	logHookRuntime(result.RuntimeMS)

	return result, nil
}

func logHookRuntime(runtimeMS int64) {
	if runtimeMS > slowHookBudgetMS {
		debuglog.Debug(
			"hook.inspection.slow",
			zap.Int64("runtime_ms", runtimeMS),
			zap.Int64("budget_ms", slowHookBudgetMS),
		)

		return
	}

	debuglog.Debug("hook.inspection.exit", zap.Int64("runtime_ms", runtimeMS))
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
		memoryRouteFor,
		malformedShellRouteFor,
		shellFileToolRouteFor,
		gitWrapperRouteFor,
		lintToolRouteFor,
		pythonRuntimeRouteFor,
	} {
		route := routeFor(ctx.Event)
		if route.Block || route.Rewrite {
			debuglog.Debug(
				"hook.route.selected",
				zap.Bool("block", route.Block),
				zap.Bool("rewrite", route.Rewrite),
				zap.String("reason", route.Reason),
			)

			return route
		}
	}

	debuglog.Debug("hook.route.none")

	return InspectionRoute{}
}

func sortedMapKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	slices.Sort(keys)

	return keys
}

func estimatedMapTokens(values map[string]any) int {
	if len(values) == 0 {
		return 0
	}

	payload, err := json.Marshal(values)
	if err != nil {
		return 0
	}

	return debuglog.EstimateTokens(string(payload))
}

func evaluateDispatchedPolicies(
	bundle policy.Bundle,
	ctx InspectionContext,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	event := ctx.Event

	decisions := proxySearchReplaceEditDecisions(bundle, event)

	entries := bundle.Dispatch.Hooks[event.HookEventName][event.ToolName]

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

	if output := mergeHookSpecificOutputs(
		semanticPolicyInjectionOutput(bundle, event),
		sessionMemoryImportOutput(event),
		continuationOutput(event),
		lifecycleOutput(event),
		postEditOutput(bundle, event),
	); output != nil {
		return output, nil
	}

	if event.HookEventName == eventPostToolUse && event.ToolName != toolBash {
		if output := codeIntelEnrichmentOutput(event, event.ToolOutput()); output != nil {
			return output, nil
		}
	}

	if event.HookEventName != eventPostToolUse || event.ToolName != toolBash {
		return nil, nil
	}

	return postToolBashOutput(bundle, event)
}

func mergeHookSpecificOutputs(outputs ...*HookSpecificOutput) *HookSpecificOutput {
	var merged *HookSpecificOutput

	contexts := make([]string, 0, len(outputs))

	for _, output := range outputs {
		if output == nil {
			continue
		}

		if merged == nil {
			merged = &HookSpecificOutput{
				HookEventName:            output.HookEventName,
				PermissionDecision:       output.PermissionDecision,
				PermissionDecisionReason: output.PermissionDecisionReason,
				UpdatedInput:             cloneUpdatedInput(output.UpdatedInput),
			}
		} else {
			mergeHookOutputFields(merged, output)
		}

		if context := strings.TrimSpace(output.AdditionalContext); context != "" {
			contexts = append(contexts, context)
		}
	}

	if merged == nil {
		return nil
	}

	merged.AdditionalContext = strings.Join(contexts, "\n\n")

	return merged
}

func mergeHookOutputFields(merged, output *HookSpecificOutput) {
	if merged.HookEventName == "" {
		merged.HookEventName = output.HookEventName
	}

	if merged.PermissionDecision == "" {
		merged.PermissionDecision = output.PermissionDecision
	}

	if merged.PermissionDecisionReason == "" {
		merged.PermissionDecisionReason = output.PermissionDecisionReason
	}

	if len(output.UpdatedInput) == 0 {
		return
	}

	if merged.UpdatedInput == nil {
		merged.UpdatedInput = map[string]any{}
	}

	maps.Copy(merged.UpdatedInput, output.UpdatedInput)
}

func cloneUpdatedInput(input map[string]any) map[string]any {
	if len(input) == 0 {
		return nil
	}

	clone := make(map[string]any, len(input))
	maps.Copy(clone, input)

	return clone
}

func postToolBashOutput(
	bundle policy.Bundle,
	event Event,
) (*HookSpecificOutput, []agentproxy.ProviderEvent) {
	command := event.Command()
	output := event.ToolOutput()
	proxiedOutput := proxyPostToolOutput(event, output)
	codeIntelContext := codeIntelEnrichmentContext(event, proxiedOutput.Text)

	if !shouldEmitPostToolBashContext(event, command, output, proxiedOutput) &&
		codeIntelContext == "" {
		return nil, proxiedOutput.Events
	}

	context := buildHookOutputContext(
		command,
		proxiedOutput.Text,
		event.ReturnCode(),
		selectedOutputFormat(),
		event.Cwd,
		postToolReminder(bundle, event),
	)
	if codeIntelContext != "" {
		context = strings.TrimSpace(context + "\n\n" + codeIntelContext)
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: context,
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
		operation = operationPush
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
	if operation == operationPush {
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

	context := evaluatorContext(ctx)

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

func sessionMemoryImportOutput(event Event) *HookSpecificOutput {
	if event.HookEventName != eventSessionStart || strings.TrimSpace(event.Cwd) == "" {
		return nil
	}

	_, err := memories.ImportExisting(event.Cwd)
	if err == nil {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName: event.HookEventName,
		AdditionalContext: feedback.MustRender(feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("event", eventSessionStart),
				feedback.S("status", "warning"),
				feedback.S("summary", "memory import failed"),
				feedback.S("reason", err.Error()),
				feedback.S(
					"repair",
					"Run coding-ethos-run agent-hooks sync and inspect repo memory settings.",
				),
			},
		}, feedback.FormatTOON),
	}
}

func evaluatorContext(ctx InspectionContext) evaluators.Context {
	event := ctx.Event

	return evaluators.Context{
		Scope:              event.HookEventName,
		EventName:          event.HookEventName,
		EventMatcher:       event.Matcher,
		EventSource:        event.Source,
		Provider:           event.Provider(),
		SessionID:          event.SessionID,
		StrategicIntent:    event.StrategicIntent(),
		ActiveTodo:         event.ActiveTodo(),
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
