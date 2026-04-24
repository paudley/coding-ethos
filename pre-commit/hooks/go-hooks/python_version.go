// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var pythonRequirementPattern = regexp.MustCompile(`(?:>=|~=|==)\s*([0-9]+\.[0-9]+)`)

type pythonVersionConsistencySettings struct {
	Enabled bool
}

type pythonVersionIssue struct {
	Path   string
	Field  string
	Expect string
	Found  string
}

func normalizedConfigString(value any) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(fmt.Sprint(value))
}

func loadPythonVersionConsistencySettings() (
	pythonVersionConsistencySettings,
	string,
	string,
	error,
) {
	var settings pythonVersionConsistencySettings
	bundleRoot, consumer, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, "", "", err
	}
	_ = bundleRoot
	err = decodeConfigSection(rootConfig, "python.version_consistency", &settings)
	if err != nil {
		return settings, "", "", fmt.Errorf("parse version_consistency config: %w", err)
	}
	expectedValue, _ := rootConfigValue(rootConfig, "style.python_version")
	expected := strings.TrimSpace(fmt.Sprint(firstNonNil(expectedValue, "3.13")))
	if expected == "" || expected == "<nil>" {
		expected = "3.13"
	}

	return settings, expected, consumer, nil
}

func pyupgradeFlagForVersion(version string) string {
	return "--py" + strings.ReplaceAll(strings.TrimSpace(version), ".", "") + "-plus"
}

func pythonVersionRequirementValue(spec string) string {
	match := pythonRequirementPattern.FindStringSubmatch(strings.TrimSpace(spec))
	if len(match) == pythonVersionMatchParts {
		return match[1]
	}

	return ""
}

func readPlainTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}

	return strings.TrimSpace(string(data)), nil
}

func readPyprojectRequiresPython(path string) (string, error) {
	config, err := loadPyprojectConfig(path)
	if err != nil {
		return "", err
	}
	project := pyprojectMap(config["project"])
	if project == nil {
		return "", nil
	}

	return normalizedConfigString(project["requires-python"]), nil
}

func readMypyPythonVersion(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	currentSection := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") ||
			strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentSection = strings.TrimSpace(
				strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"),
			)

			continue
		}
		if currentSection != "mypy" {
			continue
		}
		if key, value, ok := strings.Cut(trimmed, "="); ok &&
			strings.TrimSpace(key) == "python_version" {
			return strings.TrimSpace(value), nil
		}
	}

	return "", nil
}

func readPyrightPythonVersion(path string) (string, error) {
	var payload map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	err = json.Unmarshal(data, &payload)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}

	return normalizedConfigString(payload["pythonVersion"]), nil
}

func readRuffTargetVersion(path string) (string, error) {
	var payload map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	err = toml.Unmarshal(data, &payload)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", path, err)
	}

	return normalizedConfigString(payload["target-version"]), nil
}

func collectPythonVersionIssues(
	expected string,
	consumerRoot string,
) ([]pythonVersionIssue, error) {
	issues := make([]pythonVersionIssue, 0)
	addIssue := func(path string, field string, found string, want string) {
		if strings.TrimSpace(found) == "" {
			found = "<missing>"
		}
		issues = append(issues, pythonVersionIssue{
			Path:   path,
			Field:  field,
			Expect: want,
			Found:  found,
		})
	}

	wantRuffVersion := "py" + strings.ReplaceAll(expected, ".", "")
	specs := []struct {
		Read            func(string) (string, error)
		Normalize       func(string) string
		RelativePath    string
		Field           string
		Expected        string
		DisplayExpected string
	}{
		{
			RelativePath: ".python-version",
			Field:        "version",
			Expected:     expected,
			Read:         readPlainTrimmed,
		},
		{
			RelativePath:    "pyproject.toml",
			Field:           "project.requires-python",
			Expected:        expected,
			DisplayExpected: ">=" + expected,
			Read:            readPyprojectRequiresPython,
			Normalize:       pythonVersionRequirementValue,
		},
		{
			RelativePath: "mypy.ini",
			Field:        "mypy.python_version",
			Expected:     expected,
			Read:         readMypyPythonVersion,
		},
		{
			RelativePath: "pyrightconfig.json",
			Field:        "pythonVersion",
			Expected:     expected,
			Read:         readPyrightPythonVersion,
		},
		{
			RelativePath: "ruff.toml",
			Field:        "target-version",
			Expected:     wantRuffVersion,
			Read:         readRuffTargetVersion,
		},
	}
	for _, spec := range specs {
		err := appendPythonVersionIssue(
			consumerRoot,
			spec.RelativePath,
			spec.Field,
			spec.Expected,
			spec.Read,
			spec.Normalize,
			firstNonEmpty(spec.DisplayExpected, spec.Expected),
			addIssue,
		)
		if err != nil {
			return nil, err
		}
	}

	return issues, nil
}

func appendPythonVersionIssue(
	consumerRoot string,
	relativePath string,
	field string,
	expected string,
	read func(string) (string, error),
	normalize func(string) string,
	displayExpected string,
	addIssue func(string, string, string, string),
) error {
	fullPath := filepath.Join(consumerRoot, relativePath)
	_, err := os.Stat(fullPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}

		return fmt.Errorf("stat %s: %w", fullPath, err)
	}
	found, err := read(fullPath)
	if err != nil {
		return err
	}
	comparison := found
	if normalize != nil {
		comparison = normalize(found)
	}
	if comparison == expected {
		return nil
	}
	addIssue(relativePath, field, found, displayExpected)

	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func checkPythonVersionConsistencyCommand(_ Config, _ []string) int {
	settings, expected, consumerRoot, err := loadPythonVersionConsistencySettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	if !settings.Enabled {
		return 0
	}

	issues, err := collectPythonVersionIssues(expected, consumerRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: python version consistency: %v\n", err)

		return 1
	}
	if len(issues) == 0 {
		return 0
	}

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintln(os.Stderr, "PYTHON VERSION CONSISTENCY CHECK FAILED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", reportDividerWidth))
	fmt.Fprintf(os.Stderr, "Configured style.python_version: %s\n\n", expected)
	fmt.Fprintln(os.Stderr, "Mismatches found:")
	for _, issue := range issues {
		fmt.Fprintf(
			os.Stderr,
			"  %s [%s]: found %q, expected %q\n",
			issue.Path,
			issue.Field,
			issue.Found,
			issue.Expect,
		)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "How to fix:")
	fmt.Fprintln(
		os.Stderr,
		"  1. Update .python-version and pyproject.toml to match style.python_version.",
	)
	fmt.Fprintln(
		os.Stderr,
		"  2. Run `make sync-tool-configs` to refresh mypy.ini, "+
			"pyrightconfig.json, and ruff.toml.",
	)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", reportDividerWidth))

	return 1
}
