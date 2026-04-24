package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

var pythonRequirementPattern = regexp.MustCompile(`(?:>=|~=|==)\s*([0-9]+\.[0-9]+)`)

type pythonVersionConsistencySettings struct {
	Enabled bool `json:"enabled"`
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

func loadPythonVersionConsistencySettings() (pythonVersionConsistencySettings, string, string, error) {
	var settings pythonVersionConsistencySettings
	bundleRoot, consumer, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return settings, "", "", err
	}
	_ = bundleRoot
	if err := decodeConfigSection(rootConfig, "python.version_consistency", &settings); err != nil {
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
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

func readPlainTrimmed(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
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
		return "", err
	}
	currentSection := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			currentSection = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]"))
			continue
		}
		if currentSection != "mypy" {
			continue
		}
		if key, value, ok := strings.Cut(trimmed, "="); ok && strings.TrimSpace(key) == "python_version" {
			return strings.TrimSpace(value), nil
		}
	}
	return "", nil
}

func readPyrightPythonVersion(path string) (string, error) {
	var payload map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return normalizedConfigString(payload["pythonVersion"]), nil
}

func readRuffTargetVersion(path string) (string, error) {
	var payload map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if err := toml.Unmarshal(data, &payload); err != nil {
		return "", err
	}
	return normalizedConfigString(payload["target-version"]), nil
}

func collectPythonVersionIssues(expected string, consumerRoot string) ([]pythonVersionIssue, error) {
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

	pythonVersionFile := filepath.Join(consumerRoot, ".python-version")
	if _, err := os.Stat(pythonVersionFile); err == nil {
		found, err := readPlainTrimmed(pythonVersionFile)
		if err != nil {
			return nil, err
		}
		if found != expected {
			addIssue(".python-version", "version", found, expected)
		}
	}

	pyprojectPath := filepath.Join(consumerRoot, "pyproject.toml")
	if _, err := os.Stat(pyprojectPath); err == nil {
		found, err := readPyprojectRequiresPython(pyprojectPath)
		if err != nil {
			return nil, err
		}
		minimum := pythonVersionRequirementValue(found)
		if minimum != expected {
			addIssue("pyproject.toml", "project.requires-python", found, ">="+expected)
		}
	}

	mypyPath := filepath.Join(consumerRoot, "mypy.ini")
	if _, err := os.Stat(mypyPath); err == nil {
		found, err := readMypyPythonVersion(mypyPath)
		if err != nil {
			return nil, err
		}
		if found != expected {
			addIssue("mypy.ini", "mypy.python_version", found, expected)
		}
	}

	pyrightPath := filepath.Join(consumerRoot, "pyrightconfig.json")
	if _, err := os.Stat(pyrightPath); err == nil {
		found, err := readPyrightPythonVersion(pyrightPath)
		if err != nil {
			return nil, err
		}
		if found != expected {
			addIssue("pyrightconfig.json", "pythonVersion", found, expected)
		}
	}

	ruffPath := filepath.Join(consumerRoot, "ruff.toml")
	if _, err := os.Stat(ruffPath); err == nil {
		found, err := readRuffTargetVersion(ruffPath)
		if err != nil {
			return nil, err
		}
		want := "py" + strings.ReplaceAll(expected, ".", "")
		if found != want {
			addIssue("ruff.toml", "target-version", found, want)
		}
	}

	return issues, nil
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

	fmt.Fprintf(os.Stderr, "\n%s\n", strings.Repeat("=", 70))
	fmt.Fprintln(os.Stderr, "PYTHON VERSION CONSISTENCY CHECK FAILED")
	fmt.Fprintf(os.Stderr, "%s\n\n", strings.Repeat("=", 70))
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
	fmt.Fprintln(os.Stderr, "  1. Update .python-version and pyproject.toml to match style.python_version.")
	fmt.Fprintln(os.Stderr, "  2. Run `make sync-tool-configs` to refresh mypy.ini, pyrightconfig.json, and ruff.toml.")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s\n", strings.Repeat("=", 70))
	return 1
}
