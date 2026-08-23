// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:tagliatelle // Provider hook output schemas use native camelCase names.
package hooks

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/feedback"
)

type providerHookOutput struct {
	HookSpecificOutput *HookSpecificOutput    `json:"hookSpecificOutput,omitempty"`
	Decision           string                 `json:"decision,omitempty"`
	Message            string                 `json:"message,omitempty"`
	Reason             string                 `json:"reason,omitempty"`
	SystemMessage      string                 `json:"systemMessage,omitempty"`
	TraceID            string                 `json:"traceId,omitempty"`
	TrackingID         string                 `json:"trackingID,omitempty"`
	AgentRemediation   []agentmsg.Remediation `json:"agent_remediation,omitempty"`
}

func EncodeProviderResult(writer io.Writer, result Result) error {
	output := providerOutput(result)
	if output.empty() {
		if neutralCodexPreToolOutput(result) {
			_, err := writer.Write([]byte("{}\n"))
			if err != nil {
				return fmt.Errorf("encode neutral Codex PreToolUse hook result: %w", err)
			}
		}

		return nil
	}

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(output)
	if err != nil {
		return fmt.Errorf("encode provider hook result: %w", err)
	}

	return nil
}

func neutralCodexPreToolOutput(result Result) bool {
	return result.Provider == providerCodex &&
		result.Event == eventPreToolUse &&
		!result.Blocked()
}

func (output providerHookOutput) empty() bool {
	return output.HookSpecificOutput == nil &&
		output.Decision == "" &&
		output.Message == "" &&
		output.Reason == "" &&
		output.SystemMessage == "" &&
		output.TraceID == "" &&
		output.TrackingID == "" &&
		len(output.AgentRemediation) == 0
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
	case providerKimi:
		return kimiAllowedOutput(result)
	default:
		return claudeAllowedOutput(result)
	}
}

func kimiAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
	if output.AdditionalContext == "" {
		return providerHookOutput{}
	}

	if output.HookEventName == eventStop {
		return providerHookOutput{
			Message: output.AdditionalContext,
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:            output.HookEventName,
				PermissionDecision:       "deny",
				PermissionDecisionReason: output.AdditionalContext,
			},
		}
	}

	return providerHookOutput{Message: output.AdditionalContext}
}

func claudeAllowedOutput(result Result) providerHookOutput {
	output := result.HookSpecificOutput
	if output.AdditionalContext == "" {
		return providerHookOutput{HookSpecificOutput: output}
	}

	switch output.HookEventName {
	case eventUserPromptSubmit, eventPostToolUse, eventPostToolBatch:
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
	case eventSessionStart, eventUserPromptSubmit, eventPostToolUse:
		return providerHookOutput{
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:     output.HookEventName,
				AdditionalContext: message,
			},
		}
	case eventStop:
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

	remediation := agentmsg.FromDecisions(
		blockingDecisions(result.Decisions),
		result.Tool,
	)
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
		if result.Event == eventPreToolUse {
			output.HookSpecificOutput = &HookSpecificOutput{
				HookEventName:            result.Event,
				PermissionDecision:       "deny",
				PermissionDecisionReason: message,
			}
		}

		return output
	case providerKimi:
		return providerHookOutput{
			Decision:         "deny",
			Message:          message,
			Reason:           message,
			TraceID:          result.TrackingID,
			TrackingID:       result.TrackingID,
			AgentRemediation: remediation,
			HookSpecificOutput: &HookSpecificOutput{
				HookEventName:            result.Event,
				PermissionDecision:       "deny",
				PermissionDecisionReason: message,
			},
		}
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
	blocking := blockingDecisions(result.Decisions)
	if len(blocking) == 0 {
		return feedback.MustRender(feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("event", result.Event),
				feedback.S("status", statusBlocked),
				feedback.S("summary", "coding-ethos blocked this action"),
			},
		}, feedback.FormatTOON)
	}

	scalars := []feedback.Scalar{
		feedback.S("event", result.Event),
		feedback.S("tool", result.Tool),
		feedback.S("status", result.Status),
	}
	if result.TrackingID != "" {
		scalars = append(scalars, feedback.S("trackingID", result.TrackingID))
	}

	rows := make([][]string, 0, len(blocking))
	for _, decision := range blocking {
		rows = append(rows, []string{
			decision.PolicyID,
			decision.Decision,
			decision.Severity,
			decision.Message,
			decision.Suggestion,
		})
	}

	return feedback.MustRender(feedback.Message{
		Scalars: scalars,
		Tables: []feedback.Table{feedback.T(
			"decisions",
			[]string{"policy_id", "decision", "severity", "message", "suggestion"},
			rows,
		)},
	}, feedback.FormatTOON)
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

	if output.HookEventName == eventSessionStart {
		return codexSessionStartAllowedMessage(context)
	}

	switch {
	case output.HookEventName == eventUserPromptSubmit:
		return feedback.MustRender(feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("event", eventUserPromptSubmit),
				feedback.S("status", "guidance"),
			},
			Tables: []feedback.Table{feedback.T(
				"guidance",
				[]string{"message"},
				[][]string{{"Use and maintain a todo list for multi-step work."}},
			)},
		}, feedback.FormatTOON)
	case output.HookEventName == eventStop:
		return feedback.MustRender(feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("event", eventStop),
				feedback.S("status", "guidance"),
			},
			Tables: []feedback.Table{feedback.T(
				"guidance",
				[]string{"message"},
				[][]string{
					{
						"Before ending, confirm planned work is complete, " +
							"summarize changed files and checks, and keep hook " +
							"or lint failures visible.",
					},
				},
			)},
		}, feedback.FormatTOON)
	case strings.Contains(normalized, "tool: Write") ||
		strings.Contains(normalized, "tool: Edit") ||
		strings.Contains(normalized, "tool: MultiEdit"):
		return "coding-ethos: review the edited file; run focused formatting, " +
			"lint, type, or tests; fix static-analysis findings structurally."
	case strings.Contains(normalized, "event: PostToolUse") &&
		strings.Contains(normalized, "tool: Bash"):
		return context
	default:
		return ""
	}
}

func codexSessionStartAllowedMessage(context string) string {
	if strings.HasPrefix(strings.TrimSpace(context), "event: SessionStart") {
		return context
	}

	return feedback.MustRender(feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("event", eventSessionStart),
			feedback.S("status", "guidance"),
		},
		Tables: []feedback.Table{feedback.T(
			"guidance",
			[]string{"message"},
			[][]string{
				{
					"Load repository conventions, managed toolchain rules, " +
						"and generated skills before editing.",
				},
			},
		)},
	}, feedback.FormatTOON)
}
