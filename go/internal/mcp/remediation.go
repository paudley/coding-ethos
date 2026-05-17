// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

func (server Server) explainRemediation(args json.RawMessage) (any, error) {
	var input remediationExplainInput

	err := json.Unmarshal(args, &input)
	if err != nil {
		return nil, fmt.Errorf("parse remediation explain arguments: %w", err)
	}

	remediation := normalizeRemediationInput(input)
	if strings.TrimSpace(remediation.Message) == "" &&
		strings.TrimSpace(remediation.PolicyID) == "" &&
		strings.TrimSpace(remediation.SkillID) == "" {
		return nil, apperror.StaticError(
			"remediation payload, policy_id, or skill_id is required",
		)
	}

	response := map[string]any{
		"kind":        "remediation_explain",
		"remediation": remediation,
		"next_steps":  remediation.NextSteps,
		"mcp":         remediation.MCP,
	}
	if remediation.PolicyID != "" {
		response["policy"] = server.policySummary(remediation.PolicyID)
		response["principles"] = server.principleSummaries(
			firstStringSlice(
				remediation.PrincipleIDs,
				server.principleIDsForPolicy(remediation.PolicyID),
			),
		)
	}

	if remediation.SkillID != "" {
		response["skill"] = server.skillSummaryByID(remediation.SkillID)
	}

	if remediation.Command != "" || remediation.Path != "" || remediation.File != "" {
		response["action_context"] = map[string]any{
			"failed_action": remediation.FailedAction,
			"command":       remediation.Command,
			"file":          remediation.File,
			"path":          remediation.Path,
			"tool":          remediation.Tool,
		}
	}

	return response, nil
}

func normalizeRemediationInput(input remediationExplainInput) agentmsg.Remediation {
	remediation := input.Remediation
	remediation.ID = firstNonEmpty(remediation.ID, input.ID)
	remediation.Code = firstNonEmpty(remediation.Code, input.Code)
	remediation.Command = firstNonEmpty(remediation.Command, input.Command)
	remediation.FailedAction = firstNonEmpty(
		remediation.FailedAction,
		input.FailedAction,
	)
	remediation.File = firstNonEmpty(remediation.File, input.File)
	remediation.Message = firstNonEmpty(remediation.Message, input.Message)
	remediation.Path = firstNonEmpty(remediation.Path, input.Path)
	remediation.PolicyID = firstNonEmpty(remediation.PolicyID, input.PolicyID)
	remediation.Severity = firstNonEmpty(remediation.Severity, input.Severity)
	remediation.SkillID = firstNonEmpty(remediation.SkillID, input.SkillID)

	remediation.Tool = firstNonEmpty(remediation.Tool, input.Tool)
	if remediation.Line == 0 {
		remediation.Line = input.Line
	}

	if remediation.Column == 0 {
		remediation.Column = input.Column
	}

	if remediation.MCP == nil {
		remediation.MCP = remediationMCP(remediation.PolicyID, remediation.SkillID)
	}

	if len(remediation.NextSteps) == 0 {
		remediation.NextSteps = remediationSteps(
			remediation.PolicyID,
			remediation.SkillID,
		)
	}

	if remediation.SkillUse == "" && remediation.SkillID != "" {
		remediation.SkillUse = fmt.Sprintf(
			"Load the %s skill before editing or retrying.",
			remediation.SkillID,
		)
	}

	return remediation
}

func (server Server) skillSummaryByID(skillID string) map[string]any {
	skill, ok := server.bundle.Skills[skillID]
	if !ok {
		return map[string]any{"id": skillID}
	}

	return map[string]any{
		"id":                skill.ID,
		"title":             skill.Title,
		"description":       skill.Description,
		"short_hint":        skill.ShortHint,
		"focus":             skill.Focus,
		"remediation_steps": skill.RemediationSteps,
		"principle_ids":     skill.PrincipleIDs,
	}
}

func remediationMCP(policyID, skillID string) *agentmsg.MCPCall {
	if strings.TrimSpace(policyID) != "" {
		return &agentmsg.MCPCall{
			Tool: "policy_explain",
			Arguments: map[string]string{
				"policy_id": strings.TrimSpace(policyID),
			},
		}
	}

	if strings.TrimSpace(skillID) != "" {
		return &agentmsg.MCPCall{
			Tool: "skill_lookup",
			Arguments: map[string]string{
				"skill_id": strings.TrimSpace(skillID),
			},
		}
	}

	return nil
}

func remediationSteps(policyID, skillID string) []string {
	steps := []string{}
	if strings.TrimSpace(policyID) != "" {
		steps = append(
			steps,
			fmt.Sprintf(
				"Call MCP policy_explain with policy_id=%s before retrying.",
				policyID,
			),
		)
	}

	if strings.TrimSpace(skillID) != "" {
		steps = append(
			steps,
			fmt.Sprintf(
				"Call MCP skill_lookup with skill_id=%s for the repair playbook.",
				skillID,
			),
		)
	}

	if len(steps) == 0 {
		steps = append(steps, "Fix the reported violation before retrying.")
	}

	return steps
}

func firstStringSlice(values ...[]string) []string {
	for _, value := range values {
		if len(value) > 0 {
			return append([]string(nil), value...)
		}
	}

	return nil
}
