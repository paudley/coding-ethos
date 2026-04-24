package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type typeCheckerConfig struct {
	Name                 string   `json:"name"`
	Command              []string `json:"command"`
	PassFilesAsArgs      bool     `json:"pass_files_as_args"`
	UseHookProject       bool     `json:"use_hook_project"`
	ConfigFlags          []string `json:"config_flags"`
	RepoConfig           string   `json:"repo_config"`
	FallbackBundleConfig string   `json:"fallback_bundle_config"`
}

type typeCheckSettings struct {
	Enabled      bool                `json:"enabled"`
	Checkers     []typeCheckerConfig `json:"checkers"`
	BundleRoot   string              `json:"-"`
	ConsumerRoot string              `json:"-"`
	HooksProject string              `json:"-"`
}

type typeCheckResult struct {
	Name       string
	ExitCode   int
	Output     string
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
	if err := decodeConfigSection(rootConfig, "python.type_check", &settings); err != nil {
		return settings, fmt.Errorf("parse type_check config: %w", err)
	}
	settings.BundleRoot = bundleRoot
	settings.ConsumerRoot = consumer
	settings.HooksProject = filepath.Join(bundleRoot, "hooks")
	if len(settings.Checkers) == 0 {
		settings.Checkers = defaultTypeCheckers()
	}
	for i := range settings.Checkers {
		checker := &settings.Checkers[i]
		if len(checker.Command) == 0 {
			for _, candidate := range defaultTypeCheckers() {
				if checker.Name == candidate.Name {
					checker.Command = append([]string{}, candidate.Command...)
					break
				}
			}
		}
		if !checker.PassFilesAsArgs {
			// Leave explicit false as-is.
		} else if !fieldPresentInTypeCheckerConfig(rootConfig, checker.Name, "pass_files_as_args") {
			checker.PassFilesAsArgs = true
		}
		if !checker.UseHookProject && !fieldPresentInTypeCheckerConfig(rootConfig, checker.Name, "use_hook_project") {
			checker.UseHookProject = true
		}
		if len(checker.ConfigFlags) == 0 {
			for _, candidate := range defaultTypeCheckers() {
				if checker.Name == candidate.Name {
					checker.ConfigFlags = append([]string{}, candidate.ConfigFlags...)
					if checker.RepoConfig == "" {
						checker.RepoConfig = candidate.RepoConfig
					}
					if checker.FallbackBundleConfig == "" {
						checker.FallbackBundleConfig = candidate.FallbackBundleConfig
					}
					break
				}
			}
		}
	}
	return settings, nil
}

func fieldPresentInTypeCheckerConfig(rootConfig map[string]any, name string, field string) bool {
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
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
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

func resolveTypeCheckerCommand(checker typeCheckerConfig, settings typeCheckSettings) []string {
	command := append([]string{}, checker.Command...)
	if len(checker.ConfigFlags) > 0 && !commandHasAnyOption(command, checker.ConfigFlags) {
		if repoConfig := typeCheckConfigPath(settings.ConsumerRoot, checker.RepoConfig); repoConfig != "" {
			command = append(command, checker.ConfigFlags[0], repoConfig)
		} else if bundleConfig := typeCheckConfigPath(settings.BundleRoot, checker.FallbackBundleConfig); bundleConfig != "" {
			command = append(command, checker.ConfigFlags[0], bundleConfig)
		}
	}
	if checker.UseHookProject {
		command = append([]string{"uv", "run", "--quiet", "--project", settings.HooksProject}, command...)
	}
	return command
}

func isCheckablePythonFile(path string) bool {
	return path != "" &&
		strings.HasSuffix(path, ".py") &&
		!strings.HasPrefix(path, ".venv/") &&
		!strings.Contains(path, string(filepath.Separator)+".venv"+string(filepath.Separator)) &&
		!strings.Contains(path, "/docker/") &&
		!strings.Contains(path, "vulture_whitelist")
}

func normalizeTypeCheckFiles(paths []string) []string {
	seen := map[string]bool{}
	files := make([]string, 0, len(paths))
	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" || seen[path] || !isCheckablePythonFile(path) {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			continue
		}
		seen[path] = true
		files = append(files, path)
	}
	return files
}

func stagedTypeCheckFiles() ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only", "--diff-filter=ACMR")
	cmd.Dir = repoRoot()
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("failed to get staged files from git: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("failed to get staged files from git: %w", err)
	}
	return normalizeTypeCheckFiles(strings.Split(strings.TrimSpace(string(output)), "\n")), nil
}

func runTypeChecker(checker typeCheckerConfig, settings typeCheckSettings, files []string) typeCheckResult {
	start := time.Now()
	command := resolveTypeCheckerCommand(checker, settings)
	if checker.PassFilesAsArgs {
		command = append(command, files...)
	}
	if len(command) == 0 {
		return typeCheckResult{Name: checker.Name, ExitCode: 1, Output: "Error: empty checker command"}
	}
	cmd := exec.Command(command[0], command[1:]...)
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
		strings.Repeat("=", 70),
		fmt.Sprintf("TYPE CHECKING (PARALLEL) - %d staged file(s)", fileCount),
		strings.Repeat("=", 70),
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
	lines = append(lines, fmt.Sprintf("Summary: %d passed, %d failed", passed, len(results)-passed))
	lines = append(lines, fmt.Sprintf("Total time: %.0fms (parallel execution)", totalTime))
	lines = append(lines, "")
	for _, result := range results {
		icon := "OK"
		status := "PASS"
		if result.ExitCode != 0 {
			icon = "XX"
			status = "FAIL"
		}
		lines = append(lines, fmt.Sprintf("%s %s: %s (%.0fms)", icon, result.Name, status, result.DurationMS))
		if result.ExitCode != 0 && strings.TrimSpace(result.Output) != "" {
			lines = append(lines, "")
			for _, line := range strings.Split(strings.TrimSpace(result.Output), "\n") {
				lines = append(lines, "   "+line)
			}
			lines = append(lines, "")
		}
	}
	lines = append(lines, strings.Repeat("=", 70))
	return strings.Join(lines, "\n")
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
	checkers := make([]typeCheckerConfig, 0, len(settings.Checkers))
	for _, checker := range settings.Checkers {
		if checker.Name != "" && len(checker.Command) > 0 {
			checkers = append(checkers, checker)
		}
	}
	if len(checkers) == 0 {
		fmt.Fprintln(os.Stderr, "No type checkers registered")
		return 0
	}

	files := normalizeTypeCheckFiles(args)
	if len(args) == 0 {
		files, err = stagedTypeCheckFiles()
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
			return 1
		}
	}
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "No staged Python files to check")
		return 0
	}

	results := make([]typeCheckResult, len(checkers))
	var wg sync.WaitGroup
	for i, checker := range checkers {
		wg.Add(1)
		go func(idx int, candidate typeCheckerConfig) {
			defer wg.Done()
			results[idx] = runTypeChecker(candidate, settings, files)
		}(i, checker)
	}
	wg.Wait()

	for _, result := range results {
		if result.ExitCode != 0 {
			fmt.Fprintln(os.Stdout, formatTypeCheckResults(results, len(files)))
			fmt.Fprintln(os.Stderr)
			fmt.Fprintln(os.Stderr, "XX Type checking failed")
			fmt.Fprintln(os.Stderr, "   Fix the errors above and try again.")
			fmt.Fprintln(os.Stderr)
			return 1
		}
	}
	return 0
}
