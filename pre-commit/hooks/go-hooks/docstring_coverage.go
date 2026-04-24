package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type docstringCoverageSettings struct {
	Enabled         bool     `json:"enabled"`
	Threshold       int      `json:"threshold"`
	CheckPaths      []string `json:"check_paths"`
	ExcludePatterns []string `json:"exclude_patterns"`
	Command         []string `json:"command"`
	UseHookProject  bool     `json:"use_hook_project"`
	BundleRoot      string   `json:"-"`
	ConsumerRoot    string   `json:"-"`
	HooksProject    string   `json:"-"`
}

func configSectionFieldPresent(rootConfig map[string]any, path string, field string) bool {
	value, ok := rootConfigValue(rootConfig, path)
	if !ok {
		return false
	}
	section, ok := value.(map[string]any)
	if !ok {
		return false
	}
	_, present := section[field]
	return present
}

func loadDocstringCoverageSettings() (docstringCoverageSettings, error) {
	var settings docstringCoverageSettings
	bundleRoot, consumer, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, err
	}
	if value, ok := rootConfigValue(rootConfig, "python.docstring_coverage"); ok {
		section, ok := value.(map[string]any)
		if !ok {
			return settings, fmt.Errorf("parse docstring_coverage config: expected mapping")
		}
		if enabled, ok := section["enabled"]; ok {
			typed, ok := enabled.(bool)
			if !ok {
				return settings, fmt.Errorf("parse docstring_coverage config: enabled must be boolean")
			}
			settings.Enabled = typed
		}
		if threshold, ok := section["threshold"]; ok {
			switch typed := threshold.(type) {
			case int:
				settings.Threshold = typed
			case int64:
				settings.Threshold = int(typed)
			case float64:
				settings.Threshold = int(typed)
			default:
				return settings, fmt.Errorf("parse docstring_coverage config: threshold must be numeric")
			}
		}
		if paths, ok := section["check_paths"]; ok {
			settings.CheckPaths = normalizeStringList(paths)
		}
		if patterns, ok := section["exclude_patterns"]; ok {
			settings.ExcludePatterns = normalizeStringList(patterns)
		}
		if command, ok := section["command"]; ok {
			settings.Command = normalizeStringList(command)
		}
		if useHookProject, ok := section["use_hook_project"]; ok {
			typed, ok := useHookProject.(bool)
			if !ok {
				return settings, fmt.Errorf("parse docstring_coverage config: use_hook_project must be boolean")
			}
			settings.UseHookProject = typed
		}
	}
	settings.BundleRoot = bundleRoot
	settings.ConsumerRoot = consumer
	settings.HooksProject = filepath.Join(bundleRoot, "hooks")
	if settings.Threshold <= 0 {
		settings.Threshold = 90
	}
	if len(settings.CheckPaths) == 0 {
		settings.CheckPaths = []string{"coding_ethos", "pre-commit/hooks"}
	}
	if len(settings.ExcludePatterns) == 0 {
		settings.ExcludePatterns = []string{`__pycache__`, `\.venv`, `tests`, `.*_test\.py$`, `test_.*\.py$`}
	}
	if len(settings.Command) == 0 {
		settings.Command = []string{"interrogate"}
	}
	if !settings.UseHookProject && !configSectionFieldPresent(rootConfig, "python.docstring_coverage", "use_hook_project") {
		settings.UseHookProject = true
	}
	return settings, nil
}

func buildDocstringCoverageCommand(settings docstringCoverageSettings) []string {
	command := append([]string{}, settings.Command...)
	command = append(
		command,
		"--fail-under",
		fmt.Sprint(settings.Threshold),
		"--verbose",
		"--ignore-init-method",
		"--ignore-init-module",
		"--ignore-magic",
		"--ignore-private",
		"--ignore-semiprivate",
		"--ignore-property-decorators",
		"--ignore-nested-functions",
		"--ignore-nested-classes",
	)
	for _, pattern := range settings.ExcludePatterns {
		command = append(command, "--ignore-regex", pattern)
	}
	command = append(command, settings.CheckPaths...)
	if settings.UseHookProject {
		command = append([]string{"uv", "run", "--quiet", "--project", settings.HooksProject}, command...)
	}
	return command
}

func runDocstringCoverage(settings docstringCoverageSettings) (int, string, string, error) {
	command := buildDocstringCoverageCommand(settings)
	if len(command) == 0 {
		return 1, "", "", fmt.Errorf("docstring coverage command is empty")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = settings.ConsumerRoot
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String(), nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), stdout.String(), stderr.String(), nil
	}
	return 1, stdout.String(), stderr.String(), err
}

func checkDocstringCoverageCommand(_ Config, _ []string) int {
	settings, err := loadDocstringCoverageSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
		return 1
	}
	if !settings.Enabled {
		return 0
	}

	exitCode, stdout, stderr, err := runDocstringCoverage(settings)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: failed to run docstring coverage command: %v\n", err)
		return 1
	}
	if exitCode == 0 {
		return 0
	}

	fmt.Fprintln(os.Stdout, strings.Repeat("=", 60))
	fmt.Fprintln(os.Stdout, "DOCSTRING COVERAGE CHECK FAILED (ETHOS §18)")
	fmt.Fprintln(os.Stdout, strings.Repeat("=", 60))
	fmt.Fprintf(os.Stdout, "Threshold: %d%%\n", settings.Threshold)
	fmt.Fprintf(os.Stdout, "Paths: %s\n", strings.Join(settings.CheckPaths, ", "))
	fmt.Fprintln(os.Stdout)
	if strings.TrimSpace(stdout) != "" {
		fmt.Fprint(os.Stdout, stdout)
		if !strings.HasSuffix(stdout, "\n") {
			fmt.Fprintln(os.Stdout)
		}
	}
	if strings.TrimSpace(stderr) != "" {
		fmt.Fprint(os.Stderr, stderr)
		if !strings.HasSuffix(stderr, "\n") {
			fmt.Fprintln(os.Stderr)
		}
	}
	fmt.Fprintln(os.Stdout)
	fmt.Fprintln(os.Stdout, "Per ETHOS §18 (Documentation as Contract):")
	fmt.Fprintln(os.Stdout, "  - Every public function must have a Google-style docstring")
	fmt.Fprintln(os.Stdout, "  - Docstrings document the contract between function and caller")
	fmt.Fprintln(os.Stdout, "  - If you change behavior, update the docstring")
	fmt.Fprintln(os.Stdout)
	fmt.Fprintf(os.Stdout, "Add docstrings to reach %d%% coverage.\n", settings.Threshold)
	return 1
}
