// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Name       string
	Output     string
	ExitCode   int
	DurationMS float64
}

func defaultTypeCheckers() []typeCheckerConfig {
	return []typeCheckerConfig{
		{
			Name:                 "pyright",
			Command:              []string{"pyright"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--project", "-p"},
			RepoConfig:           "pyrightconfig.json",
			FallbackBundleConfig: "hooks/pyproject.toml",
		},
		{
			Name:                 "mypy",
			Command:              []string{"mypy"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--config-file"},
			RepoConfig:           "mypy.ini",
			FallbackBundleConfig: "hooks/pyproject.toml",
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
		if path == "" || seen[path] ||
			!isCheckablePythonFile(path, excludedPathFragments) {
			continue
		}
		_, err := os.Stat(path)
		if err != nil {
			continue
		}
		seen[path] = true
		files = append(files, path)
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
	cmd := exec.CommandContext(context.Background(), command[0], command[1:]...)
	cmd.Dir = settings.ConsumerRoot
	output, err := cmd.CombinedOutput()
	duration := float64(time.Since(start).Milliseconds())
	if err == nil {
		return typeCheckResult{
			Name:       checker.Name,
			ExitCode:   0,
			Output:     string(output),
			DurationMS: duration,
		}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return typeCheckResult{
			Name:       checker.Name,
			ExitCode:   exitErr.ExitCode(),
			Output:     strings.TrimSpace(string(output)),
			DurationMS: duration,
		}
	}

	return typeCheckResult{
		Name:       checker.Name,
		ExitCode:   1,
		Output:     fmt.Sprintf("Error running %s: %v", checker.Name, err),
		DurationMS: duration,
	}
}

func formatTypeCheckResults(results []typeCheckResult, fileCount int) string {
	lines := []string{
		"",
		strings.Repeat("=", reportDividerWidth),
		fmt.Sprintf("TYPE CHECKING (PARALLEL) - %d staged file(s)", fileCount),
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
		if result.ExitCode != 0 && strings.TrimSpace(result.Output) != "" {
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
			_, _ = fmt.Fprintln(os.Stdout, formatTypeCheckResults(results, len(files)))
			_, _ = fmt.Fprintln(os.Stderr)
			_, _ = fmt.Fprintln(
				os.Stderr,
				"FATAL: type checking failed in one or more configured checkers.",
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
