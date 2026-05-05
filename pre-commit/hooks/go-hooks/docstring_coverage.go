// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:gosec // Runs validated hook commands.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var errDocstringCoverageCommandEmpty = errors.New(
	"docstring coverage command is empty",
)

const nativeDocstringCoverageCommand = "coding-ethos-docstring-coverage"

type docstringCoverageSettings struct {
	BundleRoot               string
	ConsumerRoot             string
	HooksProject             string
	CheckPaths               []string
	ExcludePatterns          []string
	Command                  []string
	Threshold                int
	Enabled                  bool
	UseHookProject           bool
	Verbose                  bool
	IgnoreInitMethod         bool
	IgnoreInitModule         bool
	IgnoreMagic              bool
	IgnorePrivate            bool
	IgnoreSemiprivate        bool
	IgnorePropertyDecorators bool
	IgnoreNestedFunctions    bool
	IgnoreNestedClasses      bool
}

type docstringCoverageFailureReport struct {
	Format    string   `json:"format"`
	Tool      string   `json:"tool"`
	Status    string   `json:"status"`
	Stdout    string   `json:"stdout,omitempty"`
	Stderr    string   `json:"stderr,omitempty"`
	Paths     []string `json:"paths"`
	Guidance  []string `json:"guidance"`
	Threshold int      `json:"threshold"`
}

func configSectionFieldPresent(
	rootConfig map[string]any,
	path string,
	field string,
) bool {
	value, found := rootConfigValue(rootConfig, path)
	if !found {
		return false
	}

	section, isSection := value.(map[string]any)
	if !isSection {
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

	err = decodeConfigSection(rootConfig, "python.docstring_coverage", &settings)
	if err != nil {
		return settings, fmt.Errorf("parse docstring_coverage config: %w", err)
	}

	settings.BundleRoot = bundleRoot
	settings.ConsumerRoot = consumer
	settings.HooksProject = filepath.Join(bundleRoot, "hooks")
	applyDocstringCoverageDefaults(&settings, rootConfig)

	return settings, nil
}

func applyDocstringCoverageDefaults(
	settings *docstringCoverageSettings,
	rootConfig map[string]any,
) {
	if settings.Threshold <= 0 {
		settings.Threshold = 90
	}

	if len(settings.CheckPaths) == 0 {
		settings.CheckPaths = []string{
			"coding_ethos",
			"pre-commit/hooks",
		}
	}

	if len(settings.ExcludePatterns) == 0 {
		settings.ExcludePatterns = []string{
			`__pycache__`,
			`\.venv`,
			`tests`,
			`.*_test\.py$`,
			`test_.*\.py$`,
		}
	}

	if len(settings.Command) == 0 {
		settings.Command = []string{nativeDocstringCoverageCommand}
	}

	applyDocstringCoverageFlagDefaults(settings, rootConfig)

	if settings.UseHookProject {
		return
	}

	if !configSectionFieldPresent(
		rootConfig,
		"python.docstring_coverage",
		"use_hook_project",
	) {
		settings.UseHookProject = true
	}
}

func applyDocstringCoverageFlagDefaults(
	settings *docstringCoverageSettings,
	rootConfig map[string]any,
) {
	defaultTrueIfUnset(rootConfig, "verbose", &settings.Verbose)
	defaultTrueIfUnset(rootConfig, "ignore_init_method", &settings.IgnoreInitMethod)
	defaultTrueIfUnset(rootConfig, "ignore_init_module", &settings.IgnoreInitModule)
	defaultTrueIfUnset(rootConfig, "ignore_magic", &settings.IgnoreMagic)
	defaultTrueIfUnset(rootConfig, "ignore_private", &settings.IgnorePrivate)
	defaultTrueIfUnset(rootConfig, "ignore_semiprivate", &settings.IgnoreSemiprivate)
	defaultTrueIfUnset(
		rootConfig,
		"ignore_property_decorators",
		&settings.IgnorePropertyDecorators,
	)
	defaultTrueIfUnset(
		rootConfig,
		"ignore_nested_functions",
		&settings.IgnoreNestedFunctions,
	)
	defaultTrueIfUnset(
		rootConfig,
		"ignore_nested_classes",
		&settings.IgnoreNestedClasses,
	)
}

func defaultTrueIfUnset(rootConfig map[string]any, field string, target *bool) {
	if !configSectionFieldPresent(rootConfig, "python.docstring_coverage", field) {
		*target = true
	}
}

func buildDocstringCoverageCommand(settings docstringCoverageSettings) []string {
	command := append([]string{}, settings.Command...)
	command = append(
		command,
		"--fail-under",
		strconv.Itoa(settings.Threshold),
	)

	command = appendDocstringCoverageFlags(command, settings)
	for _, pattern := range settings.ExcludePatterns {
		command = append(command, "--ignore-regex", pattern)
	}

	command = append(command, settings.CheckPaths...)
	if settings.UseHookProject {
		command = append(
			[]string{"uv", "run", "--quiet", "--project", settings.HooksProject},
			command...,
		)
	}

	return command
}

func appendDocstringCoverageFlags(
	command []string,
	settings docstringCoverageSettings,
) []string {
	command = appendFlagIfEnabled(command, settings.Verbose, "--verbose")
	command = appendFlagIfEnabled(
		command,
		settings.IgnoreInitMethod,
		"--ignore-init-method",
	)
	command = appendFlagIfEnabled(
		command,
		settings.IgnoreInitModule,
		"--ignore-init-module",
	)
	command = appendFlagIfEnabled(command, settings.IgnoreMagic, "--ignore-magic")
	command = appendFlagIfEnabled(command, settings.IgnorePrivate, "--ignore-private")
	command = appendFlagIfEnabled(
		command,
		settings.IgnoreSemiprivate,
		"--ignore-semiprivate",
	)
	command = appendFlagIfEnabled(
		command,
		settings.IgnorePropertyDecorators,
		"--ignore-property-decorators",
	)
	command = appendFlagIfEnabled(
		command,
		settings.IgnoreNestedFunctions,
		"--ignore-nested-functions",
	)
	command = appendFlagIfEnabled(
		command,
		settings.IgnoreNestedClasses,
		"--ignore-nested-classes",
	)

	return command
}

func appendFlagIfEnabled(command []string, enabled bool, flag string) []string {
	if enabled {
		return append(command, flag)
	}

	return command
}

func runDocstringCoverage(
	settings docstringCoverageSettings,
) (int, string, string, error) {
	if usesNativeDocstringCoverage(settings.Command) {
		return runNativeDocstringCoverage(settings)
	}

	command := buildDocstringCoverageCommand(settings)
	if len(command) == 0 {
		return 1, "", "", errDocstringCoverageCommandEmpty
	}

	cmd := exec.CommandContext(context.Background(), command[0], command[1:]...)
	cmd.Dir = settings.ConsumerRoot

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

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
		fmt.Fprintf(
			os.Stderr,
			"ERROR: failed to run docstring coverage command: %v\n",
			err,
		)

		return 1
	}

	if exitCode == 0 {
		return 0
	}

	_, _ = fmt.Fprintln(
		os.Stdout,
		formatDocstringCoverageFailure(
			settings,
			stdout,
			stderr,
			selectedHookOutputFormat(),
		),
	)
	if strings.TrimSpace(stderr) != "" {
		_, _ = fmt.Fprint(os.Stderr, stderr)
		if !strings.HasSuffix(stderr, "\n") {
			_, _ = fmt.Fprintln(os.Stderr)
		}
	}

	return 1
}

func formatDocstringCoverageFailure(
	settings docstringCoverageSettings,
	stdout string,
	stderr string,
	format string,
) string {
	switch format {
	case hookOutputFormatJSON:
		return formatDocstringCoverageFailureJSON(settings, stdout, stderr)
	case hookOutputFormatTOON:
		return formatDocstringCoverageFailureTOON(settings, stdout, stderr)
	default:
		return formatDocstringCoverageFailureHuman(settings, stdout)
	}
}

func docstringCoverageFailureSummary(
	settings docstringCoverageSettings,
	stdout string,
	stderr string,
	format string,
) docstringCoverageFailureReport {
	return docstringCoverageFailureReport{
		Format:    format,
		Tool:      "docstring_coverage",
		Status:    statusFail,
		Threshold: settings.Threshold,
		Paths:     settings.CheckPaths,
		Stdout:    strings.TrimSpace(stdout),
		Stderr:    strings.TrimSpace(stderr),
		Guidance: []string{
			"Every public function must have a Google-style docstring",
			"Docstrings document the contract between function and caller",
			"If behavior changes, update the docstring",
		},
	}
}

func formatDocstringCoverageFailureJSON(
	settings docstringCoverageSettings,
	stdout string,
	stderr string,
) string {
	payload := docstringCoverageFailureSummary(
		settings,
		stdout,
		stderr,
		hookOutputFormatJSON,
	)

	content, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return formatDocstringCoverageFailureHuman(settings, stdout)
	}

	return string(content)
}

func formatDocstringCoverageFailureTOON(
	settings docstringCoverageSettings,
	stdout string,
	stderr string,
) string {
	payload := docstringCoverageFailureSummary(
		settings,
		stdout,
		stderr,
		hookOutputFormatTOON,
	)

	lines := []string{
		"format: " + payload.Format,
		"tool: " + payload.Tool,
		"status: " + payload.Status,
		fmt.Sprintf("threshold: %d", payload.Threshold),
		fmt.Sprintf("paths[%d]{path}:", len(payload.Paths)),
	}
	for _, path := range payload.Paths {
		lines = append(lines, "  "+toonCell(path))
	}

	if payload.Stdout != "" {
		lines = append(lines, "stdout: "+toonCell(payload.Stdout))
	}

	if payload.Stderr != "" {
		lines = append(lines, "stderr: "+toonCell(payload.Stderr))
	}

	lines = append(
		lines,
		fmt.Sprintf("guidance[%d]{message}:", len(payload.Guidance)),
	)
	for _, item := range payload.Guidance {
		lines = append(lines, "  "+toonCell(item))
	}

	return strings.Join(lines, "\n")
}

func formatDocstringCoverageFailureHuman(
	settings docstringCoverageSettings,
	stdout string,
) string {
	lines := []string{
		strings.Repeat("=", compactDividerWidth),
		"DOCSTRING COVERAGE CHECK FAILED (ETHOS §18)",
		strings.Repeat("=", compactDividerWidth),
		fmt.Sprintf("Threshold: %d%%", settings.Threshold),
		"Paths: " + strings.Join(settings.CheckPaths, ", "),
		"",
	}
	if strings.TrimSpace(stdout) != "" {
		lines = append(lines, strings.TrimRight(stdout, "\n"))
	}

	lines = append(
		lines,
		"",
		"Per ETHOS §18 (Documentation as Contract):",
		"  - Every public function must have a Google-style docstring",
		"  - Docstrings document the contract between function and caller",
		"  - If you change behavior, update the docstring",
		"",
		fmt.Sprintf("Add docstrings to reach %d%% coverage.", settings.Threshold),
	)

	return strings.Join(lines, "\n")
}
