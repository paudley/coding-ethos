// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
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

var (
	errPostEditRuffFailed   = errors.New("ruff command failed")
	errPostEditRuffTimedOut = errors.New("ruff command timed out")
)

func postEditOutput(bundle policy.Bundle, event Event) *HookSpecificOutput {
	if event.HookEventName != eventPostToolUse || !isEditTool(event.ToolName) {
		return nil
	}

	files := event.Files()
	lintShieldState := postEditLintShieldState(event)
	lintState := postEditLintState(bundle, event)
	fastLintState := postEditFastLintState(bundle, event)

	lintHistory := postEditLintHistory(event)
	if event.Provider() == providerCodex &&
		!postEditHasActionableSignal(
			lintShieldState,
			lintState,
			fastLintState,
			lintHistory,
		) {
		return nil
	}

	context := buildPostEditContext(
		event.ToolName,
		files,
		bundle.Skills,
		postToolReminder(bundle, event),
		lintShieldState,
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
	lintShieldState postEditLintShieldResult,
	lintState postEditLintResult,
	fastLintState postEditLintResult,
	lintHistory postEditLintHistoryResult,
) bool {
	return lintShieldState.Error != "" ||
		postEditLintResultHasSignal(lintState) ||
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
	lintShieldState postEditLintShieldResult,
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

	lines = appendPostEditLintShield(lines, lintShieldState)
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
		"- Fix static-analysis findings structurally; do not weaken policy "+
			"or add broad suppressions.",
		"- Keep the todo list current if more work remains.",
	)

	return strings.Join(lines, "\n")
}

func postEditLanguageAdvice(files []string) []string {
	languages := map[string]bool{}

	for _, file := range files {
		addPostEditFileLanguages(languages, file)
	}

	advice := []string{}
	advice = appendLanguageAdvice(advice, languages, "python", pythonPostEditAdvice)
	advice = appendLanguageAdvice(advice, languages, "go", goPostEditAdvice)
	advice = appendLanguageAdvice(advice, languages, "shell", shellPostEditAdvice)
	advice = appendWorkflowOrYAMLAdvice(advice, languages)
	advice = appendLanguageAdvice(advice, languages, "markdown", markdownPostEditAdvice)

	return advice
}

const (
	pythonPostEditAdvice = "- python: run ruff/mypy/pyright or a focused " +
		"pytest target before claiming the edit is complete."
	goPostEditAdvice = "- go: run gofmt plus focused go test/golangci-lint " +
		"for the touched package."
	shellPostEditAdvice = "- shell: run shellcheck and verify quoting/fail-fast " +
		"behavior."
	githubActionsPostEditAdvice = "- github-actions: run actionlint " +
		"for workflow changes."
	yamlPostEditAdvice     = "- yaml: run yamllint or the repo-specific YAML validator."
	markdownPostEditAdvice = "- markdown: if the file is generated, update the " +
		"source config and regenerate instead of hand-editing."
)

func addPostEditFileLanguages(languages map[string]bool, file string) {
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

func appendLanguageAdvice(
	advice []string,
	languages map[string]bool,
	language string,
	message string,
) []string {
	if languages[language] {
		return append(advice, message)
	}

	return advice
}

func appendWorkflowOrYAMLAdvice(advice []string, languages map[string]bool) []string {
	if languages["github_actions"] {
		return append(advice, githubActionsPostEditAdvice)
	}

	return appendLanguageAdvice(advice, languages, "yaml", yamlPostEditAdvice)
}

type postEditLintResult struct {
	Error       string
	Status      string
	Diagnostics []diagnostics.Diagnostic
	Checked     bool
}

type postEditLintShieldResult struct {
	Error        string
	Status       string
	ChangedFiles []string
	Checked      bool
}

type postEditLintHistoryResult struct {
	Analysis lint.Analysis
	Checked  bool
}

type postEditFileSnapshot struct {
	Hash  [sha256.Size]byte
	Found bool
}

func postEditLintShieldState(event Event) postEditLintShieldResult {
	files := existingPythonPostEditFiles(event.Cwd, event.Files())
	if len(files) == 0 {
		return postEditLintShieldResult{}
	}

	snapshots := postEditFileSnapshots(event.Cwd, files)
	for _, args := range postEditLintShieldCommands(files) {
		err := runPostEditRuff(event.Cwd, args)
		if err != nil {
			if errors.Is(err, exec.ErrNotFound) {
				return postEditLintShieldResult{}
			}

			return postEditLintShieldResult{
				Checked: true,
				Status:  statusBlocked,
				Error:   err.Error(),
			}
		}
	}

	changed := postEditChangedFiles(event.Cwd, files, snapshots)
	if len(changed) == 0 {
		return postEditLintShieldResult{
			Checked: true,
			Status:  statusAllowed,
		}
	}

	return postEditLintShieldResult{
		Checked:      true,
		Status:       "applied",
		ChangedFiles: changed,
	}
}

func postEditLintShieldCommands(files []string) [][]string {
	return [][]string{
		append([]string{"format", "--quiet"}, files...),
		append(
			[]string{
				"check",
				"--fix",
				"--quiet",
				"--ignore-noqa",
				"--output-format",
				"json",
			},
			files...,
		),
	}
}

func runPostEditRuff(cwd string, args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fastPostEditTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "ruff", args...)
	command.Dir = cwd

	output, err := command.CombinedOutput()
	if err == nil {
		return nil
	}

	if ctx.Err() != nil {
		return fmt.Errorf("%w: %s: %w", errPostEditRuffTimedOut, args[0], ctx.Err())
	}

	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("run ruff: %w", err)
	}

	if len(diagnostics.Parse("ruff", string(output), "")) > 0 {
		return nil
	}

	return fmt.Errorf("%w: %s: %w", errPostEditRuffFailed, args[0], err)
}

func postEditFileSnapshots(
	cwd string,
	files []string,
) map[string]postEditFileSnapshot {
	snapshots := make(map[string]postEditFileSnapshot, len(files))
	for _, file := range files {
		content, err := os.ReadFile(postEditFilePath(cwd, file))
		if err != nil {
			continue
		}

		snapshots[file] = postEditFileSnapshot{
			Hash:  sha256.Sum256(content),
			Found: true,
		}
	}

	return snapshots
}

func postEditChangedFiles(
	cwd string,
	files []string,
	snapshots map[string]postEditFileSnapshot,
) []string {
	changed := []string{}

	for _, file := range files {
		before, ok := snapshots[file]
		if !ok || !before.Found {
			continue
		}

		content, err := os.ReadFile(postEditFilePath(cwd, file))
		if err != nil {
			continue
		}

		if sha256.Sum256(content) != before.Hash {
			changed = append(changed, file)
		}
	}

	return changed
}

func postEditFilePath(cwd, file string) string {
	if cwd != "" && !filepath.IsAbs(file) {
		return filepath.Join(cwd, file)
	}

	return file
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
	files := make([]string, 0, len(event.Files()))

	for _, file := range event.Files() {
		item := file
		if event.Cwd != "" && filepath.IsAbs(item) {
			relative, inlineErrAutoA := filepath.Rel(event.Cwd, item)
			if inlineErrAutoA == nil &&
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
	files := existingPythonPostEditFiles(event.Cwd, event.Files())
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

		return postEditLintResult{
			Checked:     true,
			Status:      statusBlocked,
			Diagnostics: parsed,
		}
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

func existingPythonPostEditFiles(cwd string, files []string) []string {
	existing := []string{}

	for _, file := range pythonPostEditFiles(files) {
		path := file
		if cwd != "" && !filepath.IsAbs(path) {
			path = filepath.Join(cwd, path)
		}

		info, err := os.Stat(path)
		if err != nil || info.IsDir() {
			continue
		}

		existing = append(existing, file)
	}

	return existing
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
		lines = append(
			lines,
			"- recurring_checks: "+postEditCountsLine(analysis.TopChecks),
		)
	}

	if len(analysis.TopCodes) > 0 {
		lines = append(
			lines,
			"- recurring_tool_codes: "+postEditCountsLine(analysis.TopCodes),
		)
	}

	if len(analysis.UnmappedCodes) > 0 {
		lines = append(
			lines,
			"- unmapped_tool_codes: "+postEditCountsLine(analysis.UnmappedCodes),
		)
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

func appendPostEditLintShield(
	lines []string,
	state postEditLintShieldResult,
) []string {
	if !state.Checked {
		return lines
	}

	lines = append(lines, "", "lint_shield:", "- tool: ruff", "- status: "+state.Status)
	if state.Error != "" {
		return append(lines, "- detail: "+state.Error)
	}

	if len(state.ChangedFiles) == 0 {
		return append(lines, "- changed_files: none")
	}

	lines = append(lines, "changed_files:")
	for _, file := range state.ChangedFiles {
		lines = append(lines, "- "+file)
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
		return append(
			lines,
			"- status: error",
			"- tool: ruff",
			"- detail: "+state.Error,
		)
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
