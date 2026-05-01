// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const protocolVersion = "2025-06-18"

type Server struct {
	bundle policy.Bundle
}

func NewServer(bundle policy.Bundle) Server {
	return Server{bundle: bundle}
}

func (server Server) Serve(reader io.Reader, writer io.Writer) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var request requestMessage
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			if err := writeResponse(writer, nil, nil, &rpcError{
				Code:    -32700,
				Message: "parse error",
			}); err != nil {
				return err
			}
			continue
		}

		if request.ID == nil {
			continue
		}

		result, responseErr := server.handle(request)
		if err := writeResponse(writer, request.ID, result, responseErr); err != nil {
			return err
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read MCP request: %w", err)
	}

	return nil
}

func (server Server) handle(request requestMessage) (any, *rpcError) {
	switch request.Method {
	case "initialize":
		return initializeResult(), nil
	case "tools/list":
		return map[string]any{"tools": toolDefinitions()}, nil
	case "tools/call":
		return server.handleToolCall(request.Params)
	default:
		return nil, &rpcError{Code: -32601, Message: "method not found"}
	}
}

func (server Server) handleToolCall(params json.RawMessage) (any, *rpcError) {
	var call toolCallParams
	if err := json.Unmarshal(params, &call); err != nil {
		return nil, &rpcError{Code: -32602, Message: "invalid tool call params"}
	}

	var (
		result any
		err    error
	)
	switch call.Name {
	case "policy_check_command":
		result, err = server.checkCommand(call.Arguments)
	case "policy_check_edit":
		result, err = server.checkEdit(call.Arguments)
	case "lint_check":
		result, err = server.checkLint(call.Arguments)
	case "lint_advice":
		result, err = server.lintAdvice(call.Arguments)
	case "policy_explain":
		result, err = server.explainPolicy(call.Arguments)
	case "skill_lookup":
		result, err = server.lookupSkill(call.Arguments)
	case "skill_recommend":
		result, err = server.recommendSkills(call.Arguments)
	default:
		return nil, &rpcError{Code: -32602, Message: "unknown tool"}
	}
	if err != nil {
		return nil, &rpcError{Code: -32602, Message: err.Error()}
	}

	return toolResult(result), nil
}

func (server Server) checkCommand(args json.RawMessage) (any, error) {
	var input commandCheckInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse command check arguments: %w", err)
	}
	if strings.TrimSpace(input.Command) == "" {
		return nil, fmt.Errorf("command is required")
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
		return nil, err
	}

	return policyCheckResponse("command", result), nil
}

func (server Server) checkEdit(args json.RawMessage) (any, error) {
	var input editCheckInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse edit check arguments: %w", err)
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}
	if input.After == "" {
		return nil, fmt.Errorf("after is required")
	}

	result, err := hooks.Run(server.bundle, hooks.Options{Event: hooks.Event{
		HookEventName: "PreToolUse",
		Source:        providerOrDefault(input.Provider),
		ToolName:      "Write",
		Cwd:           input.Cwd,
		ToolInput: map[string]any{
			"file_path": input.Path,
			"content":   input.After,
		},
	}})
	if err != nil {
		return nil, err
	}

	response := policyCheckResponse("edit", result)
	response["path"] = input.Path
	response["has_before"] = input.Before != ""

	return response, nil
}

func (server Server) checkLint(args json.RawMessage) (any, error) {
	var input lintCheckInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse lint check arguments: %w", err)
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
		return nil, err
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

func (server Server) lintAdvice(args json.RawMessage) (any, error) {
	var input lintAdviceInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse lint advice arguments: %w", err)
	}
	if strings.TrimSpace(input.Tool) == "" {
		return nil, fmt.Errorf("tool is required")
	}
	if strings.TrimSpace(input.Message) == "" {
		return nil, fmt.Errorf("message is required")
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

func (server Server) explainPolicy(args json.RawMessage) (any, error) {
	var input policyExplainInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse policy explain arguments: %w", err)
	}
	if strings.TrimSpace(input.PolicyID) == "" {
		return nil, fmt.Errorf("policy_id is required")
	}

	var buffer bytes.Buffer
	if err := policy.ExplainPolicy(&buffer, server.bundle, input.PolicyID); err != nil {
		return nil, err
	}

	return map[string]any{
		"policy_id":   input.PolicyID,
		"explanation": buffer.String(),
	}, nil
}

func (server Server) lookupSkill(args json.RawMessage) (any, error) {
	var input skillLookupInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse skill lookup arguments: %w", err)
	}
	if strings.TrimSpace(input.SkillID) == "" {
		return nil, fmt.Errorf("skill_id is required")
	}

	skill, ok := server.bundle.Skills[input.SkillID]
	if !ok {
		return nil, fmt.Errorf("unknown skill %q", input.SkillID)
	}

	principles := make([]map[string]any, 0, len(skill.PrincipleIDs))
	for _, principleID := range skill.PrincipleIDs {
		if principle, ok := server.bundle.Principles[principleID]; ok {
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
	if err := json.Unmarshal(args, &input); err != nil {
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
			"skill_id":      stringEvidence(decision.Evidence, "skill_id"),
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

func diagnosticFromInput(input lintAdviceInput) diagnostics.Diagnostic {
	return diagnostics.Diagnostic{
		Code:     strings.TrimSpace(input.Code),
		File:     strings.TrimSpace(input.File),
		Message:  strings.TrimSpace(input.Message),
		Severity: strings.TrimSpace(input.Severity),
		Tool:     strings.TrimSpace(input.Tool),
		Column:   input.Column,
		Line:     input.Line,
	}
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

	explicitSkill := server.skillIDForDiagnostic(input.Diagnostic)
	scored := make([]scoredSkill, 0, len(server.bundle.Skills))
	for _, skill := range server.bundle.Skills {
		score := 0
		if explicitSkill != "" && skill.ID == explicitSkill {
			score += 100
		}
		score += scoreSkillByText(skill, text)
		if score == 0 {
			continue
		}
		scored = append(scored, scoredSkill{skill: skill, score: score})
	}

	slices.SortFunc(scored, func(left scoredSkill, right scoredSkill) int {
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

func (server Server) skillIDForDiagnostic(input lintAdviceInput) string {
	if strings.TrimSpace(input.Tool) == "" || strings.TrimSpace(input.Message) == "" {
		return ""
	}

	enriched := diagnostics.Enrich(
		[]diagnostics.Diagnostic{diagnosticFromInput(input)},
		server.bundle.EvidenceMaps,
	)
	if len(enriched) == 0 {
		return ""
	}

	return strings.TrimSpace(enriched[0].SkillID)
}

func scoreSkillByText(skill policy.Skill, text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}

	score := 0
	for _, term := range skill.TriggerTerms {
		normalized := strings.ToLower(strings.TrimSpace(term))
		if normalized != "" && strings.Contains(text, normalized) {
			score += 10
		}
	}
	for _, signal := range []string{skill.ID, skill.Title, skill.Description, skill.Focus} {
		normalized := strings.ToLower(strings.TrimSpace(signal))
		if normalized != "" && strings.Contains(text, normalized) {
			score += 3
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

func stringEvidence(evidence map[string]any, key string) string {
	if len(evidence) == 0 {
		return ""
	}

	value, ok := evidence[key]
	if !ok {
		return ""
	}

	text, ok := value.(string)
	if !ok {
		return ""
	}

	return text
}
