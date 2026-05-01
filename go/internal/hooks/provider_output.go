// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // Provider hook output schemas use native camelCase names.
package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

type providerHookOutput struct {
	HookSpecificOutput *HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
	Decision           string              `json:"decision,omitempty"`
	Reason             string              `json:"reason,omitempty"`
	SystemMessage      string              `json:"systemMessage,omitempty"`
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
	if output.AdditionalContext == "" {
		return providerHookOutput{}
	}

	message := codexAllowedMessage(output.AdditionalContext)
	if message == "" {
		return providerHookOutput{}
	}

	return providerHookOutput{SystemMessage: message}
}

func geminiAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
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
	switch result.Provider {
	case "gemini":
		return providerHookOutput{
			Decision:      "deny",
			Reason:        message,
			SystemMessage: message,
		}
	case "codex":
		message = compactProviderMessage(message)
		return providerHookOutput{
			Decision: "block",
			Reason:   message,
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:            result.Event,
				PermissionDecision:       "deny",
				PermissionDecisionReason: message,
			},
			SystemMessage: message,
		}
	default:
		return providerHookOutput{
			Decision: "block",
			Reason:   message,
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
	message := providerBlockReason(result)
	if result.Provider == providerCodex {
		return compactProviderMessage(message)
	}

	return message
}

func providerBlockReason(result Result) string {
	blocking := blockingDecisions(result.Decisions)
	if len(blocking) == 0 {
		return "Blocked by coding-ethos policy."
	}

	parts := make([]string, 0, len(blocking))
	if hasSevereViolation(blocking) {
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

func providerContextSummary(context string) string {
	if strings.TrimSpace(context) == "" {
		return ""
	}

	return "coding-ethos added hook context for this turn."
}

func codexAllowedMessage(context string) string {
	normalized := strings.Join(strings.Fields(context), " ")
	if normalized == "" {
		return ""
	}

	switch {
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
