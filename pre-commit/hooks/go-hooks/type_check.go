// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:gocognit,lll // Type-check orchestration has several tool paths.
package main

import (
	"context"
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

	diag "blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/toolcatalog"
	"github.com/pelletier/go-toml/v2"
)

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

func defaultPolicyEvidenceMaps() []diag.EvidenceMap {
	return []diag.EvidenceMap{
		defaultRuffEvidenceMap(),
		defaultRuffImportOrderEvidenceMap(),
		defaultRuffSQLSafetyEvidenceMap(),
		defaultMypyEvidenceMap(),
		defaultShellcheckEvidenceMap(),
		defaultYamllintEvidenceMap(),
		defaultHadolintEvidenceMap(),
		defaultActionlintEvidenceMap(),
		defaultGolangciEvidenceMap(),
	}
}

func defaultRuffEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "ruff",
		Codes:        []string{"PLC" + "0415"},
		PolicyID:     "python.conditional_imports",
		PrincipleIDs: []string{"no-conditional-imports", "fail-fast-fail-hard-overview"},
		Confidence:   "high",
		Meaning:      "Import executes away from module scope, usually inside runtime control flow.",
		Advice: diag.EvidenceAdvice{
			Summary: "Move required imports to module scope and fail during startup.",
			Steps: []string{
				"Declare the dependency as required.",
				"Import it at module scope.",
				"Replace runtime fallback paths with startup validation.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultRuffImportOrderEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "ruff",
		Codes:        []string{"E402"},
		PolicyID:     "python.import_order",
		PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense", "linting-as-code-quality-enforcement"},
		Confidence:   "high",
		Meaning:      "Import ordering is hiding setup side effects or runtime dependency flow.",
		Advice: diag.EvidenceAdvice{
			Summary: "Move imports to the top of the module or split setup into a helper.",
			Steps: []string{
				"Put imports before executable statements.",
				"Move path or environment setup into test fixtures or helper modules.",
				"Keep dependency loading explicit and reviewable.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultRuffSQLSafetyEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "ruff",
		Codes:        []string{"S608"},
		PolicyID:     "python.sql_safety",
		PrincipleIDs: []string{"security-by-design", "linting-as-code-quality-enforcement"},
		Confidence:   "high",
		Meaning:      "SQL text is being assembled dynamically and may bypass parameterization.",
		Advice: diag.EvidenceAdvice{
			Summary: "Use parameterized SQL or a reviewed central SQL helper.",
			Steps: []string{
				"Replace string-built SQL with placeholders and bound parameters.",
				"If dynamic identifiers are required, validate them against an allowlist.",
				"Keep test-only SQL safety exceptions explicit and narrow.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultMypyEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "mypy",
		Codes:        []string{"no-any-return"},
		PolicyID:     "python.optional_returns",
		PrincipleIDs: []string{"no-optional-types-for-required-dependencies"},
		Confidence:   "medium",
		Meaning:      "A required return path is leaking Any instead of a precise type.",
		Advice: diag.EvidenceAdvice{
			Summary: "Replace Any return flow with an explicit required type.",
			Steps: []string{
				"Identify the source of Any.",
				"Add the missing annotation or typed adapter at the boundary.",
				"Keep required dependencies non-optional.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultShellcheckEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "shellcheck",
		Codes:        []string{"SC*"},
		PolicyID:     "shell.static_analysis",
		PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		Confidence:   "medium",
		Meaning:      "Shellcheck found fragile or ambiguous shell behavior.",
		Advice: diag.EvidenceAdvice{
			Summary: "Fix the shell script structure instead of suppressing ShellCheck.",
			Steps: []string{
				"Quote expansions and make data flow explicit.",
				"Prefer arrays and checked commands over stringly shell assembly.",
				"Keep shell behavior deterministic under strict mode.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultYamllintEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "yamllint",
		Codes:        []string{"indentation", "truthy"},
		PolicyID:     "yaml.config_clarity",
		PrincipleIDs: []string{"validation-at-the-gate"},
		Confidence:   "medium",
		Meaning:      "YAML structure or scalar spelling is ambiguous for configuration.",
		Advice: diag.EvidenceAdvice{
			Summary: "Make YAML configuration explicit and parser-stable.",
			Steps: []string{
				"Fix indentation to match the intended structure.",
				"Quote ambiguous scalars when the value is meant to be a string.",
				"Keep configuration readable enough to review in diffs.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultHadolintEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "hadolint",
		Codes:        []string{"DL*"},
		PolicyID:     "docker.reproducible_builds",
		PrincipleIDs: []string{"security-by-design"},
		Confidence:   "medium",
		Meaning:      "Dockerfile instructions weaken reproducibility or container safety.",
		Advice: diag.EvidenceAdvice{
			Summary: "Make the container build deterministic and least-privilege.",
			Steps: []string{
				"Pin package versions where practical.",
				"Avoid broad shell pipelines that hide failures.",
				"Prefer explicit users, trusted sources, and minimal layers.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultActionlintEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "actionlint",
		Codes:        []string{"*"},
		PolicyID:     "workflow.validation",
		PrincipleIDs: []string{"validation-at-the-gate"},
		Confidence:   "high",
		Meaning:      "GitHub Actions workflow syntax or expression behavior is invalid.",
		Advice: diag.EvidenceAdvice{
			Summary: "Fix workflow definitions before relying on CI as a quality gate.",
			Steps: []string{
				"Validate expressions, job wiring, and event-specific context.",
				"Keep workflow behavior explicit instead of runtime surprises.",
				"Re-run the workflow hook locally before pushing.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultGolangciEvidenceMap() diag.EvidenceMap {
	return diag.EvidenceMap{
		Source:       "golangci-lint",
		Codes:        []string{"errcheck", "gosec", "staticcheck", "revive"},
		PolicyID:     "go.static_analysis",
		PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		Confidence:   "high",
		Meaning:      "Go static analysis found correctness, security, or maintainability risk.",
		Advice: diag.EvidenceAdvice{
			Summary: "Fix the Go issue structurally and keep golangci-lint blocking.",
			Steps: []string{
				"Handle errors explicitly.",
				"Remove suspicious or insecure constructs instead of suppressing them.",
				"Prefer a small refactor over weakening lint coverage.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
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

func typeCheckConfigPath(root string, name string) string {
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

func commandHasAnyOption(command []string, options []string) bool {
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

func workspaceMemberMatchesHooksProject(member string, settings typeCheckSettings) bool {
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

func samePath(left string, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)

	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return filepath.Clean(left) == filepath.Clean(right)
	}

	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func sameRealPath(left string, right string) bool {
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

		_, err = os.Stat(absolutePath) // #nosec G703 -- path is cleaned and scoped above.
		if err != nil {
			continue
		}

		seen[absolutePath] = true
		files = append(files, absolutePath)
	}

	return files
}

func stagedTypeCheckFiles(settings typeCheckSettings) ([]string, error) {
	cmd := exec.CommandContext(
		context.Background(),
		"git",
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
	)
	cmd.Dir = repoRoot()

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
		strings.Split(strings.TrimSpace(string(output)), "\n"),
		settings.ExcludedPathFragments,
	), nil
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
	outputText := toolResult.Combined
	diagnostics := diag.Parse(defaultString(checker.Parser, checker.Name), outputText, "")
	diagnostics = diag.Enrich(diagnostics, settings.EvidenceMaps)

	duration := float64(time.Since(start).Milliseconds())
	if toolResult.RunnerFailure == nil && toolResult.ExitCode == 0 {
		return typeCheckResult{
			Name:        checker.Name,
			ExitCode:    0,
			Output:      outputText,
			Diagnostics: diagnostics,
			DurationMS:  duration,
		}
	}

	if toolResult.RunnerFailure == nil {
		return typeCheckResult{
			Name:        checker.Name,
			ExitCode:    toolResult.ExitCode,
			Output:      outputText,
			Diagnostics: diagnostics,
			DurationMS:  duration,
		}
	}

	return typeCheckResult{
		Name:       checker.Name,
		ExitCode:   1,
		Output:     fmt.Sprintf("Error running %s: %v", checker.Name, toolResult.RunnerFailure),
		DurationMS: duration,
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
	switch format {
	case hookOutputFormatJSON:
		return formatTypeCheckResultsJSON(results, fileCount)
	case hookOutputFormatTOON:
		return formatTypeCheckResultsTOON(results, fileCount)
	default:
		return formatTypeCheckResultsHuman(results, fileCount)
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
		return formatTypeCheckResultsHuman(results, fileCount)
	}

	return string(content)
}

func formatTypeCheckResultsTOON(results []typeCheckResult, fileCount int) string {
	summary := typeCheckSummaryForResults(
		results,
		fileCount,
		hookOutputFormatTOON,
	)
	lines := []string{
		"format: " + summary.Format,
		"status: " + summary.Status,
		fmt.Sprintf("file_count: %d", summary.FileCount),
		fmt.Sprintf("passed: %d", summary.Passed),
		fmt.Sprintf("failed: %d", summary.Failed),
		fmt.Sprintf("duration_ms: %.0f", summary.Duration),
		"checks{name,status,exit_code,duration_ms}:",
	}

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

	diagnostics := typeCheckDiagnosticsForResults(results)
	if len(diagnostics) == 0 {
		lines = append(lines, "diagnostics[0]{tool,file,line,column,severity,code,policy_id,message,advice}:")
	} else {
		lines = append(
			lines,
			fmt.Sprintf(
				"diagnostics[%d]{tool,file,line,column,severity,code,policy_id,message,advice}:",
				len(diagnostics),
			),
		)
		for _, diagnostic := range diagnostics {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s,%s,%d,%d,%s,%s,%s,%s,%s",
					toonCell(diagnostic.Tool),
					toonCell(diagnostic.File),
					diagnostic.Line,
					diagnostic.Column,
					toonCell(defaultString(diagnostic.Severity, "error")),
					toonCell(diagnostic.Code),
					toonCell(diagnostic.PolicyID),
					toonCell(diagnostic.Message),
					toonCell(diagnostic.Advice),
				),
			)
		}
	}

	rawOutputs := typeCheckRawOutputsForResults(results)
	if len(rawOutputs) > 0 {
		lines = append(
			lines,
			fmt.Sprintf("raw_outputs[%d]{tool,output}:", len(rawOutputs)),
		)
		for _, item := range rawOutputs {
			lines = append(
				lines,
				fmt.Sprintf("  %s,%s", toonCell(item.Name), toonCell(item.Output)),
			)
		}
	}

	return strings.Join(lines, "\n")
}

func typeCheckDiagnosticsForResults(
	results []typeCheckResult,
) []diag.Diagnostic {
	diagnostics := []diag.Diagnostic{}
	for _, result := range results {
		diagnostics = append(diagnostics, result.Diagnostics...)
	}

	sort.SliceStable(diagnostics, func(left int, right int) bool {
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
	rawOutputs := []typeCheckResult{}

	for _, result := range results {
		if result.ExitCode == 0 || len(result.Diagnostics) > 0 ||
			strings.TrimSpace(result.Output) == "" {
			continue
		}

		rawOutputs = append(rawOutputs, result)
	}

	return rawOutputs
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

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}

	return value
}

func formatTypeCheckResultsHuman(results []typeCheckResult, fileCount int) string {
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
			location := ""
			if diagnostic.Line > 0 {
				location = fmt.Sprintf(":%d", diagnostic.Line)
				if diagnostic.Column > 0 {
					location += fmt.Sprintf(":%d", diagnostic.Column)
				}
			}

			code := diagnostic.Code
			if code != "" {
				code = " " + code
			}

			severity := diagnostic.Severity
			if severity == "" {
				severity = "error"
			}

			lines = append(
				lines,
				fmt.Sprintf(
					"      %s%s [%s%s] %s",
					filepath.Base(file),
					location,
					severity,
					code,
					diagnostic.Message,
				),
			)
			if diagnostic.PolicyID != "" {
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
			}
		}

		lines = append(lines, "")
	}

	return lines
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
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled {
		return 0
	}

	checkers := configuredTypeCheckers(settings)
	if len(checkers) == 0 {
		if hookVerboseSuccessOutputEnabled() {
			fmt.Fprintln(os.Stderr, "No type checkers registered")
		}

		return 0
	}

	files, err := loadFilesForTypeCheck(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if len(files) == 0 {
		if hookVerboseSuccessOutputEnabled() {
			fmt.Fprintln(os.Stderr, "No staged Python files to check")
		}

		return 0
	}

	results := runConfiguredTypeCheckers(checkers, settings, files)

	for _, result := range results {
		if result.ExitCode != 0 {
			_, _ = fmt.Fprintln(
				os.Stdout,
				formatTypeCheckResults(
					results,
					len(files),
					selectedHookOutputFormat(),
				),
			)
			_, _ = fmt.Fprintln(os.Stderr)
			_, _ = fmt.Fprintln(
				os.Stderr,
				"FATAL: Python static analysis failed in one or more configured checkers.",
			)
			_, _ = fmt.Fprintln(
				os.Stderr,
				"Fix the reported checker output above and run the hook again.",
			)
			_, _ = fmt.Fprintln(os.Stderr)

			return 1
		}
	}

	return 0
}
