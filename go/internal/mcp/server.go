// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	protocolVersion         = "2025-06-18"
	maxSARIFHistoryPayloads = 1000
	skillSummaryLimit       = 100
	fallbackSkillMatchScore = 5
	repoMapResourceURI      = "coding-ethos://code-intel/repo-map"
)

var (
	errManagedLintRuntimeUnavailable = apperror.StaticError(
		"managed lint runtime is not configured",
	)
	errSARIFHistoryTooLarge = apperror.StaticError(
		"sarif history exceeds supported MCP payload count",
	)
)

type Server struct {
	runtime Runtime
	bundle  policy.Bundle
}

type Runtime struct {
	BundlePath    string
	CerunPath     string
	EthosRoot     string
	ConsumerRoot  string
	InvocationCwd string
}

func NewServer(bundle policy.Bundle) Server {
	return Server{bundle: bundle}
}

func NewServerWithRuntime(bundle policy.Bundle, runtime Runtime) Server {
	return Server{bundle: bundle, runtime: runtime}
}

func (server Server) Serve(reader io.Reader, writer io.Writer) error {
	bufferedReader := bufio.NewReader(reader)

	for {
		payload, framing, err := readMessage(bufferedReader)
		if errors.Is(err, io.EOF) {
			return nil
		}

		if err != nil {
			return fmt.Errorf("read MCP request: %w", err)
		}

		var request requestMessage

		inlineErr0 := json.Unmarshal(payload, &request)
		if inlineErr0 != nil {
			err := writeResponse(writer, framing, nil, nil, &rpcError{
				Code:    -32700,
				Message: "parse error",
			})
			if err != nil {
				return err
			}

			continue
		}

		if request.ID == nil {
			continue
		}

		result, responseErr := server.handle(request)

		inlineErr1 := writeResponse(
			writer,
			framing,
			request.ID,
			result,
			responseErr,
		)
		if inlineErr1 != nil {
			return inlineErr1
		}
	}
}

func (server Server) handle(request requestMessage) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		return initializeResult(request.Params), nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		return server.handleToolCall(request.Params)
	case "resources/list":
		return map[string]any{"resources": resourceDefinitions()}, nil
	case "resources/read":
		return server.handleResourceRead(request.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func (server Server) handleToolCall(params json.RawMessage) (any, *rpcError) {
	var call toolCallParams

	inlineErr2 := json.Unmarshal(params, &call)
	if inlineErr2 != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid tool call params"}
	}

	handler, found := server.toolHandler(call.Name)
	if !found {
		return nil, &rpcError{Code: -32602, Message: "unknown tool"}
	}

	result, err := handler(call.Arguments)
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}

	return toolResult(result), nil
}

func (server Server) handleResourceRead(params json.RawMessage) (any, *rpcError) {
	var read resourceReadParams

	inlineErr2 := json.Unmarshal(params, &read)
	if inlineErr2 != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid resource read params"}
	}

	if strings.TrimSpace(read.URI) != repoMapResourceURI {
		return nil, &rpcError{Code: -32602, Message: "unknown resource"}
	}

	result, err := server.repoMapResource()
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}

	return result, nil
}

type toolHandler func(json.RawMessage) (any, error)

func (server Server) toolHandler(name string) (toolHandler, bool) {
	for _, entry := range server.toolHandlers() {
		if entry.Name == name {
			return entry.Handler, true
		}
	}

	return nil, false
}

type toolHandlerEntry struct {
	Handler toolHandler
	Name    string
}

func (server Server) toolHandlers() []toolHandlerEntry {
	return []toolHandlerEntry{
		{Name: "policy_check_command", Handler: server.checkCommand},
		{Name: "policy_check_edit", Handler: server.checkEdit},
		{Name: "cerun_check", Handler: server.checkCerun},
		{Name: "cerun_run", Handler: server.runCerun},
		{Name: "managed_lint", Handler: server.checkLint},
		{Name: "lint_advice", Handler: server.lintAdvice},
		{Name: "sarif_remediation_advice", Handler: server.sarifRemediationAdvice},
		{Name: "sarif_risk_summary", Handler: server.sarifRiskSummary},
		{Name: "sarif_trend_analysis", Handler: server.sarifTrendAnalysis},
		{Name: "sarif_policy_feedback", Handler: server.sarifPolicyFeedback},
		{Name: "tool_capabilities", Handler: server.toolCapabilitiesHandler},
		{Name: "policy_explain", Handler: server.explainPolicy},
		{Name: "skill_lookup", Handler: server.lookupSkill},
		{Name: "remediation_explain", Handler: server.explainRemediation},
		{Name: "code_intel_overview", Handler: server.codeIntelOverview},
		{Name: "code_intel_search", Handler: server.codeIntelSearch},
		{Name: "code_intel_answer", Handler: server.codeIntelAnswer},
		{Name: "semantic_search", Handler: server.semanticSearch},
		{Name: "code_intel_index_status", Handler: server.codeIntelIndexStatus},
		{Name: "code_intel_hook_usage", Handler: server.codeIntelHookUsage},
		{Name: "code_intel_index_code", Handler: server.codeIntelIndexCode},
		{Name: "code_similarity_check", Handler: server.codeSimilarityCheck},
		{Name: "code_intel_repo_map", Handler: server.codeIntelRepoMap},
		{
			Name:    "code_intel_embedding_candidates",
			Handler: server.codeIntelEmbeddingCandidates,
		},
		{Name: "code_intel_code_chunks", Handler: server.codeIntelCodeChunks},
		{Name: "code_intel_code_context", Handler: server.codeIntelCodeContext},
		{Name: "code_intel_context_card", Handler: server.codeIntelContextCard},
		{Name: "code_intel_change_risk", Handler: server.codeIntelChangeRisk},
		{Name: "code_intel_health", Handler: server.codeIntelHealth},
		{Name: "skill_recommend", Handler: server.recommendSkills},
	}
}

func (server Server) toolCapabilitiesHandler(json.RawMessage) (any, error) {
	return server.toolCapabilities(), nil
}

func (server Server) toolCapabilities() any {
	views := toolcatalog.ToolCapabilityViews()

	return map[string]any{
		"kind":  "tool_capabilities",
		"tools": views,
		"sandbox": map[string]any{
			"default_backend":     "native",
			"required_mode":       "fail_closed",
			"advisory_auto_mode":  "records_degraded_enforcement",
			"network_tag":         "network",
			"no_network_tag":      "no-network",
			"git_tag":             "git",
			"no_git_tag":          "no-git",
			"seccomp_profile_key": "seccomp_profile",
		},
	}
}

func (server Server) checkCommand(args json.RawMessage) (any, error) {
	var input commandCheckInput

	inlineErr3 := json.Unmarshal(args, &input)
	if inlineErr3 != nil {
		return nil, fmt.Errorf("parse command check arguments: %w", inlineErr3)
	}

	if strings.TrimSpace(input.Command) == "" {
		return nil, apperror.StaticError("command is required")
	}

	result, err := hooks.Run(server.bundle, hooks.Options{Event: hooks.Event{
		HookEventName: "PreToolUse",
		Source:        providerOrDefault(input.Provider),
		ToolName:      "Bash",
		Cwd:           input.Cwd,
		ToolInput: map[string]any{
			"command": input.Command,
		},
	}})
	if err != nil {
		return nil, fmt.Errorf("run command policy check: %w", err)
	}

	return policyCheckResponse("command", result), nil
}

func (server Server) checkEdit(args json.RawMessage) (any, error) {
	var input editCheckInput

	inlineErr4 := json.Unmarshal(args, &input)
	if inlineErr4 != nil {
		return nil, fmt.Errorf("parse edit check arguments: %w", inlineErr4)
	}

	if strings.TrimSpace(input.Path) == "" {
		return nil, apperror.StaticError("path is required")
	}

	if input.After == "" {
		return nil, apperror.StaticError("after is required")
	}

	result, err := hooks.Run(server.bundle, hooks.Options{Event: hooks.Event{
		HookEventName: "PreToolUse",
		Source:        providerOrDefault(input.Provider),
		ToolName:      "Write",
		Cwd:           input.Cwd,
		ToolInput: map[string]any{
			"file_path": input.Path,
			"before":    input.Before,
			"content":   input.After,
		},
	}})
	if err != nil {
		return nil, fmt.Errorf("run edit policy check: %w", err)
	}

	response := policyCheckResponse("edit", result)
	response["path"] = input.Path
	response["has_before"] = input.Before != ""

	return response, nil
}

func (server Server) checkLint(args json.RawMessage) (any, error) {
	var input lintCheckInput

	inlineErr5 := json.Unmarshal(args, &input)
	if inlineErr5 != nil {
		return nil, fmt.Errorf("parse lint check arguments: %w", inlineErr5)
	}

	if strings.TrimSpace(input.Tool) != "" {
		return server.checkManagedLint(input)
	}

	result, err := lint.Run(server.bundle, lint.Options{
		Command:       input.Command,
		Cwd:           input.Cwd,
		Scope:         input.Scope,
		Files:         append([]string(nil), input.Files...),
		Argv:          append([]string(nil), input.Argv...),
		AdminApproved: input.AdminApproved,
	})
	if err != nil {
		return nil, fmt.Errorf("run lint policy check: %w", err)
	}

	return map[string]any{
		"engine":      "compiled_policy_lint",
		"scope":       result.Scope,
		"status":      result.Status,
		"blocked":     result.Blocked(),
		"files":       result.Files,
		"findings":    result.Findings,
		"diagnostics": result.Diagnostics,
		"skill_hints": result.SkillHints,
	}, nil
}

func (server Server) checkManagedLint(input lintCheckInput) (any, error) {
	tool := strings.TrimSpace(input.Tool)
	if _, found := toolcatalog.HookOwnedTool(tool); !found {
		return nil, apperror.Wrapf(
			apperror.StaticError("unknown managed lint tool %q"),
			"unknown managed lint tool %q",
			tool,
		)
	}

	inlineErr6 := server.runtime.validateManagedLint()
	if inlineErr6 != nil {
		return nil, inlineErr6
	}

	var stdout bytes.Buffer

	exitCode := managedcapture.Run(managedcapture.Options{
		PolicyContext: managedLintPolicyContext(server.bundle),
		Tool:          tool,
		EthosRoot:     server.runtime.EthosRoot,
		ConsumerRoot:  server.runtime.ConsumerRoot,
		InvocationCwd: firstNonEmpty(input.Cwd, server.runtime.InvocationCwd),
		OutputFormat:  hookoutput.FormatJSON,
		Output:        &stdout,
		Args:          managedLintToolArgs(input),
	})

	output := stdout.Bytes()

	var result lint.Result

	inlineErr7 := json.Unmarshal(output, &result)
	if inlineErr7 != nil {
		return nil, fmt.Errorf(
			"decode managed lint %q JSON: %w; output: %s",
			tool,
			inlineErr7,
			strings.TrimSpace(stdout.String()),
		)
	}

	return managedLintOutput(tool, exitCode, result), nil
}

func managedLintToolArgs(input lintCheckInput) []string {
	args := make([]string, 0, len(input.Argv)+len(input.Files))
	args = append(args, input.Argv...)
	args = append(args, input.Files...)

	return args
}

func managedLintPolicyContext(bundle policy.Bundle) managedcapture.PolicyContext {
	return managedcapture.PolicyContext{
		Skills:       bundle.Skills,
		EvidenceMaps: bundle.EvidenceMaps,
		Policies:     managedLintPolicies(bundle.Policies),
	}
}

func managedLintPolicies(policies map[string]policy.Policy) []policy.Policy {
	items := make([]policy.Policy, 0, len(policies))
	for _, item := range policies {
		items = append(items, item)
	}

	return items
}

func managedLintOutput(
	tool string,
	exitCode int,
	result lint.Result,
) map[string]any {
	return map[string]any{
		"engine":      "managed_lint_capture",
		"tool":        tool,
		"exit_code":   exitCode,
		"status":      result.Status,
		"blocked":     result.Blocked(),
		"files":       result.Files,
		"findings":    result.Findings,
		"diagnostics": result.Diagnostics,
		"skill_hints": result.SkillHints,
		"capture":     result.Capture,
	}
}

func (server Server) lintAdvice(args json.RawMessage) (any, error) {
	var input lintAdviceInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse lint advice arguments: %w", err)
	}

	if strings.TrimSpace(input.Tool) == "" {
		return nil, apperror.StaticError("tool is required")
	}

	if strings.TrimSpace(input.Message) == "" {
		return nil, apperror.StaticError("message is required")
	}

	enriched := lint.EnrichResultWithSkills(
		lint.Result{
			Diagnostics: diagnostics.Enrich(
				[]diagnostics.Diagnostic{diagnosticFromInput(input)},
				server.bundle.EvidenceMaps,
			),
		},
		server.bundle.Skills,
	)

	return map[string]any{
		"diagnostic":  enriched.Diagnostics[0],
		"skill_hints": enriched.SkillHints,
	}, nil
}

func (server Server) sarifRemediationAdvice(args json.RawMessage) (any, error) {
	var input sarifRemediationInput

	inlineErr8 := json.Unmarshal(args, &input)
	if inlineErr8 != nil {
		return nil, fmt.Errorf("parse SARIF remediation arguments: %w", inlineErr8)
	}

	if strings.TrimSpace(input.SARIF) == "" {
		sarif, err := server.sarifFromTraceID(input.TraceID)
		if err != nil {
			return nil, err
		}

		input.SARIF = sarif
	}

	if input.ResultIndex < 0 {
		return nil, apperror.StaticError("result_index must be non-negative")
	}

	finding, err := parseSARIFRemediationFinding(input.SARIF, input.ResultIndex)
	if err != nil {
		return nil, err
	}

	if finding.PolicyID == "" {
		finding.PolicyID = finding.RuleID
	}

	finding = server.enrichSARIFRemediationFinding(finding)
	response := sarifRemediationResponse(finding)
	server.addSARIFRemediationContext(response, finding)

	return response, nil
}

func (server Server) enrichSARIFRemediationFinding(
	finding sarifRemediationFinding,
) sarifRemediationFinding {
	if finding.SkillID == "" {
		finding.SkillID = server.skillIDForDiagnostic(finding.diagnosticInput())
	}

	if len(finding.PrincipleIDs) == 0 {
		finding.PrincipleIDs = server.principleIDsForPolicy(finding.PolicyID)
	}

	return finding
}

func sarifRemediationResponse(finding sarifRemediationFinding) map[string]any {
	return map[string]any{
		"finding": finding.summary(),
		"advice":  finding.advice(),
		"rerun":   finding.rerun(),
		"guardrails": []string{
			"Apply structural fixes; do not weaken coding-ethos policy " +
				"or generated tool configuration.",
			"Use the MCP managed_lint path or managed project commands to verify the repair.",
		},
	}
}

func (server Server) addSARIFRemediationContext(
	response map[string]any,
	finding sarifRemediationFinding,
) {
	if finding.PolicyID != "" {
		response["policy"] = server.policySummary(finding.PolicyID)
	}

	if len(finding.PrincipleIDs) > 0 {
		response["principles"] = server.principleSummaries(finding.PrincipleIDs)
	}

	if finding.SkillID != "" {
		if skill, found := server.bundle.Skills[finding.SkillID]; found {
			response["skill"] = skillSummary(skill, skillSummaryLimit)
		}
	}
}

func (server Server) sarifRiskSummary(args json.RawMessage) (any, error) {
	var input sarifRiskSummaryInput

	inlineErr9 := json.Unmarshal(args, &input)
	if inlineErr9 != nil {
		return nil, fmt.Errorf("parse SARIF risk summary arguments: %w", inlineErr9)
	}

	if strings.TrimSpace(input.SARIF) == "" {
		sarif, err := server.sarifFromTraceID(input.TraceID)
		if err != nil {
			return nil, err
		}

		input.SARIF = sarif
	}

	summary, err := summarizeSARIFRisk(input.SARIF)
	if err != nil {
		return nil, err
	}

	return summary, nil
}

func (server Server) sarifTrendAnalysis(args json.RawMessage) (any, error) {
	var input sarifTrendInput

	inlineErr10 := json.Unmarshal(args, &input)
	if inlineErr10 != nil {
		return nil, fmt.Errorf("parse SARIF trend analysis arguments: %w", inlineErr10)
	}

	baseline, err := server.sarifPayloadFromEither(
		input.BaselineSARIF,
		input.BaselineTraceID,
	)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}

	current, err := server.sarifPayloadFromEither(
		input.CurrentSARIF,
		input.CurrentTraceID,
	)
	if err != nil {
		return nil, fmt.Errorf("current: %w", err)
	}

	history, err := server.sarifHistoryPayloads(
		input.HistorySARIF,
		input.HistoryTraceIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}

	trend, err := analyzeSARIFTrend(baseline, current, history)
	if err != nil {
		return nil, err
	}

	return trend, nil
}

func (server Server) sarifPolicyFeedback(args json.RawMessage) (any, error) {
	var input sarifPolicyFeedbackInput

	inlineErr11 := json.Unmarshal(args, &input)
	if inlineErr11 != nil {
		return nil, fmt.Errorf("parse SARIF policy feedback arguments: %w", inlineErr11)
	}

	payload, err := server.sarifPayloadFromEither(input.SARIF, input.TraceID)
	if err != nil {
		return nil, err
	}

	feedback, err := analyzeSARIFPolicyFeedback(payload)
	if err != nil {
		return nil, err
	}

	return feedback, nil
}

func (server Server) sarifFromTraceID(traceID string) (string, error) {
	tracePath, err := server.resolveLintTraceID(traceID)
	if err != nil {
		return "", fmt.Errorf("resolve lint trace id: %w", err)
	}

	sidecar, err := os.ReadFile(lint.SARIFPathForTracePath(tracePath))
	if err == nil {
		return string(sidecar), nil
	}

	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read lint trace SARIF sidecar: %w", err)
	}

	result, err := lint.ReplayTrace(tracePath)
	if err != nil {
		return "", fmt.Errorf("replay lint trace: %w", err)
	}

	output, err := hookoutput.FormatLintResult(result, hookoutput.FormatSARIF)
	if err != nil {
		return "", fmt.Errorf("format lint trace as SARIF: %w", err)
	}

	return output, nil
}

func (server Server) sarifPayloadFromEither(sarif, traceID string) (string, error) {
	if strings.TrimSpace(sarif) != "" {
		return sarif, nil
	}

	return server.sarifFromTraceID(traceID)
}

func (server Server) sarifHistoryPayloads(
	sarifPayloads []string,
	traceIDs []string,
) ([]string, error) {
	if len(sarifPayloads) > maxSARIFHistoryPayloads ||
		len(traceIDs) > maxSARIFHistoryPayloads-len(sarifPayloads) {
		return nil, errSARIFHistoryTooLarge
	}

	history := make([]string, 0, len(sarifPayloads)+len(traceIDs))
	for _, payload := range sarifPayloads {
		if strings.TrimSpace(payload) != "" {
			history = append(history, payload)
		}
	}

	for _, traceID := range traceIDs {
		if strings.TrimSpace(traceID) == "" {
			continue
		}

		payload, err := server.sarifFromTraceID(traceID)
		if err != nil {
			return nil, err
		}

		history = append(history, payload)
	}

	return history, nil
}

func (server Server) resolveLintTraceID(traceID string) (string, error) {
	traceID = strings.TrimSpace(traceID)
	if traceID == "" {
		return "", apperror.StaticError("sarif or trace_id is required")
	}

	if strings.ContainsAny(traceID, `/\`) ||
		traceID == "." ||
		traceID == ".." ||
		strings.Contains(traceID, "..") {
		return "", apperror.StaticError(
			"trace_id must be a lint trace file name, not a path",
		)
	}

	if strings.TrimSpace(server.runtime.ConsumerRoot) == "" {
		return "", errManagedLintRuntimeUnavailable
	}

	if !strings.HasSuffix(traceID, ".json") {
		traceID += ".json"
	}

	path, err := lint.TracePathForID(server.runtime.ConsumerRoot, traceID)
	if err != nil {
		return "", fmt.Errorf("resolve lint trace path: %w", err)
	}

	return path, nil
}

func (server Server) explainPolicy(args json.RawMessage) (any, error) {
	var input policyExplainInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse policy explain arguments: %w", err)
	}

	if strings.TrimSpace(input.PolicyID) == "" {
		return nil, apperror.StaticError("policy_id is required")
	}

	var buffer bytes.Buffer

	err = policy.ExplainPolicy(&buffer, server.bundle, input.PolicyID)
	if err != nil {
		return nil, fmt.Errorf("explain policy %q: %w", input.PolicyID, err)
	}

	return map[string]any{
		"policy_id":   input.PolicyID,
		"explanation": buffer.String(),
	}, nil
}

func (server Server) policySummary(policyID string) map[string]any {
	policyDef, found := server.bundle.Policies[policyID]
	if !found {
		return map[string]any{"id": policyID}
	}

	return map[string]any{
		"id":              policyDef.ID,
		"category":        policyDef.Category,
		"message":         policyDef.Message,
		"suggestion":      policyDef.Suggestion,
		"principle_ids":   policyDef.PrincipleIDs,
		"supported_modes": policyDef.SupportedModes,
	}
}

func (server Server) principleIDsForPolicy(policyID string) []string {
	policyDef, found := server.bundle.Policies[policyID]
	if !found {
		return nil
	}

	return append([]string(nil), policyDef.PrincipleIDs...)
}

func (server Server) principleSummaries(principleIDs []string) []map[string]any {
	principles := make([]map[string]any, 0, len(principleIDs))
	for _, principleID := range principleIDs {
		principle, found := server.bundle.Principles[principleID]
		if !found {
			principles = append(principles, map[string]any{"id": principleID})

			continue
		}

		principles = append(principles, map[string]any{
			"id":        principle.ID,
			"title":     principle.Title,
			"directive": principle.Directive,
			"summary":   principle.Summary,
		})
	}

	return principles
}

func (server Server) lookupSkill(args json.RawMessage) (any, error) {
	var input skillLookupInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse skill lookup arguments: %w", err)
	}

	if strings.TrimSpace(input.SkillID) == "" {
		return nil, apperror.StaticError("skill_id is required")
	}

	skill, found := server.bundle.Skills[input.SkillID]
	if !found {
		return nil, apperror.Wrapf(
			apperror.StaticError("unknown skill %q"),
			"unknown skill %q",
			input.SkillID,
		)
	}

	principles := make([]map[string]any, 0, len(skill.PrincipleIDs))
	for _, principleID := range skill.PrincipleIDs {
		if principle, found := server.bundle.Principles[principleID]; found {
			principles = append(principles, map[string]any{
				"id":          principle.ID,
				"title":       principle.Title,
				"directive":   principle.Directive,
				"detail_path": principle.DetailPath,
			})
		}
	}

	return map[string]any{
		"id":                skill.ID,
		"title":             skill.Title,
		"description":       skill.Description,
		"short_hint":        skill.ShortHint,
		"focus":             skill.Focus,
		"trigger_terms":     skill.TriggerTerms,
		"remediation_steps": skill.RemediationSteps,
		"principle_ids":     skill.PrincipleIDs,
		"principles":        principles,
	}, nil
}

func (server Server) recommendSkills(args json.RawMessage) (any, error) {
	var input skillRecommendInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse skill recommendation arguments: %w", err)
	}

	limit := input.Limit
	if limit <= 0 {
		limit = 3
	}

	return map[string]any{
		"recommendations": server.skillRecommendations(input, limit),
	}, nil
}

func argsContext() context.Context {
	return context.Background()
}

func policyCheckResponse(scope string, result hooks.Result) map[string]any {
	decisions := make([]map[string]any, 0, len(result.Decisions))
	for _, decision := range result.Decisions {
		decisions = append(decisions, map[string]any{
			"policy_id":     decision.PolicyID,
			"decision":      decision.Decision,
			"severity":      decision.Severity,
			"message":       decision.Message,
			"suggestion":    decision.Suggestion,
			"principle_ids": decision.PrincipleIDs,
			"skill_id":      decision.EvidenceSkillID(),
		})
	}

	return map[string]any{
		"scope":     scope,
		"status":    result.Status,
		"blocked":   result.Blocked(),
		"decisions": decisions,
	}
}

func providerOrDefault(provider string) string {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "mcp"
	}

	return provider
}

func (runtime Runtime) validateManagedLint() error {
	if strings.TrimSpace(runtime.BundlePath) == "" ||
		strings.TrimSpace(runtime.EthosRoot) == "" ||
		strings.TrimSpace(runtime.ConsumerRoot) == "" {
		return errManagedLintRuntimeUnavailable
	}

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

type scoredSkill struct {
	skill policy.Skill
	score int
}

func (server Server) skillRecommendations(
	input skillRecommendInput,
	limit int,
) []map[string]any {
	text := strings.ToLower(strings.Join([]string{
		input.Intent,
		input.Command,
		input.Path,
		input.Diagnostic.Tool,
		input.Diagnostic.Code,
		input.Diagnostic.File,
		input.Diagnostic.Message,
	}, " "))

	enrichedDiagnostic := server.enrichedDiagnostic(input.Diagnostic)
	explicitSkill := strings.TrimSpace(enrichedDiagnostic.SkillID)

	scored := make([]scoredSkill, 0, len(server.bundle.Skills))
	for _, skill := range server.bundle.Skills {
		score := 0
		if explicitSkill != "" && skill.ID == explicitSkill {
			score += 100
		}

		score += scoreSkillByPrincipleOverlap(skill, enrichedDiagnostic.PrincipleIDs)

		score += scoreSkillByText(skill, text)
		if score == 0 {
			continue
		}

		scored = append(scored, scoredSkill{skill: skill, score: score})
	}

	slices.SortFunc(scored, func(left, right scoredSkill) int {
		if left.score != right.score {
			return right.score - left.score
		}

		return strings.Compare(left.skill.ID, right.skill.ID)
	})

	if len(scored) > limit {
		scored = scored[:limit]
	}

	recommendations := make([]map[string]any, 0, len(scored))
	for _, item := range scored {
		recommendations = append(recommendations, skillSummary(item.skill, item.score))
	}

	return recommendations
}

func fallbackSkillScore(skill policy.Skill, text string) int {
	if skill.ID != "agent-operating-discipline" || !broadEngineeringWorkSignal(text) {
		return 0
	}

	return fallbackSkillMatchScore
}

func broadEngineeringWorkSignal(text string) bool {
	for _, signal := range []string{
		"address",
		"debug",
		"diagnose",
		"fix",
		"implement",
		"repair",
		"resolve",
		"refactor",
	} {
		if strings.Contains(text, signal) {
			return true
		}
	}

	return false
}

func scoreSkillByText(skill policy.Skill, text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}

	score := fallbackSkillScore(skill, text)

	for _, term := range skill.TriggerTerms {
		normalized := strings.ToLower(strings.TrimSpace(term))
		if normalized != "" && strings.Contains(text, normalized) {
			score += 10
		}
	}

	for _, signal := range []string{
		skill.ID,
		skill.Title,
		skill.Description,
		skill.Focus,
	} {
		normalized := strings.ToLower(strings.TrimSpace(signal))
		if normalized != "" && strings.Contains(text, normalized) {
			score += 3
		}
	}

	return score
}

func scoreSkillByPrincipleOverlap(skill policy.Skill, principleIDs []string) int {
	if len(skill.PrincipleIDs) == 0 || len(principleIDs) == 0 {
		return 0
	}

	score := 0

	for _, skillPrincipleID := range skill.PrincipleIDs {
		for _, diagnosticPrincipleID := range principleIDs {
			if skillPrincipleID == diagnosticPrincipleID {
				score += 20
			}
		}
	}

	return score
}

func skillSummary(skill policy.Skill, score int) map[string]any {
	return map[string]any{
		"id":                skill.ID,
		"title":             skill.Title,
		"description":       skill.Description,
		"short_hint":        skill.ShortHint,
		"focus":             skill.Focus,
		"trigger_terms":     skill.TriggerTerms,
		"remediation_steps": skill.RemediationSteps,
		"principle_ids":     skill.PrincipleIDs,
		"score":             score,
	}
}
