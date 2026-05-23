// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/pelletier/go-toml/v2"

	diag "blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const typeCheckTOONSummaryRows = 7

type typeCheckerConfig struct {
	Name                 string
	Parser               string
	RepoConfig           string
	FallbackBundleConfig string
	Command              []string
	ConfigFlags          []string
	PassFilesAsArgs      bool
	UseHookProject       bool
	Enabled              bool
}

type typeCheckSettings struct {
	BundleRoot            string
	ConsumerRoot          string
	HooksProject          string
	Checkers              []typeCheckerConfig
	EvidenceMaps          []diag.EvidenceMap
	Skills                map[string]hookSkill
	ExcludedPathFragments []string
	Enabled               bool
}

type typeCheckResult struct {
	Name        string            `json:"name"`
	Output      string            `json:"output,omitempty"`
	Diagnostics []diag.Diagnostic `json:"diagnostics,omitempty"`
	ExitCode    int               `json:"exit_code"`
	DurationMS  float64           `json:"duration_ms"`
}

type typeCheckSummary struct {
	Format    string            `json:"format"`
	Status    string            `json:"status"`
	Results   []typeCheckResult `json:"results"`
	FileCount int               `json:"file_count"`
	Passed    int               `json:"passed"`
	Failed    int               `json:"failed"`
	Duration  float64           `json:"duration_ms"`
}

func defaultTypeCheckers() []typeCheckerConfig {
	tools := toolcatalog.PythonStaticTools()

	checkers := make([]typeCheckerConfig, 0, len(tools))
	for _, tool := range tools {
		checkers = append(checkers, typeCheckerFromCatalog(tool))
	}

	return checkers
}

func typeCheckerFromCatalog(tool toolcatalog.Tool) typeCheckerConfig {
	return typeCheckerConfig{
		Name:                 tool.Name,
		Parser:               tool.Parser,
		Command:              append([]string(nil), tool.Command...),
		PassFilesAsArgs:      tool.PassFilesAsArgs,
		UseHookProject:       tool.UseHookProject,
		ConfigFlags:          append([]string(nil), tool.ConfigFlags...),
		RepoConfig:           tool.RepoConfig,
		FallbackBundleConfig: tool.FallbackBundleConfig,
		Enabled:              tool.EnabledByDefault,
	}
}

func loadTypeCheckSettings() (typeCheckSettings, error) {
	var settings typeCheckSettings

	bundleRoot, consumer, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}

	err = decodeConfigSection(rootConfig, "python.type_check", &settings)
	if err != nil {
		return settings, fmt.Errorf("parse type_check config: %w", err)
	}

	settings.BundleRoot = bundleRoot
	settings.ConsumerRoot = consumer

	settings.HooksProject = filepath.Join(bundleRoot, "hooks")
	if len(settings.Checkers) == 0 {
		settings.Checkers = defaultTypeCheckers()
	}

	if len(settings.ExcludedPathFragments) == 0 &&
		!configSectionFieldPresent(
			rootConfig,
			"python.type_check",
			"excluded_path_fragments",
		) {
		settings.ExcludedPathFragments = []string{"/docker/", "vulture_whitelist"}
	}

	settings.EvidenceMaps = loadHookEvidenceMaps()
	settings.Skills = loadHookSkills()

	for checkerIndex := range settings.Checkers {
		applyTypeCheckerDefaults(&settings.Checkers[checkerIndex], rootConfig)
	}

	return settings, nil
}

func applyTypeCheckerDefaults(
	checker *typeCheckerConfig,
	rootConfig map[string]any,
) {
	defaultChecker, hasDefault := defaultTypeCheckerByName(checker.Name)
	if checker.Parser == "" && hasDefault {
		checker.Parser = defaultChecker.Parser
	}

	if len(checker.Command) == 0 && hasDefault {
		checker.Command = append([]string{}, defaultChecker.Command...)
	}

	applyTypeCheckerBooleanDefaults(checker, rootConfig)
	applyTypeCheckerConfigDefaults(checker, defaultChecker, hasDefault)
	applyTypeCheckerEnabledDefault(checker, rootConfig, defaultChecker, hasDefault)
}

func applyTypeCheckerBooleanDefaults(
	checker *typeCheckerConfig,
	rootConfig map[string]any,
) {
	if shouldDefaultTypeCheckerField(
		*checker,
		rootConfig,
		"pass_files_as_args",
	) {
		checker.PassFilesAsArgs = true
	}

	if shouldDefaultTypeCheckerField(*checker, rootConfig, "use_hook_project") {
		checker.UseHookProject = true
	}
}

func applyTypeCheckerConfigDefaults(
	checker *typeCheckerConfig,
	defaultChecker typeCheckerConfig,
	hasDefault bool,
) {
	if len(checker.ConfigFlags) != 0 || !hasDefault {
		return
	}

	checker.ConfigFlags = append([]string{}, defaultChecker.ConfigFlags...)
	if checker.RepoConfig == "" {
		checker.RepoConfig = defaultChecker.RepoConfig
	}

	if checker.FallbackBundleConfig == "" {
		checker.FallbackBundleConfig = defaultChecker.FallbackBundleConfig
	}
}

func applyTypeCheckerEnabledDefault(
	checker *typeCheckerConfig,
	rootConfig map[string]any,
	defaultChecker typeCheckerConfig,
	hasDefault bool,
) {
	if !fieldPresentInTypeCheckerConfig(rootConfig, checker.Name, "enabled") {
		checker.Enabled = true
		if hasDefault {
			checker.Enabled = defaultChecker.Enabled
		}
	}
}

func shouldDefaultTypeCheckerField(
	checker typeCheckerConfig,
	rootConfig map[string]any,
	field string,
) bool {
	switch field {
	case "pass_files_as_args":
		return checker.PassFilesAsArgs &&
			!fieldPresentInTypeCheckerConfig(rootConfig, checker.Name, field)
	case "use_hook_project":
		return !checker.UseHookProject &&
			!fieldPresentInTypeCheckerConfig(rootConfig, checker.Name, field)
	default:
		return false
	}
}

func defaultTypeCheckerByName(name string) (typeCheckerConfig, bool) {
	for _, candidate := range defaultTypeCheckers() {
		if candidate.Name == name {
			return candidate, true
		}
	}

	return typeCheckerConfig{}, false
}

func fieldPresentInTypeCheckerConfig(
	rootConfig map[string]any,
	name string,
	field string,
) bool {
	value, found := rootConfigValue(rootConfig, "python.type_check.checkers")
	if !found {
		return false
	}

	items, isList := value.([]any)
	if !isList {
		return false
	}

	for _, item := range items {
		mapping, ok := item.(map[string]any)
		if !ok {
			continue
		}

		if strings.TrimSpace(fmt.Sprint(mapping["name"])) != name {
			continue
		}

		_, present := mapping[field]

		return present
	}

	return false
}

func typeCheckConfigPath(root, name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}

	path := filepath.Join(root, name)

	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		return path
	}

	return ""
}

func commandHasAnyOption(command, options []string) bool {
	for _, token := range command {
		for _, option := range options {
			if token == option || strings.HasPrefix(token, option+"=") {
				return true
			}
		}
	}

	return false
}

func resolveTypeCheckerCommand(
	checker typeCheckerConfig,
	settings typeCheckSettings,
) []string {
	command := append([]string{}, checker.Command...)

	command = appendTypeCheckerConfig(command, checker, settings)
	if checker.UseHookProject {
		projectRoot := preferredTypeCheckerProjectRoot(settings)
		command = append(
			[]string{"uv", "run", "--quiet", "--project", projectRoot},
			command...)
	}

	return command
}

func preferredTypeCheckerProjectRoot(settings typeCheckSettings) string {
	if consumerWorkspaceIncludesHooksProject(settings) {
		return settings.ConsumerRoot
	}

	return settings.HooksProject
}

func consumerWorkspaceIncludesHooksProject(settings typeCheckSettings) bool {
	pyprojectPath := filepath.Join(settings.ConsumerRoot, "pyproject.toml")

	content, err := os.ReadFile(pyprojectPath)
	if err != nil {
		return false
	}

	var pyproject map[string]any

	err = toml.Unmarshal(content, &pyproject)
	if err != nil {
		return false
	}

	members := uvWorkspaceMembers(pyproject)
	if len(members) == 0 {
		return false
	}

	for _, member := range members {
		if workspaceMemberMatchesHooksProject(member, settings) {
			return true
		}
	}

	return false
}

func uvWorkspaceMembers(pyproject map[string]any) []string {
	tool, hasTool := pyproject["tool"].(map[string]any)
	if !hasTool {
		return nil
	}

	uv, hasUV := tool["uv"].(map[string]any)
	if !hasUV {
		return nil
	}

	workspace, hasWorkspace := uv["workspace"].(map[string]any)
	if !hasWorkspace {
		return nil
	}

	return normalizeStringList(workspace["members"])
}

func workspaceMemberMatchesHooksProject(
	member string,
	settings typeCheckSettings,
) bool {
	normalized := filepath.ToSlash(filepath.Clean(strings.TrimSpace(member)))
	if normalized == "." || normalized == "" {
		return false
	}

	hooksRel, err := filepath.Rel(settings.ConsumerRoot, settings.HooksProject)
	if err == nil && normalized == filepath.ToSlash(filepath.Clean(hooksRel)) {
		return true
	}

	memberAbs := filepath.Join(settings.ConsumerRoot, filepath.FromSlash(normalized))
	if samePath(memberAbs, settings.HooksProject) {
		return true
	}

	return sameRealPath(memberAbs, settings.HooksProject)
}

func samePath(left, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)

	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}

	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func sameRealPath(left, right string) bool {
	leftReal, leftErr := filepath.EvalSymlinks(left)

	rightReal, rightErr := filepath.EvalSymlinks(right)
	if leftErr != nil || rightErr != nil {
		return false
	}

	return samePath(leftReal, rightReal)
}

func appendTypeCheckerConfig(
	command []string,
	checker typeCheckerConfig,
	settings typeCheckSettings,
) []string {
	if len(checker.ConfigFlags) == 0 ||
		commandHasAnyOption(command, checker.ConfigFlags) {
		return command
	}

	repoConfig := typeCheckConfigPath(settings.ConsumerRoot, checker.RepoConfig)
	if repoConfig != "" {
		return append(command, checker.ConfigFlags[0], repoConfig)
	}

	bundleConfig := typeCheckConfigPath(
		settings.BundleRoot,
		checker.FallbackBundleConfig,
	)
	if bundleConfig != "" {
		return append(command, checker.ConfigFlags[0], bundleConfig)
	}

	return command
}

func isCheckablePythonFile(path string, excludedPathFragments []string) bool {
	if path == "" || !strings.HasSuffix(path, ".py") {
		return false
	}

	if strings.HasPrefix(path, ".venv/") ||
		strings.Contains(
			path,
			string(filepath.Separator)+".venv"+string(filepath.Separator),
		) {
		return false
	}

	for _, fragment := range excludedPathFragments {
		if fragment != "" && strings.Contains(path, fragment) {
			return false
		}
	}

	return true
}

func normalizeTypeCheckFiles(
	paths []string,
	excludedPathFragments []string,
) []string {
	seen := map[string]bool{}

	files := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" || !isCheckablePythonFile(path, excludedPathFragments) {
			continue
		}

		absolutePath, err := filepath.Abs(path)
		if err != nil {
			continue
		}

		if seen[absolutePath] {
			continue
		}

		_, err = os.Stat(
			absolutePath,
		) // #nosec G703 -- path is cleaned and scoped above.
		if err != nil {
			continue
		}

		seen[absolutePath] = true
		files = append(files, absolutePath)
	}

	return files
}

func stagedTypeCheckFiles(settings typeCheckSettings) ([]string, error) {
	root := repoRoot()
	cmd := evaluators.GitCommand(
		root,
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
	)

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf(
				"failed to get staged files from git: %w: %s",
				err,
				strings.TrimSpace(string(exitErr.Stderr)),
			)
		}

		return nil, fmt.Errorf("failed to get staged files from git: %w", err)
	}

	return normalizeTypeCheckFiles(
		repoRelativeTypeCheckPaths(
			root,
			strings.Split(strings.TrimSpace(string(output)), "\n"),
		),
		settings.ExcludedPathFragments,
	), nil
}

func repoRelativeTypeCheckPaths(root string, paths []string) []string {
	normalized := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" || filepath.IsAbs(path) {
			normalized = append(normalized, path)

			continue
		}

		normalized = append(normalized, filepath.Join(root, path))
	}

	return normalized
}

func runTypeChecker(
	checker typeCheckerConfig,
	settings typeCheckSettings,
	files []string,
) typeCheckResult {
	start := time.Now()

	command := resolveTypeCheckerCommand(checker, settings)
	if checker.PassFilesAsArgs {
		command = append(command, files...)
	}

	if len(command) == 0 {
		return typeCheckResult{
			Name:     checker.Name,
			ExitCode: 1,
			Output:   "Error: empty checker command",
		}
	}

	toolResult := runExternalTool(externalToolRequest{
		Name:    checker.Name,
		Dir:     settings.ConsumerRoot,
		Command: command,
	})
	diagnostics := diag.Parse(
		defaultString(checker.Parser, checker.Name),
		toolResult.Stdout,
		toolResult.Stderr,
	)
	diagnostics = diag.Enrich(diagnostics, settings.EvidenceMaps)

	duration := float64(time.Since(start).Milliseconds())
	if toolResult.TimedOut {
		return typeCheckResult{
			Name:        checker.Name,
			ExitCode:    1,
			Diagnostics: []diag.Diagnostic{timeoutTypeCheckerDiagnostic(checker.Name)},
			DurationMS:  duration,
		}
	}

	if toolResult.RunnerFailure == nil && toolResult.ExitCode == 0 {
		return typeCheckResult{
			Name:        checker.Name,
			ExitCode:    0,
			Diagnostics: diagnostics,
			DurationMS:  duration,
		}
	}

	if toolResult.RunnerFailure == nil {
		if len(diagnostics) == 0 {
			diagnostics = []diag.Diagnostic{
				unparseableTypeCheckerDiagnostic(checker.Name, toolResult.ExitCode),
			}
		}

		return typeCheckResult{
			Name:        checker.Name,
			ExitCode:    toolResult.ExitCode,
			Diagnostics: diagnostics,
			DurationMS:  duration,
		}
	}

	return typeCheckResult{
		Name:        checker.Name,
		ExitCode:    1,
		Diagnostics: []diag.Diagnostic{typeCheckerRunnerDiagnostic(checker.Name)},
		DurationMS:  duration,
	}
}

func unparseableTypeCheckerDiagnostic(name string, exitCode int) diag.Diagnostic {
	return diag.Diagnostic{
		Tool:     name,
		Severity: "fatal",
		Code:     "UNPARSEABLE_OUTPUT",
		Message: fmt.Sprintf(
			"%s exited with status %d without parseable diagnostics.",
			name,
			exitCode,
		),
		Detail: "stdout/stderr were captured but are not rendered because they " +
			"were not parsed into diagnostics.",
	}
}

func timeoutTypeCheckerDiagnostic(name string) diag.Diagnostic {
	return diag.Diagnostic{
		Tool:     name,
		Severity: "fatal",
		Code:     timeoutCode,
		Message:  name + " timed out before completing.",
		Detail:   "Timeouts are fatal gate failures because the tool did not complete.",
	}
}

func typeCheckerRunnerDiagnostic(name string) diag.Diagnostic {
	return diag.Diagnostic{
		Tool:     name,
		Severity: "fatal",
		Code:     "RUNNER_FAILURE",
		Message:  "type checker runner failed before producing diagnostics.",
		Detail:   "stdout/stderr were captured but are not rendered.",
	}
}

func typeCheckSummaryForResults(
	results []typeCheckResult,
	fileCount int,
	format string,
) typeCheckSummary {
	summary := typeCheckSummary{
		Format:    format,
		Status:    statusPass,
		FileCount: fileCount,
		Results:   results,
	}
	for _, result := range results {
		summary.Duration += result.DurationMS
		if result.ExitCode == 0 {
			summary.Passed++
		} else {
			summary.Failed++
			summary.Status = statusFail
		}
	}

	return summary
}

func formatTypeCheckResults(
	results []typeCheckResult,
	fileCount int,
	format string,
) string {
	return formatTypeCheckResultsWithSkills(results, fileCount, format, nil)
}

func formatTypeCheckResultsWithSkills(
	results []typeCheckResult,
	fileCount int,
	format string,
	skills map[string]hookSkill,
) string {
	switch format {
	case hookOutputFormatJSON:
		return formatTypeCheckResultsJSON(results, fileCount)
	case hookOutputFormatTOON:
		return formatTypeCheckResultsTOON(results, fileCount, skills)
	default:
		return formatTypeCheckResultsHuman(results, fileCount, skills)
	}
}

func formatTypeCheckResultsJSON(results []typeCheckResult, fileCount int) string {
	payload := typeCheckSummaryForResults(
		results,
		fileCount,
		hookOutputFormatJSON,
	)

	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return formatTypeCheckResultsHuman(results, fileCount, nil)
	}

	return string(content)
}

func formatTypeCheckResultsTOON(
	results []typeCheckResult,
	fileCount int,
	skills map[string]hookSkill,
) string {
	summary := typeCheckSummaryForResults(
		results,
		fileCount,
		hookOutputFormatTOON,
	)
	lines := make([]string, 0, typeCheckTOONSummaryRows+len(results))
	lines = append(lines,
		"status: "+summary.Status,
		fmt.Sprintf("file_count: %d", summary.FileCount),
		fmt.Sprintf("passed: %d", summary.Passed),
		fmt.Sprintf("failed: %d", summary.Failed),
		fmt.Sprintf("duration_ms: %.0f", summary.Duration),
		"checks{name,status,exit_code,duration_ms}:",
	)

	lines = append(lines, typeCheckResultRowsTOON(results)...)

	diagnostics := typeCheckDiagnosticsForResults(results)
	lines = append(lines, typeCheckDiagnosticRowsTOON(diagnostics)...)
	lines = append(lines, typeCheckRawOutputRowsTOON(results)...)
	lines = append(lines, typeCheckSkillAdviceRowsTOON(diagnostics, skills)...)

	return strings.Join(lines, "\n")
}

func typeCheckResultRowsTOON(results []typeCheckResult) []string {
	lines := make([]string, 0, len(results))

	for _, result := range results {
		status := statusPass
		if result.ExitCode != 0 {
			status = statusFail
		}

		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%d,%.0f",
				toonCell(result.Name),
				status,
				result.ExitCode,
				result.DurationMS,
			),
		)
	}

	return lines
}

func typeCheckDiagnosticRowsTOON(diagnostics []diag.Diagnostic) []string {
	header := "diagnostics[%d]{tool,file,line,column,severity,code," +
		"policy_id,skill_id,message,advice}:"
	if len(diagnostics) == 0 {
		return []string{fmt.Sprintf(header, 0)}
	}

	lines := []string{fmt.Sprintf(header, len(diagnostics))}
	for _, diagnostic := range diagnostics {
		lines = append(
			lines,
			fmt.Sprintf(
				"  %s,%s,%d,%d,%s,%s,%s,%s,%s,%s",
				toonCell(diagnostic.Tool),
				toonCell(diagnostic.File),
				diagnostic.Line,
				diagnostic.Column,
				toonCell(defaultString(diagnostic.Severity, "error")),
				toonCell(diagnostic.Code),
				toonCell(diagnostic.PolicyID),
				toonCell(diagnostic.SkillID),
				toonCell(diagnostic.Message),
				toonCell(diagnostic.Advice),
			),
		)
	}

	return lines
}

func typeCheckRawOutputRowsTOON(results []typeCheckResult) []string {
	rawOutputs := typeCheckRawOutputsForResults(results)
	if len(rawOutputs) == 0 {
		return nil
	}

	lines := []string{fmt.Sprintf("raw_outputs[%d]{tool,output}:", len(rawOutputs))}
	for _, item := range rawOutputs {
		lines = append(
			lines,
			fmt.Sprintf("  %s,%s", toonCell(item.Name), toonCell(item.Output)),
		)
	}

	return lines
}

func typeCheckSkillAdviceRowsTOON(
	diagnostics []diag.Diagnostic,
	skills map[string]hookSkill,
) []string {
	hints := typeCheckSkillHints(diagnostics, skills)
	if len(hints) == 0 {
		return nil
	}

	lines := []string{
		fmt.Sprintf("advice[%d]{principle_id,skill_id,message,next}:", len(hints)),
	}
	for _, hint := range hints {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%s,%s",
			toonCell(hint.PrincipleID),
			toonCell(hint.SkillID),
			toonCell(hint.Message),
			toonCell(hint.Next),
		))
	}

	return lines
}

func typeCheckDiagnosticsForResults(
	results []typeCheckResult,
) []diag.Diagnostic {
	diagnostics := []diag.Diagnostic{}
	for _, result := range results {
		diagnostics = append(diagnostics, result.Diagnostics...)
	}

	sort.SliceStable(diagnostics, func(left, right int) bool {
		if diagnostics[left].File != diagnostics[right].File {
			return diagnostics[left].File < diagnostics[right].File
		}

		if diagnostics[left].Line != diagnostics[right].Line {
			return diagnostics[left].Line < diagnostics[right].Line
		}

		if diagnostics[left].Column != diagnostics[right].Column {
			return diagnostics[left].Column < diagnostics[right].Column
		}

		return diagnostics[left].Tool < diagnostics[right].Tool
	})

	return diagnostics
}

func typeCheckRawOutputsForResults(results []typeCheckResult) []typeCheckResult {
	return nil
}

type typeCheckSkillHint struct {
	PrincipleID string
	SkillID     string
	Message     string
	Next        string
}

func typeCheckSkillHints(
	diagnostics []diag.Diagnostic,
	skills map[string]hookSkill,
) []typeCheckSkillHint {
	if len(diagnostics) == 0 || len(skills) == 0 {
		return nil
	}

	hints := []typeCheckSkillHint{}
	seen := map[string]bool{}

	for _, diagnostic := range diagnostics {
		skillID := strings.TrimSpace(diagnostic.SkillID)
		if skillID == "" || seen[skillID] {
			continue
		}

		skill, ok := skills[skillID]
		if !ok {
			continue
		}

		message := firstNonEmpty(skill.ShortHint, skill.Description)
		if message == "" {
			continue
		}

		hints = append(hints, typeCheckSkillHint{
			PrincipleID: firstNonEmpty(
				firstTypeCheckPrinciple(diagnostic.PrincipleIDs),
				firstTypeCheckPrinciple(skill.PrincipleIDs),
			),
			SkillID: skillID,
			Message: message,
			Next:    "Load the " + skillID + " skill for the remediation playbook.",
		})
		seen[skillID] = true
	}

	return hints
}

func firstTypeCheckPrinciple(principles []string) string {
	for _, principle := range principles {
		if strings.TrimSpace(principle) != "" {
			return principle
		}
	}

	return ""
}

func toonCell(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "\\", "\\\\")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", " ")
	cleaned = strings.ReplaceAll(cleaned, "\n", " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	cleaned = strings.ReplaceAll(cleaned, ",", "\\,")

	return cleaned
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func formatTypeCheckResultsHuman(
	results []typeCheckResult,
	fileCount int,
	skills map[string]hookSkill,
) string {
	lines := []string{
		"",
		strings.Repeat("=", reportDividerWidth),
		fmt.Sprintf("PYTHON STATIC CHECKS (PARALLEL) - %d file(s)", fileCount),
		strings.Repeat("=", reportDividerWidth),
		"",
	}
	totalTime := 0.0
	passed := 0

	for _, result := range results {
		totalTime += result.DurationMS
		if result.ExitCode == 0 {
			passed++
		}
	}

	lines = append(
		lines,
		fmt.Sprintf("Summary: %d passed, %d failed", passed, len(results)-passed),
	)
	lines = append(
		lines,
		fmt.Sprintf("Total time: %.0fms (parallel execution)", totalTime),
	)
	lines = append(lines, "")

	for _, result := range results {
		icon := "OK"
		status := statusPass

		if result.ExitCode != 0 {
			icon = "XX"
			status = statusFail
		}

		lines = append(
			lines,
			fmt.Sprintf(
				"%s %s: %s (%.0fms)",
				icon,
				result.Name,
				status,
				result.DurationMS,
			),
		)
		if result.ExitCode != 0 && len(result.Diagnostics) > 0 {
			lines = append(lines, formatTypeCheckDiagnostics(result.Diagnostics)...)
		} else if result.ExitCode != 0 && strings.TrimSpace(result.Output) != "" {
			lines = append(lines, "")
			for line := range strings.SplitSeq(strings.TrimSpace(result.Output), "\n") {
				lines = append(lines, "   "+line)
			}

			lines = append(lines, "")
		}
	}

	if hints := typeCheckSkillHints(
		typeCheckDiagnosticsForResults(results),
		skills,
	); len(
		hints,
	) > 0 {
		lines = append(lines, "skill advice:")
		for _, hint := range hints {
			lines = append(
				lines,
				"- "+hint.SkillID+": "+hint.Message+" Next: "+hint.Next,
			)
		}
	}

	lines = append(lines, strings.Repeat("=", reportDividerWidth))

	return strings.Join(lines, "\n")
}

func formatTypeCheckDiagnostics(diagnostics []diag.Diagnostic) []string {
	grouped := map[string][]diag.Diagnostic{}
	files := []string{}

	for _, diagnostic := range diagnostics {
		file := diagnostic.File
		if file == "" {
			file = "<unknown>"
		}

		if _, ok := grouped[file]; !ok {
			files = append(files, file)
		}

		grouped[file] = append(grouped[file], diagnostic)
	}

	sort.Strings(files)

	lines := []string{""}
	for _, file := range files {
		lines = append(lines, "   "+file)
		for _, diagnostic := range grouped[file] {
			lines = append(lines, formatTypeCheckDiagnosticHuman(file, diagnostic)...)
		}

		lines = append(lines, "")
	}

	return lines
}

func formatTypeCheckDiagnosticHuman(file string, diagnostic diag.Diagnostic) []string {
	location := typeCheckLocationSuffix(diagnostic)
	code := typeCheckCodeSuffix(diagnostic)
	severity := defaultString(diagnostic.Severity, "error")

	lines := []string{
		fmt.Sprintf(
			"      %s%s [%s%s] %s",
			filepath.Base(file),
			location,
			severity,
			code,
			diagnostic.Message,
		),
	}
	if diagnostic.PolicyID == "" {
		return lines
	}

	lines = append(
		lines,
		fmt.Sprintf(
			"         policy: %s (%s)",
			diagnostic.PolicyID,
			defaultString(diagnostic.Confidence, "mapped"),
		),
	)
	if diagnostic.Advice != "" {
		lines = append(lines, "         advice: "+diagnostic.Advice)
	}

	if diagnostic.SkillID != "" {
		lines = append(lines, "         skill: "+diagnostic.SkillID)
	}

	return lines
}

func typeCheckLocationSuffix(diagnostic diag.Diagnostic) string {
	if diagnostic.Line <= 0 {
		return ""
	}

	location := fmt.Sprintf(":%d", diagnostic.Line)
	if diagnostic.Column > 0 {
		location += fmt.Sprintf(":%d", diagnostic.Column)
	}

	return location
}

func typeCheckCodeSuffix(diagnostic diag.Diagnostic) string {
	if diagnostic.Code == "" {
		return ""
	}

	return " " + diagnostic.Code
}

func configuredTypeCheckers(settings typeCheckSettings) []typeCheckerConfig {
	checkers := make([]typeCheckerConfig, 0, len(settings.Checkers))
	for _, checker := range settings.Checkers {
		if checker.Enabled && checker.Name != "" && len(checker.Command) > 0 {
			checkers = append(checkers, checker)
		}
	}

	return checkers
}

func loadFilesForTypeCheck(args []string) ([]string, error) {
	settings, err := loadTypeCheckSettings()
	if err != nil {
		return nil, err
	}

	if len(args) != 0 {
		return normalizeTypeCheckFiles(args, settings.ExcludedPathFragments), nil
	}

	return stagedTypeCheckFiles(settings)
}

func runConfiguredTypeCheckers(
	checkers []typeCheckerConfig,
	settings typeCheckSettings,
	files []string,
) []typeCheckResult {
	results := make([]typeCheckResult, len(checkers))

	var waitGroup sync.WaitGroup
	for checkerIndex, checker := range checkers {
		waitGroup.Add(1)

		go func(index int, candidate typeCheckerConfig) {
			defer waitGroup.Done()

			results[index] = runTypeChecker(candidate, settings, files)
		}(checkerIndex, checker)
	}

	waitGroup.Wait()

	return results
}

func checkTypeCheckersCommand(_ Config, args []string) int {
	settings, err := loadTypeCheckSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled {
		return 0
	}

	checkers := configuredTypeCheckers(settings)
	if len(checkers) == 0 {
		if hookVerboseSuccessOutputEnabled() {
			writeLine(os.Stderr, "No type checkers registered")
		}

		return 0
	}

	files, err := loadFilesForTypeCheck(args)
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if len(files) == 0 {
		if hookVerboseSuccessOutputEnabled() {
			writeLine(os.Stderr, "No staged Python files to check")
		}

		return 0
	}

	results := runConfiguredTypeCheckers(checkers, settings, files)

	for _, result := range results {
		if result.ExitCode != 0 {
			writeLine(
				os.Stdout,
				formatTypeCheckResultsWithSkills(
					results,
					len(files),
					selectedHookOutputFormat(),
					settings.Skills,
				),
			)
			writeText(
				os.Stderr,
				strings.Join([]string{
					"fatal: Python static analysis failed in one or more configured checkers.",
					"Fix the reported checker output above and run the hook again.",
				}, "\n"),
			)

			return 1
		}
	}

	return 0
}
