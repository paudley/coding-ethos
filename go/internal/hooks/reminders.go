// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	maxPriorityEthosReminders = 3
	staticAnalysisPrincipleID = "static-analysis-is-the-first-line-of-defense"
	lintQualityPrincipleID    = "linting-as-code-quality-enforcement"
)

type reminderKind string

const (
	reminderKindAmbient  reminderKind = "ambient"
	reminderKindPriority reminderKind = "priority"
)

type renderedEthosReminder struct {
	MCPArguments string       `json:"mcp_arguments"`
	MCPTool      string       `json:"mcp_tool"`
	PolicyID     string       `json:"policy_id,omitempty"`
	PrincipleID  string       `json:"principle_id"`
	Axiom        string       `json:"axiom"`
	Action       string       `json:"action"`
	Kind         reminderKind `json:"kind"`
}

func priorityEthosRemindersFor(
	config policy.ReminderConfig,
	result Result,
	decisions []policy.Decision,
) []renderedEthosReminder {
	if len(config.Items) == 0 {
		config = fallbackReminderConfig()
	}

	candidates := ethosReminderCandidates(config, decisions)
	if len(candidates) == 0 {
		return nil
	}

	reminders := make(
		[]renderedEthosReminder,
		0,
		minInt(maxPriorityEthosReminders, len(candidates)),
	)
	seen := map[string]bool{}
	for _, decision := range decisions {
		for _, principleID := range decision.PrincipleIDs {
			key := decision.PolicyID + "\x00" + principleID
			if seen[key] {
				continue
			}

			principleReminders := ethosRemindersForPrinciple(config, principleID)
			if len(principleReminders) == 0 {
				continue
			}

			selected := principleReminders[stableDecisionReminderIndex(
				result,
				decision,
				len(principleReminders),
			)]
			reminders = append(
				reminders,
				renderPolicyReminder(decision.PolicyID, selected, reminderKindPriority),
			)
			seen[key] = true
			if len(reminders) >= maxPriorityEthosReminders {
				return reminders
			}
		}
	}

	return reminders
}

func postToolEthosRemindersFor(
	config policy.ReminderConfig,
	event Event,
) []renderedEthosReminder {
	if event.HookEventName != "PostToolUse" {
		return nil
	}
	if len(config.Items) == 0 {
		config = fallbackReminderConfig()
	}

	if reminders := postToolPriorityReminders(config, event); len(reminders) > 0 {
		return reminders
	}

	reminder, ok := ambientPostToolReminder(config, event)
	if !ok {
		return nil
	}

	return []renderedEthosReminder{
		renderPrincipleReminder(reminder, reminderKindAmbient),
	}
}

func ambientPostToolReminder(
	config policy.ReminderConfig,
	event Event,
) (policy.EthosReminder, bool) {
	if isLintCommand(event.Command()) {
		return config.Items[stablePostToolReminderIndex(event, len(config.Items))], true
	}

	percent := config.AmbientFrequencyPercent
	if percent == 0 && config.QuietFrequency > 0 {
		percent = 100 / config.QuietFrequency
	}
	if percent <= 0 {
		percent = policy.DefaultReminderAmbientFrequencyPercent()
	}
	if percent > 100 {
		percent = 100
	}

	index := stablePostToolReminderIndex(event, len(config.Items)*100)
	if index%100 >= percent {
		return policy.EthosReminder{}, false
	}

	return config.Items[(index/100)%len(config.Items)], true
}

func postToolPriorityReminders(
	config policy.ReminderConfig,
	event Event,
) []renderedEthosReminder {
	if !isLintCommand(event.Command()) {
		return nil
	}

	reminders := []renderedEthosReminder{}
	for _, principleID := range []string{staticAnalysisPrincipleID, lintQualityPrincipleID} {
		principleReminders := ethosRemindersForPrinciple(config, principleID)
		if len(principleReminders) == 0 {
			continue
		}

		selected := principleReminders[stablePrincipleReminderIndex(
			event,
			principleID,
			len(principleReminders),
		)]
		reminders = append(reminders, renderPrincipleReminder(selected, reminderKindPriority))
	}

	return reminders
}

func appendRenderedReminders(
	lines []string,
	reminders []renderedEthosReminder,
) []string {
	if len(reminders) == 0 {
		return lines
	}

	header := "ethos_reminder:"
	if reminders[0].Kind == reminderKindPriority {
		header = fmt.Sprintf(
			"priority_ethos_reminders[%d]{policy_id,principle_id,axiom,action,mcp_tool,mcp_arguments}:",
			len(reminders),
		)
	}

	lines = append(lines, "", header)
	for _, reminder := range reminders {
		if reminder.Kind == reminderKindAmbient {
			lines = append(
				lines,
				"  principle_id: "+toonCell(reminder.PrincipleID),
				"  axiom: "+toonCell(reminder.Axiom),
				"  action: "+toonCell(reminder.Action),
				"  mcp_tool: "+toonCell(reminder.MCPTool),
				"  mcp_arguments: "+toonCell(reminder.MCPArguments),
			)

			continue
		}

		lines = append(
			lines,
			"  "+toonCell(displayPolicyID(reminder.PolicyID))+","+
				toonCell(reminder.PrincipleID)+","+
				toonCell(reminder.Axiom)+","+
				toonCell(reminder.Action)+","+
				toonCell(reminder.MCPTool)+","+
				toonCell(reminder.MCPArguments),
		)
	}

	return lines
}

func displayPolicyID(policyID string) string {
	if strings.TrimSpace(policyID) == "" {
		return "-"
	}

	return policyID
}

func humanReminderText(reminders []renderedEthosReminder) string {
	if len(reminders) == 0 {
		return ""
	}

	if reminders[0].Kind == reminderKindAmbient {
		reminder := reminders[0]

		return "ETHOS reminder: " + reminder.Axiom + " " + reminder.Action +
			" MCP: call " + reminder.MCPTool + " with " + reminder.MCPArguments + "."
	}

	lines := []string{"Priority ETHOS reminders:"}
	for _, reminder := range reminders {
		lines = append(
			lines,
			"- ["+reminder.PrincipleID+"] "+reminder.Axiom+" "+reminder.Action+
				" MCP: call "+reminder.MCPTool+" with "+reminder.MCPArguments+".",
		)
	}

	return strings.Join(lines, "\n")
}

func ethosReminderCandidates(
	config policy.ReminderConfig,
	decisions []policy.Decision,
) []policy.EthosReminder {
	if len(config.Items) == 0 {
		config = fallbackReminderConfig()
	}

	candidates := []policy.EthosReminder{}
	seen := map[string]bool{}

	for _, decision := range decisions {
		for _, principleID := range decision.PrincipleIDs {
			if seen[principleID] {
				continue
			}

			reminders := ethosRemindersForPrinciple(config, principleID)
			if len(reminders) == 0 {
				continue
			}

			candidates = append(candidates, reminders...)
			seen[principleID] = true
		}
	}

	return candidates
}

func fallbackReminderConfig() policy.ReminderConfig {
	return policy.ExampleBundle().Advice.Reminders
}

func stableDecisionReminderIndex(
	result Result,
	decision policy.Decision,
	candidateCount int,
) int {
	parts := []string{result.Event, result.Tool, result.Status, decision.PolicyID}
	parts = append(parts, decision.PrincipleIDs...)

	return stableStringIndex(parts, candidateCount)
}

func stablePostToolReminderIndex(event Event, candidateCount int) int {
	parts := []string{
		event.HookEventName,
		event.ToolName,
		hookOutputStatus(event.ReturnCode()),
		event.Command(),
		strings.Join(event.Files(), "\x00"),
	}

	return stableStringIndex(parts, candidateCount)
}

func stablePrincipleReminderIndex(
	event Event,
	principleID string,
	candidateCount int,
) int {
	parts := []string{
		event.HookEventName,
		event.ToolName,
		event.Command(),
		principleID,
	}

	return stableStringIndex(parts, candidateCount)
}

func ethosRemindersForPrinciple(
	config policy.ReminderConfig,
	principleID string,
) []policy.EthosReminder {
	reminders := []policy.EthosReminder{}
	for _, reminder := range config.Items {
		if reminder.PrincipleID == principleID {
			reminders = append(reminders, reminder)
		}
	}

	return reminders
}

func renderPolicyReminder(
	policyID string,
	reminder policy.EthosReminder,
	kind reminderKind,
) renderedEthosReminder {
	return renderedEthosReminder{
		PolicyID:     policyID,
		PrincipleID:  reminder.PrincipleID,
		Axiom:        reminder.Axiom,
		Action:       reminder.Action,
		Kind:         kind,
		MCPTool:      "policy_explain",
		MCPArguments: fmt.Sprintf(`{"policy_id":%q}`, policyID),
	}
}

func renderPrincipleReminder(
	reminder policy.EthosReminder,
	kind reminderKind,
) renderedEthosReminder {
	return renderedEthosReminder{
		PrincipleID: reminder.PrincipleID,
		Axiom:       reminder.Axiom,
		Action:      reminder.Action,
		Kind:        kind,
		MCPTool:     "skill_recommend",
		MCPArguments: fmt.Sprintf(
			`{"intent":"apply ETHOS principle %s to this hook result","limit":1}`,
			reminder.PrincipleID,
		),
	}
}

func isLintCommand(command string) bool {
	lower := strings.ToLower(command)
	if strings.Contains(lower, "coding-ethos-lint") ||
		strings.Contains(lower, "policy-tool") {
		return true
	}

	for _, tool := range toolcatalog.CapturedLintTools() {
		if commandMentionsToken(lower, strings.ToLower(tool.Name)) {
			return true
		}
	}

	return false
}

func commandMentionsToken(command string, token string) bool {
	return strings.Contains(" "+command+" ", " "+token+" ") ||
		strings.Contains(command, "/"+token+" ") ||
		strings.Contains(command, "/"+token+"\n")
}

func stableStringIndex(parts []string, candidateCount int) int {
	index := 0
	for _, char := range []byte(strings.Join(parts, "\x00")) {
		index = (index + int(char)) % candidateCount
	}

	return index
}

func minInt(first int, second int) int {
	if first < second {
		return first
	}

	return second
}
