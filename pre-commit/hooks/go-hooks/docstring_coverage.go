// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
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

func configSectionFieldPresent(
	rootConfig map[string]any,
	path string,
	field string,
) bool {
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
		settings.Command = []string{"interrogate"}
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
	command := buildDocstringCoverageCommand(settings)
	if len(command) == 0 {
		return 1, "", "", errDocstringCoverageCommandEmpty
	}
	cmd := exec.CommandContext(context.Background(), command[0], command[1:]...)
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

	_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("=", compactDividerWidth))
	_, _ = fmt.Fprintln(os.Stdout, "DOCSTRING COVERAGE CHECK FAILED (ETHOS §18)")
	_, _ = fmt.Fprintln(os.Stdout, strings.Repeat("=", compactDividerWidth))
	_, _ = fmt.Fprintf(os.Stdout, "Threshold: %d%%\n", settings.Threshold)
	_, _ = fmt.Fprintf(
		os.Stdout,
		"Paths: %s\n",
		strings.Join(settings.CheckPaths, ", "),
	)
	_, _ = fmt.Fprintln(os.Stdout)
	if strings.TrimSpace(stdout) != "" {
		_, _ = fmt.Fprint(os.Stdout, stdout)
		if !strings.HasSuffix(stdout, "\n") {
			_, _ = fmt.Fprintln(os.Stdout)
		}
	}
	if strings.TrimSpace(stderr) != "" {
		_, _ = fmt.Fprint(os.Stderr, stderr)
		if !strings.HasSuffix(stderr, "\n") {
			_, _ = fmt.Fprintln(os.Stderr)
		}
	}
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintln(os.Stdout, "Per ETHOS §18 (Documentation as Contract):")
	_, _ = fmt.Fprintln(
		os.Stdout,
		"  - Every public function must have a Google-style docstring",
	)
	_, _ = fmt.Fprintln(
		os.Stdout,
		"  - Docstrings document the contract between function and caller",
	)
	_, _ = fmt.Fprintln(os.Stdout, "  - If you change behavior, update the docstring")
	_, _ = fmt.Fprintln(os.Stdout)
	_, _ = fmt.Fprintf(
		os.Stdout,
		"Add docstrings to reach %d%% coverage.\n",
		settings.Threshold,
	)

	return 1
}
