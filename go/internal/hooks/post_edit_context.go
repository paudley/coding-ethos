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

const (
	maxPostEditFindings        = 8
	maxPostEditHistoryCounts   = 3
	maxPostEditHistoryGuidance = 2
	fastPostEditTimeout        = 5 * time.Second
)

func postEditOutput(bundle policy.Bundle, event Event) *HookSpecificOutput {
	if event.HookEventName != "PostToolUse" || !isEditTool(event.ToolName) {
		return nil
	}

	files := event.Files()
	lintState := postEditLintState(bundle, event)
	fastLintState := postEditFastLintState(bundle, event)
	lintHistory := postEditLintHistory(event)
	if event.Provider() == providerCodex &&
		!postEditHasActionableSignal(lintState, fastLintState, lintHistory) {
		return nil
	}

	context := buildPostEditContext(
		event.ToolName,
		files,
		bundle.Skills,
		postToolReminder(bundle, event),
		lintState,
		fastLintState,
		lintHistory,
	)

	return &HookSpecificOutput{
		HookEventName:     event.HookEventName,
		AdditionalContext: hookOutputNormalizer(event.Cwd).preserveLines(context),
	}
}

func postEditHasActionableSignal(
	lintState postEditLintResult,
	fastLintState postEditLintResult,
	lintHistory postEditLintHistoryResult,
) bool {
	return postEditLintResultHasSignal(lintState) ||
		postEditLintResultHasSignal(fastLintState) ||
		lintHistory.Checked
}

func postEditLintResultHasSignal(result postEditLintResult) bool {
	return result.Error != "" || len(result.Diagnostics) > 0 ||
		result.Status == statusBlocked
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
	skills map[string]policy.Skill,
	reminders []renderedEthosReminder,
	lintState postEditLintResult,
	fastLintState postEditLintResult,
	lintHistory postEditLintHistoryResult,
) string {
	lines := []string{
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
	lines = appendPostEditLintHistory(lines, lintHistory)
	lines = appendPostEditSkillAdvice(lines, skills, lintState, fastLintState)
	lines = appendRenderedReminders(lines, reminders)

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

type postEditLintHistoryResult struct {
	Analysis lint.Analysis
	Checked  bool
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

func postEditLintHistory(event Event) postEditLintHistoryResult {
	files := postEditHistoryFiles(event)
	if len(files) == 0 {
		return postEditLintHistoryResult{}
	}

	traceDir, err := lint.DefaultTraceDir(event.Cwd)
	if err != nil {
		return postEditLintHistoryResult{}
	}

	analysis, err := lint.AnalyzeTracesWithOptions(traceDir, lint.AnalysisOptions{
		Files:                 files,
		MaxCounts:             maxPostEditHistoryCounts,
		MaxGuidanceCandidates: maxPostEditHistoryGuidance,
	})
	if err != nil || analysis.Findings == 0 {
		return postEditLintHistoryResult{}
	}

	return postEditLintHistoryResult{Analysis: analysis, Checked: true}
}

func postEditHistoryFiles(event Event) []string {
	files := []string{}
	for _, file := range event.Files() {
		item := file
		if event.Cwd != "" && filepath.IsAbs(item) {
			if relative, err := filepath.Rel(event.Cwd, item); err == nil &&
				relative != ".." &&
				!strings.HasPrefix(filepath.ToSlash(relative), "../") {
				item = relative
			}
		}
		files = append(files, item)
	}

	return files
}

func postEditFastLintState(bundle policy.Bundle, event Event) postEditLintResult {
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
		parsed = diagnostics.Enrich(parsed, bundle.EvidenceMaps)
		parsed = diagnostics.Dedupe(parsed)
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

func appendPostEditLintHistory(
	lines []string,
	history postEditLintHistoryResult,
) []string {
	if !history.Checked {
		return lines
	}

	analysis := history.Analysis
	lines = append(
		lines,
		"",
		"lint_history:",
		fmt.Sprintf("- findings: %d from prior captured runs", analysis.Findings),
	)
	if len(analysis.TopChecks) > 0 {
		lines = append(lines, "- recurring_checks: "+postEditCountsLine(analysis.TopChecks))
	}
	if len(analysis.TopCodes) > 0 {
		lines = append(lines, "- recurring_tool_codes: "+postEditCountsLine(analysis.TopCodes))
	}
	if len(analysis.UnmappedCodes) > 0 {
		lines = append(lines, "- unmapped_tool_codes: "+postEditCountsLine(analysis.UnmappedCodes))
	}
	if len(analysis.GuidanceCandidates) == 0 {
		return lines
	}

	lines = append(lines, "guidance_candidates:")
	for _, candidate := range analysis.GuidanceCandidates {
		lines = append(lines, "- "+postEditGuidanceCandidateLine(candidate))
	}

	return lines
}

func appendPostEditSkillAdvice(
	lines []string,
	skills map[string]policy.Skill,
	states ...postEditLintResult,
) []string {
	diagnostics := []diagnostics.Diagnostic{}
	for _, state := range states {
		diagnostics = append(diagnostics, state.Diagnostics...)
	}
	hints := lint.SkillHintsForDiagnostics(diagnostics, skills)
	if len(hints) == 0 {
		return lines
	}

	lines = append(lines, "", "skill_advice:")
	for _, hint := range hints {
		lines = append(
			lines,
			"- "+hint.SkillID+": "+hint.Message+" Next: "+hint.Next,
		)
	}

	return lines
}

func postEditCountsLine(counts []lint.Count) string {
	items := make([]string, 0, len(counts))
	for _, count := range counts {
		items = append(items, fmt.Sprintf("%s=%d", count.Key, count.Count))
	}

	return strings.Join(items, ", ")
}

func postEditGuidanceCandidateLine(candidate lint.GuidanceCandidate) string {
	code := candidate.Code
	if code == "" {
		code = candidate.CheckID
	}
	if code != "" {
		code = " [" + code + "]"
	}

	advice := candidate.Advice
	if advice != "" {
		advice = " advice: " + advice
	}

	return fmt.Sprintf(
		"%s%s count=%d: %s%s",
		candidate.CheckID,
		code,
		candidate.Count,
		candidate.Message,
		advice,
	)
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
	skill := item.SkillID
	if skill != "" {
		skill = " skill: " + skill
	}

	return location + code + " " + item.Message + advice + skill
}
