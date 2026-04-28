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
		return providerHookOutput{HookSpecificOutput: result.HookSpecificOutput}
	}
}

func codexAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
	if output.AdditionalContext == "" {
		return providerHookOutput{}
	}

	return providerHookOutput{HookSpecificOutput: output}
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
	message := providerBlockReason(result)
	switch result.Provider {
	case "gemini":
		return providerHookOutput{
			Decision:      "deny",
			Reason:        message,
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

func providerBlockReason(result Result) string {
	blocking := blockingDecisions(result.Decisions)
	if len(blocking) == 0 {
		return "Blocked by coding-ethos policy."
	}

	parts := make([]string, 0, len(blocking))
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
