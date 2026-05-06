// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // Provider hook output schemas use native camelCase names.
package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

type providerHookOutput struct {
	HookSpecificOutput *HookSpecificOutput    `json:"hookSpecificOutput,omitempty"`
	Decision           string                 `json:"decision,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	SystemMessage      string                 `json:"systemMessage,omitempty"`
	TraceID            string                 `json:"traceId,omitempty"`
	TrackingID         string                 `json:"trackingID,omitempty"`
	AgentRemediation   []agentmsg.Remediation `json:"agent_remediation,omitempty"`
}

func EncodeProviderResult(writer io.Writer, result Result) error {
	output := providerOutput(result)
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(output)
	if err != nil {
		return fmt.Errorf("encode provider hook result: %w", err)
	}

	return nil
}

func providerOutput(result Result) providerHookOutput {
	if result.Blocked() {
		return providerBlockedOutput(result)
	}

	if result.HookSpecificOutput == nil {
		return providerHookOutput{}
	}

	switch result.Provider {
	case "codex":
		return codexAllowedOutput(result)
	case "gemini":
		return geminiAllowedOutput(result)
	default:
		return claudeAllowedOutput(result)
	}
}

func claudeAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
	if output.AdditionalContext == "" {
		return providerHookOutput{HookSpecificOutput: output}
	}

	switch output.HookEventName {
	case "UserPromptSubmit", "PostToolUse", "PostToolBatch":
		return providerHookOutput{HookSpecificOutput: output}
	default:
		return providerHookOutput{
			SystemMessage: output.AdditionalContext,
		}
	}
}

func codexAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
	if len(output.UpdatedInput) > 0 {
		return providerHookOutput{
			HookSpecificOutput: output,
		}
	}

	if output.AdditionalContext == "" {
		return providerHookOutput{}
	}

	message := codexAllowedMessage(output)
	if message == "" {
		return providerHookOutput{}
	}

	switch output.HookEventName {
	case "SessionStart", "UserPromptSubmit", "PostToolUse":
		return providerHookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:     output.HookEventName,
				AdditionalContext: message,
			},
		}
	case "Stop":
		return providerHookOutput{SystemMessage: message}
	default:
		return providerHookOutput{}
	}
}

func geminiAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
	if len(output.UpdatedInput) > 0 {
		return providerHookOutput{
			Decision:           "allow",
			HookSpecificOutput: output,
		}
	}

	if output.AdditionalContext == "" {
		return providerHookOutput{Decision: "allow"}
	}

	return providerHookOutput{
		Decision:           "allow",
		HookSpecificOutput: output,
		SystemMessage:      providerContextSummary(output.AdditionalContext),
	}
}

func providerBlockedOutput(result Result) providerHookOutput {
	message := ProviderBlockMessage(result)

	remediation := agentmsg.FromDecisions(blockingDecisions(result.Decisions), result.Tool)
	switch result.Provider {
	case "gemini":
		return providerHookOutput{
			Decision:         "deny",
			Reason:           message,
			SystemMessage:    message,
			TraceID:          result.TrackingID,
			TrackingID:       result.TrackingID,
			AgentRemediation: remediation,
		}
	case "codex":
		output := providerHookOutput{
			Decision:         "block",
			Reason:           message,
			TraceID:          result.TrackingID,
			TrackingID:       result.TrackingID,
			AgentRemediation: remediation,
		}
		if result.Event == "PreToolUse" {
			output.HookSpecificOutput = &HookSpecificOutput{
				HookEventName:            result.Event,
				PermissionDecision:       "deny",
				PermissionDecisionReason: message,
			}
		}

		return output
	default:
		return providerHookOutput{
			Decision:         "block",
			Reason:           message,
			TraceID:          result.TrackingID,
			TrackingID:       result.TrackingID,
			AgentRemediation: remediation,
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:            result.Event,
				PermissionDecision:       "deny",
				PermissionDecisionReason: message,
			},
			SystemMessage: message,
		}
	}
}

func ProviderBlockMessage(result Result) string {
	if result.Provider == providerCodex {
		return withTrackingID(result, codexBlockMessage(result))
	}

	message := providerBlockReason(result)

	return withTrackingID(result, message)
}

func withTrackingID(result Result, message string) string {
	if result.TrackingID == "" {
		return message
	}

	return "trackingID: " + result.TrackingID + ". " + message
}

func codexBlockMessage(result Result) string {
	blocking := blockingDecisions(result.Decisions)
	if len(blocking) == 0 {
		return "coding-ethos blocked this action."
	}

	policyIDs := make([]string, 0, len(blocking))
	for _, decision := range blocking {
		if decision.PolicyID != "" {
			policyIDs = append(policyIDs, decision.PolicyID)
		}
	}

	prefix := "coding-ethos blocked this action"
	if len(policyIDs) > 0 {
		prefix += " (" + strings.Join(policyIDs, ", ") + ")"
	}

	parts := make([]string, 0, 3)

	parts = append(parts, prefix+".")
	if hasSevereViolation(blocking) && !decisionsContainSevereWarning(blocking) {
		parts = append(parts, severeViolationWarning)
	}

	decision := blocking[0]

	reason := decision.Message
	if decision.Suggestion != "" && !strings.Contains(reason, decision.Suggestion) {
		reason = sentence(reason, decision.Suggestion)
	}

	if reason != "" {
		parts = append(parts, reason)
	}

	return compactProviderMessage(strings.Join(parts, " "))
}

func providerBlockReason(result Result) string {
	blocking := blockingDecisions(result.Decisions)
	if len(blocking) == 0 {
		return "Blocked by coding-ethos policy."
	}

	parts := make([]string, 0, len(blocking))
	if hasSevereViolation(blocking) && !decisionsContainSevereWarning(blocking) {
		parts = append(parts, severeViolationWarning)
	}

	for _, decision := range blocking {
		part := decision.Message
		if decision.Suggestion != "" && !strings.Contains(part, decision.Suggestion) {
			part = sentence(part, decision.Suggestion)
		}

		parts = append(parts, part)
	}

	return strings.Join(parts, "\n")
}

func decisionsContainSevereWarning(decisions []policy.Decision) bool {
	for _, decision := range decisions {
		if strings.Contains(decision.Message, severeViolationWarning) {
			return true
		}
	}

	return false
}

func providerContextSummary(context string) string {
	if strings.TrimSpace(context) == "" {
		return ""
	}

	return "coding-ethos added hook context for this turn."
}

func codexAllowedMessage(output *HookSpecificOutput) string {
	context := output.AdditionalContext

	normalized := strings.Join(strings.Fields(context), " ")
	if normalized == "" {
		return ""
	}

	switch {
	case output.HookEventName == "SessionStart":
		return "coding-ethos: load repository conventions, managed toolchain rules, and generated skills before editing."
	case output.HookEventName == "UserPromptSubmit":
		return "coding-ethos: use and maintain a todo list for multi-step work."
	case output.HookEventName == "Stop":
		return "coding-ethos: before ending, confirm planned work is complete, summarize changed files and checks, and keep hook or lint failures visible."
	case strings.Contains(normalized, "tool: Write") ||
		strings.Contains(normalized, "tool: Edit") ||
		strings.Contains(normalized, "tool: MultiEdit"):
		return "coding-ethos: review the edited file; run focused formatting, lint, type, or tests; fix static-analysis findings structurally."
	case strings.Contains(normalized, "event: PostToolUse") &&
		strings.Contains(normalized, "tool: Bash"):
		return "coding-ethos: hook output captured; summarize failed hooks, modified files, warnings, and required fixes before continuing."
	default:
		return ""
	}
}

func compactProviderMessage(message string) string {
	return strings.Join(strings.Fields(message), " ")
}
