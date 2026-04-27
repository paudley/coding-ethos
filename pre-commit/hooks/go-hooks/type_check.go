// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

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

	"github.com/pelletier/go-toml/v2"
)

type typeCheckerConfig struct {
	Name                 string
	RepoConfig           string
	FallbackBundleConfig string
	Command              []string
	ConfigFlags          []string
	PassFilesAsArgs      bool
	UseHookProject       bool
}

type typeCheckSettings struct {
	BundleRoot            string
	ConsumerRoot          string
	HooksProject          string
	Checkers              []typeCheckerConfig
	ExcludedPathFragments []string
	Enabled               bool
}

type typeCheckResult struct {
	Name        string                `json:"name"`
	Output      string                `json:"output,omitempty"`
	Diagnostics []typeCheckDiagnostic `json:"diagnostics,omitempty"`
	ExitCode    int                   `json:"exit_code"`
	DurationMS  float64               `json:"duration_ms"`
}

type typeCheckSummary struct {
	Format    string            `json:"format"`
	Status    string            `json:"status"`
	FileCount int               `json:"file_count"`
	Passed    int               `json:"passed"`
	Failed    int               `json:"failed"`
	Duration  float64           `json:"duration_ms"`
	Results   []typeCheckResult `json:"results"`
}

type typeCheckDiagnostic struct {
	Checker  string `json:"checker"`
	File     string `json:"file"`
	Severity string `json:"severity"`
	Code     string `json:"code,omitempty"`
	Message  string `json:"message"`
	Line     int    `json:"line,omitempty"`
	Column   int    `json:"column,omitempty"`
}

func defaultTypeCheckers() []typeCheckerConfig {
	return []typeCheckerConfig{
		{
			Name:                 "ruff",
			Command:              []string{"ruff", "check", "--quiet", "--ignore-noqa", "--output-format", "json"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--config"},
			RepoConfig:           "ruff.toml",
			FallbackBundleConfig: "",
		},
		{
			Name:                 "pyright",
			Command:              []string{"pyright", "--outputjson"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--project", "-p"},
			RepoConfig:           "pyrightconfig.json",
			FallbackBundleConfig: "hooks/pyproject.toml",
		},
		{
			Name:                 "mypy",
			Command:              []string{"mypy", "--output", "json"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--config-file"},
			RepoConfig:           "mypy.ini",
			FallbackBundleConfig: "hooks/pyproject.toml",
		},
		{
			Name:                 "pylint",
			Command:              []string{"pylint", "--output-format=json"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--rcfile"},
			RepoConfig:           ".pylintrc",
			FallbackBundleConfig: "",
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
	if len(checker.Command) == 0 && hasDefault {
		checker.Command = append([]string{}, defaultChecker.Command...)
	}
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
	if len(checker.ConfigFlags) == 0 && hasDefault {
		checker.ConfigFlags = append([]string{}, defaultChecker.ConfigFlags...)
		if checker.RepoConfig == "" {
			checker.RepoConfig = defaultChecker.RepoConfig
		}
		if checker.FallbackBundleConfig == "" {
			checker.FallbackBundleConfig = defaultChecker.FallbackBundleConfig
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
	value, ok := rootConfigValue(rootConfig, "python.type_check.checkers")
	if !ok {
		return false
	}
	items, ok := value.([]any)
	if !ok {
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
	if err := toml.Unmarshal(content, &pyproject); err != nil {
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
	tool, ok := pyproject["tool"].(map[string]any)
	if !ok {
		return nil
	}
	uv, ok := tool["uv"].(map[string]any)
	if !ok {
		return nil
	}
	workspace, ok := uv["workspace"].(map[string]any)
	if !ok {
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
		_, err = os.Stat(absolutePath)
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
	diagnostics := parseTypeCheckDiagnostics(checker.Name, outputText)
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

func parseTypeCheckDiagnostics(checker string, output string) []typeCheckDiagnostic {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	switch checker {
	case "ruff":
		return parseRuffDiagnostics(output)
	case "pyright":
		return parsePyrightDiagnostics(output)
	case "mypy":
		return parseMypyDiagnostics(output)
	case "pylint":
		return parsePylintDiagnostics(output)
	default:
		return nil
	}
}

func parseRuffDiagnostics(output string) []typeCheckDiagnostic {
	var items []struct {
		Filename string `json:"filename"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		Location struct {
			Row    int `json:"row"`
			Column int `json:"column"`
		} `json:"location"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return nil
	}
	diagnostics := make([]typeCheckDiagnostic, 0, len(items))
	for _, item := range items {
		diagnostics = append(diagnostics, typeCheckDiagnostic{
			Checker:  "ruff",
			File:     item.Filename,
			Severity: "error",
			Code:     item.Code,
			Message:  item.Message,
			Line:     item.Location.Row,
			Column:   item.Location.Column,
		})
	}
	return diagnostics
}

func parsePyrightDiagnostics(output string) []typeCheckDiagnostic {
	var payload struct {
		GeneralDiagnostics []struct {
			File     string `json:"file"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Rule     string `json:"rule"`
			Range    struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
		} `json:"generalDiagnostics"`
	}
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		return nil
	}
	diagnostics := make([]typeCheckDiagnostic, 0, len(payload.GeneralDiagnostics))
	for _, item := range payload.GeneralDiagnostics {
		diagnostics = append(diagnostics, typeCheckDiagnostic{
			Checker:  "pyright",
			File:     item.File,
			Severity: item.Severity,
			Code:     item.Rule,
			Message:  item.Message,
			Line:     item.Range.Start.Line + 1,
			Column:   item.Range.Start.Character + 1,
		})
	}
	return diagnostics
}

func parseMypyDiagnostics(output string) []typeCheckDiagnostic {
	var items []struct {
		File     string `json:"file"`
		Path     string `json:"path"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		Code     string `json:"code"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	}
	trimmedOutput := strings.TrimSpace(output)
	if strings.HasPrefix(trimmedOutput, "[") {
		if err := json.Unmarshal([]byte(trimmedOutput), &items); err != nil {
			return nil
		}
	} else {
		for _, line := range strings.Split(trimmedOutput, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var item struct {
				File     string `json:"file"`
				Path     string `json:"path"`
				Severity string `json:"severity"`
				Message  string `json:"message"`
				Code     string `json:"code"`
				Line     int    `json:"line"`
				Column   int    `json:"column"`
			}
			if err := json.Unmarshal([]byte(line), &item); err != nil {
				return nil
			}
			items = append(items, item)
		}
	}
	diagnostics := make([]typeCheckDiagnostic, 0, len(items))
	for _, item := range items {
		file := item.File
		if file == "" {
			file = item.Path
		}
		diagnostics = append(diagnostics, typeCheckDiagnostic{
			Checker:  "mypy",
			File:     file,
			Severity: item.Severity,
			Code:     item.Code,
			Message:  item.Message,
			Line:     item.Line,
			Column:   item.Column,
		})
	}
	return diagnostics
}

func parsePylintDiagnostics(output string) []typeCheckDiagnostic {
	var items []struct {
		Path      string `json:"path"`
		Type      string `json:"type"`
		Symbol    string `json:"symbol"`
		MessageID string `json:"message-id"`
		Message   string `json:"message"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
	}
	if err := json.Unmarshal([]byte(output), &items); err != nil {
		return nil
	}
	diagnostics := make([]typeCheckDiagnostic, 0, len(items))
	for _, item := range items {
		code := item.Symbol
		if code == "" {
			code = item.MessageID
		}
		diagnostics = append(diagnostics, typeCheckDiagnostic{
			Checker:  "pylint",
			File:     item.Path,
			Severity: item.Type,
			Code:     code,
			Message:  item.Message,
			Line:     item.Line,
			Column:   item.Column + 1,
		})
	}
	return diagnostics
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
		fmt.Sprintf("format: %s", summary.Format),
		fmt.Sprintf("status: %s", summary.Status),
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
		lines = append(lines, "diagnostics[0]{tool,file,line,column,severity,code,message}:")
	} else {
		lines = append(
			lines,
			fmt.Sprintf(
				"diagnostics[%d]{tool,file,line,column,severity,code,message}:",
				len(diagnostics),
			),
		)
		for _, diagnostic := range diagnostics {
			lines = append(
				lines,
				fmt.Sprintf(
					"  %s,%s,%d,%d,%s,%s,%s",
					toonCell(diagnostic.Checker),
					toonCell(diagnostic.File),
					diagnostic.Line,
					diagnostic.Column,
					toonCell(defaultString(diagnostic.Severity, "error")),
					toonCell(diagnostic.Code),
					toonCell(diagnostic.Message),
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
) []typeCheckDiagnostic {
	diagnostics := []typeCheckDiagnostic{}
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

		return diagnostics[left].Checker < diagnostics[right].Checker
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
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, "\n", "\\n")
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
			for _, line := range strings.Split(strings.TrimSpace(result.Output), "\n") {
				lines = append(lines, "   "+line)
			}
			lines = append(lines, "")
		}
	}
	lines = append(lines, strings.Repeat("=", reportDividerWidth))

	return strings.Join(lines, "\n")
}

func formatTypeCheckDiagnostics(diagnostics []typeCheckDiagnostic) []string {
	grouped := map[string][]typeCheckDiagnostic{}
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
		}
		lines = append(lines, "")
	}
	return lines
}

func configuredTypeCheckers(settings typeCheckSettings) []typeCheckerConfig {
	checkers := make([]typeCheckerConfig, 0, len(settings.Checkers))
	for _, checker := range settings.Checkers {
		if checker.Name != "" && len(checker.Command) > 0 {
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
		fmt.Fprintln(os.Stderr, "No type checkers registered")

		return 0
	}

	files, err := loadFilesForTypeCheck(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No staged Python files to check")

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
