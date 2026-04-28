// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const maxPostEditFindings = 8

func postEditOutput(bundle policy.Bundle, event Event) *HookSpecificOutput {
	if event.HookEventName != "PostToolUse" || !isEditTool(event.ToolName) {
		return nil
	}

	files := event.Files()
	context := buildPostEditContext(event.ToolName, files, postEditLintState(bundle, event))

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: hookOutputNormalizer(event.Cwd).preserveLines(context),
	}
}

func isEditTool(tool string) bool {
	switch tool {
	case "Edit", "MultiEdit", "Write":
		return true
	default:
		return false
	}
}

func buildPostEditContext(
	tool string,
	files []string,
	lintState postEditLintResult,
) string {
	lines := []string{
		"CODING-ETHOS POST-EDIT CHECKPOINT",
		"tool: " + tool,
	}

	if len(files) > 0 {
		lines = append(lines, "files:")
		for _, file := range files {
			lines = append(lines, "- "+file)
		}
	}

	lines = appendPostEditLintState(lines, lintState)

	lines = append(
		lines,
		"",
		"guidance:",
		"- Review the edited file before claiming completion.",
		"- Run focused formatting, lint, type, or tests appropriate to the changed file.",
		"- Fix static-analysis findings structurally; do not weaken policy or add broad suppressions.",
		"- Keep the todo list current if more work remains.",
	)

	return strings.Join(lines, "\n")
}

type postEditLintResult struct {
	Diagnostics []diagnostics.Diagnostic
	Error       string
	Status      string
	Checked     bool
}

func postEditLintState(bundle policy.Bundle, event Event) postEditLintResult {
	files := event.Files()
	if len(files) == 0 {
		return postEditLintResult{}
	}

	result, err := lint.Run(bundle, lint.Options{
		Scope: lint.ScopeFiles,
		Files: files,
		Cwd:   event.Cwd,
	})
	if err != nil {
		return postEditLintResult{Checked: true, Error: err.Error()}
	}

	return postEditLintResult{
		Checked:     true,
		Status:      result.Status,
		Diagnostics: result.Diagnostics,
	}
}

func appendPostEditLintState(
	lines []string,
	state postEditLintResult,
) []string {
	if !state.Checked {
		return lines
	}

	lines = append(lines, "", "compiled_lint:")
	if state.Error != "" {
		return append(lines, "- status: error", "- detail: "+state.Error)
	}

	lines = append(lines, "- status: "+state.Status)
	if len(state.Diagnostics) == 0 {
		return append(lines, "- findings: none from compiled file-scope policies")
	}

	lines = append(
		lines,
		fmt.Sprintf("- findings: %d", len(state.Diagnostics)),
		"findings:",
	)
	for idx, item := range state.Diagnostics {
		if idx >= maxPostEditFindings {
			lines = append(
				lines,
				fmt.Sprintf("- ... %d more findings", len(state.Diagnostics)-idx),
			)

			break
		}

		lines = append(lines, "- "+postEditFindingLine(item))
	}

	return lines
}

func postEditFindingLine(item diagnostics.Diagnostic) string {
	location := item.File
	if item.Line > 0 {
		location += fmt.Sprintf(":%d", item.Line)
		if item.Column > 0 {
			location += fmt.Sprintf(":%d", item.Column)
		}
	}
	if location == "" {
		location = "<unknown>"
	}

	code := item.Code
	if code == "" {
		code = item.PolicyID
	}
	if code != "" {
		code = " [" + code + "]"
	}

	advice := item.Advice
	if advice != "" {
		advice = " advice: " + advice
	}

	return location + code + " " + item.Message + advice
}
