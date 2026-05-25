// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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
)

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
		lines = append(lines, "coding-ethos blocked a protected operation.", "")
	}

	if result.TrackingID != "" {
		lines = append(lines, "trackingID: "+result.TrackingID, "")
	}

	for _, decision := range decisions {
		lines = append(lines, "[coding-ethos:"+decision.PolicyID+"] "+decision.Message)
		if files := decision.EvidenceFiles(); len(files) > 0 {
			lines = append(lines, "Files: "+strings.Join(files, ", "))
		}

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
		"event: " + toonCell(result.Event),
		"tool: " + toonCell(result.Tool),
		"status: " + toonCell(result.Status),
	}
	if result.TrackingID != "" {
		lines = append(lines, "trackingID: "+toonCell(result.TrackingID))
	}

	if hasSevereViolation(decisions) {
		lines = append(
			lines,
			"protected_operation: "+toonCell("coding-ethos blocked a protected operation."),
		)
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

		if files := decision.EvidenceFiles(); len(files) > 0 {
			lines = append(lines, "    files["+strconv.Itoa(len(files))+"]{path}:")
			for _, file := range files {
				lines = append(lines, "      "+toonCell(file))
			}
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
		payload["protected_operation"] = "coding-ethos blocked a protected operation."
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
		switch decision.PolicyID {
		case "filesystem.protected_path",
			"git.hook_bypass",
			"shell.forbidden_strings":
			return true
		}
	}

	return false
}
