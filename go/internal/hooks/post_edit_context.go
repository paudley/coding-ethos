// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const maxPostEditFindings = 8
const fastPostEditTimeout = 5 * time.Second

func postEditOutput(bundle policy.Bundle, event Event) *HookSpecificOutput {
	if event.HookEventName != "PostToolUse" || !isEditTool(event.ToolName) {
		return nil
	}

	files := event.Files()
	context := buildPostEditContext(
		event.ToolName,
		files,
		postEditLintState(bundle, event),
		postEditFastLintState(event),
	)

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
	fastLintState postEditLintResult,
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
	lines = appendPostEditFastLintState(lines, fastLintState)

	if advice := postEditLanguageAdvice(files); len(advice) > 0 {
		lines = append(lines, "", "language_advice:")
		lines = append(lines, advice...)
	}

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

func postEditLanguageAdvice(files []string) []string {
	languages := map[string]bool{}
	for _, file := range files {
		normalized := filepath.ToSlash(file)
		switch strings.ToLower(filepath.Ext(normalized)) {
		case ".py":
			languages["python"] = true
		case ".go":
			languages["go"] = true
		case ".sh", ".bash":
			languages["shell"] = true
		case ".yaml", ".yml":
			languages["yaml"] = true
		case ".md":
			languages["markdown"] = true
		}
		if strings.HasPrefix(normalized, ".github/workflows/") {
			languages["github_actions"] = true
		}
	}

	advice := []string{}
	if languages["python"] {
		advice = append(
			advice,
			"- python: run ruff/mypy/pyright or a focused pytest target before claiming the edit is complete.",
		)
	}
	if languages["go"] {
		advice = append(
			advice,
			"- go: run gofmt plus focused go test/golangci-lint for the touched package.",
		)
	}
	if languages["shell"] {
		advice = append(advice, "- shell: run shellcheck and verify quoting/fail-fast behavior.")
	}
	if languages["github_actions"] {
		advice = append(advice, "- github-actions: run actionlint for workflow changes.")
	} else if languages["yaml"] {
		advice = append(advice, "- yaml: run yamllint or the repo-specific YAML validator.")
	}
	if languages["markdown"] {
		advice = append(
			advice,
			"- markdown: if the file is generated, update the source config and regenerate instead of hand-editing.",
		)
	}

	return advice
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

func postEditFastLintState(event Event) postEditLintResult {
	files := pythonPostEditFiles(event.Files())
	if len(files) == 0 {
		return postEditLintResult{}
	}

	ctx, cancel := context.WithTimeout(context.Background(), fastPostEditTimeout)
	defer cancel()

	args := append(
		[]string{"check", "--quiet", "--ignore-noqa", "--output-format", "json"},
		files...,
	)
	command := exec.CommandContext(ctx, "ruff", args...)
	command.Dir = event.Cwd

	output, err := command.CombinedOutput()
	parsed := diagnostics.Parse("ruff", string(output), "")
	if len(parsed) > 0 {
		return postEditLintResult{Checked: true, Status: statusBlocked, Diagnostics: parsed}
	}
	if err != nil {
		if ctx.Err() != nil {
			return postEditLintResult{Checked: true, Error: "ruff timed out"}
		}

		return postEditLintResult{}
	}

	return postEditLintResult{Checked: true, Status: statusAllowed}
}

func pythonPostEditFiles(files []string) []string {
	pythonFiles := []string{}
	for _, file := range files {
		if strings.EqualFold(filepath.Ext(file), ".py") {
			pythonFiles = append(pythonFiles, file)
		}
	}

	return pythonFiles
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

func appendPostEditFastLintState(
	lines []string,
	state postEditLintResult,
) []string {
	if !state.Checked {
		return lines
	}

	lines = append(lines, "", "fast_lint:")
	if state.Error != "" {
		return append(lines, "- status: error", "- tool: ruff", "- detail: "+state.Error)
	}

	lines = append(lines, "- tool: ruff", "- status: "+state.Status)
	if len(state.Diagnostics) == 0 {
		return append(lines, "- findings: none")
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
