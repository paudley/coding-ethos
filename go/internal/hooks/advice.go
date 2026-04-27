// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"encoding/json"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedAdviceHumanLinesPerDecision = 2

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
	for _, decision := range decisions {
		lines = append(lines, "[coding-ethos:"+decision.PolicyID+"] "+decision.Message)
		if decision.Suggestion != "" {
			lines = append(lines, "Suggestion: "+decision.Suggestion)
		}
	}

	if reminder, ok := ethosReminderFor(result, decisions); ok {
		lines = append(
			lines,
			"ETHOS reminder: "+reminder.Axiom+" "+reminder.Action,
		)
	}

	return strings.Join(lines, "\n")
}

func blockedAdviceTOON(result Result, decisions []policy.Decision) string {
	lines := []string{
		"format: toon",
		"event: " + toonCell(result.Event),
		"tool: " + toonCell(result.Tool),
		"status: " + toonCell(result.Status),
		"decisions:",
	}

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

	if reminder, ok := ethosReminderFor(result, decisions); ok {
		lines = append(
			lines,
			"ethos_reminder:",
			"  principle_id: "+toonCell(reminder.PrincipleID),
			"  axiom: "+toonCell(reminder.Axiom),
			"  action: "+toonCell(reminder.Action),
		)
	}

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
	if reminder, ok := ethosReminderFor(result, decisions); ok {
		payload["ethos_reminder"] = reminder
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return blockedAdviceTOON(result, decisions)
	}

	return string(encoded)
}
