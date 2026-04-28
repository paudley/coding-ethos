// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "strings"

func lifecycleOutput(event Event) *HookSpecificOutput {
	context := lifecycleContext(event)
	if context == "" {
		return nil
	}

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: hookOutputNormalizer(event.Cwd).preserveLines(context),
	}
}

func lifecycleContext(event Event) string {
	switch event.HookEventName {
	case "UserPromptSubmit":
		return buildLifecycleContext(
			"CODING-ETHOS PROMPT GUIDANCE",
			[]string{
				"Read relevant repo instructions before acting.",
				"Use and maintain a todo list for multi-step work.",
				"Define success criteria before running expensive or broad actions.",
				"Use the managed git wrapper and never bypass hook policy.",
			},
			event.Content(),
		)
	case "PostToolBatch":
		return buildLifecycleContext(
			"CODING-ETHOS TOOL BATCH CHECKPOINT",
			[]string{
				"Review tool results before continuing.",
				"Update the todo list to reflect completed and remaining work.",
				"Run focused verification after code edits and before broader gates.",
			},
			"",
		)
	case "Stop", "SessionEnd":
		return buildLifecycleContext(
			"CODING-ETHOS STOP CHECKPOINT",
			[]string{
				"Do not report completion while planned work remains.",
				"Summarize evidence: files changed, checks run, and unresolved risks.",
				"If hooks or lint failed, keep the failure visible and fix it structurally.",
			},
			"",
		)
	case "SubagentStart":
		return buildLifecycleContext(
			"CODING-ETHOS SUBAGENT START",
			[]string{
				"Keep delegated work scoped and concrete.",
				"Do not overwrite edits made by other agents or the user.",
				"Return changed files, verification, and any unresolved risks.",
			},
			event.Content(),
		)
	case "SubagentStop":
		return buildLifecycleContext(
			"CODING-ETHOS SUBAGENT COMPLETION",
			[]string{
				"Check the subagent result against the assigned scope.",
				"Integrate only verified changes and preserve unrelated user work.",
				"Record any remaining follow-up explicitly.",
			},
			"",
		)
	default:
		return ""
	}
}

func buildLifecycleContext(title string, guidance []string, prompt string) string {
	lines := []string{title, ""}
	if trimmed := strings.TrimSpace(prompt); trimmed != "" {
		lines = append(lines, "prompt:", trimmed, "")
	}

	lines = append(lines, "guidance:")
	for _, item := range guidance {
		lines = append(lines, "- "+item)
	}

	return strings.Join(lines, "\n")
}
