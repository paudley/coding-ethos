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

	"blackcat.ca/coding-ethos/go/internal/hooks"
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
	case "policy_check_path":
		result, err = server.checkPath(call.Arguments)
	case "policy_explain":
		result, err = server.explainPolicy(call.Arguments)
	case "skill_lookup":
		result, err = server.lookupSkill(call.Arguments)
	case "repo_context":
		result = server.repoContext()
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

func (server Server) checkPath(args json.RawMessage) (any, error) {
	var input pathCheckInput
	if err := json.Unmarshal(args, &input); err != nil {
		return nil, fmt.Errorf("parse path check arguments: %w", err)
	}
	if strings.TrimSpace(input.Path) == "" {
		return nil, fmt.Errorf("path is required")
	}

	result, err := hooks.Run(server.bundle, hooks.Options{Event: hooks.Event{
		HookEventName: "PreToolUse",
		Source:        providerOrDefault(input.Provider),
		ToolName:      "Write",
		Cwd:           input.Cwd,
		ToolInput: map[string]any{
			"file_path": input.Path,
			"content":   input.Content,
		},
	}})
	if err != nil {
		return nil, err
	}

	return policyCheckResponse("path", result), nil
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

func (server Server) repoContext() any {
	policyIDs := make([]string, 0, len(server.bundle.Policies))
	for policyID := range server.bundle.Policies {
		policyIDs = append(policyIDs, policyID)
	}
	slices.Sort(policyIDs)

	skillIDs := make([]string, 0, len(server.bundle.Skills))
	for skillID := range server.bundle.Skills {
		skillIDs = append(skillIDs, skillID)
	}
	slices.Sort(skillIDs)

	return map[string]any{
		"bundle_id":    server.bundle.BundleID,
		"generated_at": server.bundle.GeneratedAt,
		"sources":      server.bundle.Sources,
		"policy_ids":   policyIDs,
		"skill_ids":    skillIDs,
		"principles":   len(server.bundle.Principles),
	}
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
