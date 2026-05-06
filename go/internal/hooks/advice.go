// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"encoding/json"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockedAdviceHumanLinesPerDecision = 2
	severeViolationWarning             = "!!! CODING-ETHOS EMPLOYMENT VIOLATION: You attempted to tamper with or bypass the protected hook/git analysis system. This is not a misconfiguration or tool defect. You have done something wrong. Stop immediately, use the normal approved git workflow, and ask an admin if blocked. Continued attempts to circumvent, avoid, alter, delete, rebuild, or inspect this system may result in termination. !!!"
)

var severeViolationPolicyIDs = map[string]bool{
	"filesystem.protected_path": true,
	"git.hook_bypass":           true,
	"git.wrapper_required":      true,
	"shell.forbidden_strings":   true,
}

func BlockedAdvice(result Result) string {
	decisions := blockingDecisions(result.Decisions)
	if len(decisions) == 0 {
		return ""
	}

	switch selectedOutputFormat() {
	case outputFormatJSON:
		return blockedAdviceJSON(result, decisions)
	case outputFormatTOON:
		return blockedAdviceTOON(result, decisions)
	default:
		return blockedAdviceHuman(result, decisions)
	}
}

func blockingDecisions(decisions []policy.Decision) []policy.Decision {
	blocking := make([]policy.Decision, 0, len(decisions))
	for _, decision := range decisions {
		if decision.Decision == modeBlock || decision.Severity == modeBlock {
			blocking = append(blocking, decision)
		}
	}

	return blocking
}

func blockedAdviceHuman(result Result, decisions []policy.Decision) string {
	lines := make(
		[]string,
		0,
		len(decisions)*blockedAdviceHumanLinesPerDecision,
	)
	if hasSevereViolation(decisions) {
		lines = append(lines, severeViolationWarning, "")
	}

	if result.TrackingID != "" {
		lines = append(lines, "trackingID: "+result.TrackingID, "")
	}

	for _, decision := range decisions {
		lines = append(lines, "[coding-ethos:"+decision.PolicyID+"] "+decision.Message)
		if decision.Suggestion != "" {
			lines = append(lines, "Suggestion: "+decision.Suggestion)
		}
	}

	if reminders := priorityEthosRemindersFor(
		result.Advice.Reminders,
		result,
		decisions,
	); len(
		reminders,
	) > 0 {
		lines = append(lines, "", "Priority ETHOS reminders:")
		for _, reminder := range reminders {
			lines = append(
				lines,
				"- ["+reminder.PrincipleID+"] "+reminder.Axiom+" "+reminder.Action+
					" MCP: call "+reminder.MCPTool+" with "+reminder.MCPArguments+".",
			)
		}
	}

	return strings.Join(lines, "\n")
}

func blockedAdviceTOON(result Result, decisions []policy.Decision) string {
	lines := []string{
		"format: toon",
		"event: " + toonCell(result.Event),
		"tool: " + toonCell(result.Tool),
		"status: " + toonCell(result.Status),
	}
	if result.TrackingID != "" {
		lines = append(lines, "trackingID: "+toonCell(result.TrackingID))
	}

	if hasSevereViolation(decisions) {
		lines = append(lines, "violation_warning: "+toonCell(severeViolationWarning))
	}

	lines = append(lines, "decisions:")

	for _, decision := range decisions {
		lines = append(
			lines,
			"  - policy_id: "+toonCell(decision.PolicyID),
			"    decision: "+toonCell(decision.Decision),
			"    severity: "+toonCell(decision.Severity),
			"    message: "+toonCell(decision.Message),
		)
		if decision.Suggestion != "" {
			lines = append(lines, "    suggestion: "+toonCell(decision.Suggestion))
		}
	}

	if remediation := agentmsg.FromDecisions(
		decisions,
		result.Tool,
	); len(
		remediation,
	) > 0 {
		lines = append(
			lines,
			"agent_remediation["+strconv.Itoa(
				len(remediation),
			)+"]{policy_id,skill_id,failed_action,next,mcp_tool}:",
		)
		for _, item := range remediation {
			lines = append(
				lines,
				"  "+toonCell(item.PolicyID)+","+
					toonCell(item.SkillID)+","+
					toonCell(item.FailedAction)+","+
					toonCell(firstAgentStep(item))+","+
					toonCell(agentMCPTool(item)),
			)
		}
	}

	lines = appendRenderedReminders(
		lines,
		priorityEthosRemindersFor(result.Advice.Reminders, result, decisions),
	)

	return strings.Join(lines, "\n")
}

func blockedAdviceJSON(result Result, decisions []policy.Decision) string {
	payload := map[string]any{
		"format":    outputFormatJSON,
		"event":     result.Event,
		"tool":      result.Tool,
		"status":    result.Status,
		"decisions": decisions,
	}
	if result.TrackingID != "" {
		payload["trackingID"] = result.TrackingID
	}

	if remediation := agentmsg.FromDecisions(
		decisions,
		result.Tool,
	); len(
		remediation,
	) > 0 {
		payload["agent_remediation"] = remediation
	}

	if hasSevereViolation(decisions) {
		payload["violation_warning"] = severeViolationWarning
	}

	if reminders := priorityEthosRemindersFor(
		result.Advice.Reminders,
		result,
		decisions,
	); len(
		reminders,
	) > 0 {
		payload["priority_ethos_reminders"] = reminders
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return blockedAdviceTOON(result, decisions)
	}

	return string(encoded)
}

func firstAgentStep(item agentmsg.Remediation) string {
	if len(item.NextSteps) == 0 {
		return item.Advice
	}

	return item.NextSteps[0]
}

func agentMCPTool(item agentmsg.Remediation) string {
	if item.MCP == nil {
		return ""
	}

	return item.MCP.Tool
}

func hasSevereViolation(decisions []policy.Decision) bool {
	for _, decision := range decisions {
		if severeViolationPolicyIDs[decision.PolicyID] {
			return true
		}
	}

	return false
}
