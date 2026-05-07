// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const pyprojectUnknownTarget = "<unknown>"

type pyprojectIgnoreFinding struct {
	Tool    string
	Setting string
	Target  string
	Detail  string
}

func EvaluatePythonPyprojectIgnores(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	for _, file := range context.Files {
		if filepath.Base(file) != "pyproject.toml" {
			continue
		}

		config, err := loadPyprojectConfig(resolveGuardPath(context.Cwd, file))
		if err != nil {
			return nil, err
		}

		findings := filterAllowedPyprojectFindings(
			extractPyprojectFindings(config),
			context.EvaluatorOptions,
		)
		if len(findings) == 0 {
			continue
		}

		decision := policy.NewDecision(blockDecision, policyDef)

		decision.Diagnostics = make([]diagnostics.Diagnostic, 0, len(findings))
		for _, finding := range findings {
			decision.Diagnostics = append(decision.Diagnostics, diagnostics.Diagnostic{
				Tool:     "pyproject_ignores",
				File:     file,
				Severity: blockDecision,
				Code:     finding.Tool + "." + finding.Setting,
				PolicyID: policyDef.ID,
				Message:  finding.Target,
				Advice:   policyDef.Suggestion,
				Metadata: map[string]any{"detail": finding.Detail},
			})
		}

		decision.Evidence = map[string]any{
			"file":     file,
			"findings": len(findings),
		}

		return []policy.Decision{decision}, nil
	}

	return nil, nil
}

func EvaluatePythonUVExcludeNewer(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	expected := strings.TrimSpace(
		stringOption(context.EvaluatorOptions, "expected_value", "7 days"),
	)

	var decisions []policy.Decision

	for _, file := range context.Files {
		if filepath.Base(file) != "pyproject.toml" {
			continue
		}

		config, err := loadPyprojectConfig(resolveGuardPath(context.Cwd, file))
		if err != nil {
			return nil, err
		}

		actual := strings.TrimSpace(pyprojectString(
			pyprojectMap(config["tool"]),
			"uv",
			"exclude-newer",
		))
		if actual == expected {
			continue
		}

		message := "pyproject.toml must set [tool.uv].exclude-newer"
		if actual != "" {
			message = "pyproject.toml has the wrong [tool.uv].exclude-newer value"
		}

		decision := policy.NewDecision(blockDecision, policyDef)
		decision.Diagnostics = []diagnostics.Diagnostic{{
			Tool:     "uv",
			File:     file,
			Severity: blockDecision,
			Code:     "uv.exclude-newer",
			PolicyID: policyDef.ID,
			Message:  message,
			Advice:   policyDef.Suggestion,
			Detail:   fmt.Sprintf("expected %q, got %q", expected, actual),
		}}
		decision.Evidence = map[string]any{
			"file":           file,
			"expected_value": expected,
			"actual_value":   actual,
		}

		decisions = append(decisions, decision)
	}

	return decisions, nil
}

func loadPyprojectConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}

		return nil, fmt.Errorf("read pyproject.toml %s: %w", path, err)
	}

	config := map[string]any{}

	inlineErr0 := toml.Unmarshal(data, &config)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("parse pyproject.toml %s: %w", path, inlineErr0)
	}

	return config, nil
}

func extractPyprojectFindings(
	config map[string]any,
) map[pyprojectIgnoreFinding]struct{} {
	toolTable := pyprojectMap(config["tool"])
	if toolTable == nil {
		return map[pyprojectIgnoreFinding]struct{}{}
	}

	findings := map[pyprojectIgnoreFinding]struct{}{}
	for finding := range extractRuffFindings(toolTable) {
		findings[finding] = struct{}{}
	}

	for finding := range extractMypyFindings(toolTable) {
		findings[finding] = struct{}{}
	}

	for finding := range extractPyrightFindings(toolTable) {
		findings[finding] = struct{}{}
	}

	for finding := range extractPylintFindings(toolTable) {
		findings[finding] = struct{}{}
	}

	return findings
}

func extractRuffFindings(toolTable map[string]any) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}

	ruff := pyprojectMap(toolTable["ruff"])
	if ruff == nil {
		return findings
	}

	lint := pyprojectMap(ruff["lint"])
	for _, key := range []string{
		"per-file-ignores",
		"extend-per-file-ignores",
		"per_file_ignores",
		"extend_per_file_ignores",
	} {
		if lint != nil {
			if value, ok := lint[key]; ok {
				addPyprojectPerFileFindings(findings, "ruff", key, value)
			}
		}

		if value, ok := ruff[key]; ok {
			addPyprojectPerFileFindings(findings, "ruff", key, value)
		}
	}

	for _, key := range []string{"exclude", "extend-exclude", "extend_exclude"} {
		if value, ok := ruff[key]; ok {
			addPyprojectPatternFindings(findings, "ruff", key, value)
		}
	}

	return findings
}

func extractMypyFindings(toolTable map[string]any) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}

	mypy := pyprojectMap(toolTable["mypy"])
	if mypy == nil {
		return findings
	}

	for _, key := range []string{"per-file-ignores", "per_file_ignores"} {
		if value, ok := mypy[key]; ok {
			addPyprojectPerFileFindings(findings, "mypy", key, value)
		}
	}

	if value, ok := mypy["exclude"]; ok {
		addPyprojectPatternFindings(findings, "mypy", "exclude", value)
	}

	if overrides, ok := mypy["overrides"].([]any); ok {
		for _, rawOverride := range overrides {
			override := pyprojectMap(rawOverride)
			if override != nil {
				addMypyOverrideFindings(findings, override)
			}
		}
	}

	return findings
}

func extractPyrightFindings(
	toolTable map[string]any,
) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}

	pyright := pyprojectMap(toolTable["pyright"])
	if pyright == nil {
		return findings
	}

	for _, key := range []string{"exclude", "ignore"} {
		if value, ok := pyright[key]; ok {
			addPyprojectPatternFindings(findings, "pyright", key, value)
		}
	}

	return findings
}

func extractPylintFindings(
	toolTable map[string]any,
) map[pyprojectIgnoreFinding]struct{} {
	findings := map[pyprojectIgnoreFinding]struct{}{}

	pylint := pyprojectMap(toolTable["pylint"])
	if pylint == nil {
		return findings
	}

	sections := []map[string]any{pylint}
	if mainSection := pyprojectMap(pylint["main"]); mainSection != nil {
		sections = append(sections, mainSection)
	}

	for _, section := range sections {
		for _, key := range []string{
			"ignore",
			"ignore-patterns",
			"ignore-paths",
			"ignore-modules",
			"ignored-modules",
		} {
			if value, ok := section[key]; ok {
				addPyprojectPatternFindings(findings, "pylint", key, value)
			}
		}
	}

	return findings
}

func addMypyOverrideFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	override map[string]any,
) {
	modules := normalizePyprojectStringList(firstNonNil(
		override["module"],
		override["modules"],
	))
	if len(modules) == 0 {
		modules = []string{pyprojectUnknownTarget}
	}

	for _, key := range []string{
		"ignore_errors",
		"ignore_missing_imports",
		"disable_error_code",
		"disable_error_codes",
	} {
		value, ok := override[key]
		if !ok {
			continue
		}

		addMypyOverrideFindingsForKey(findings, modules, key, value)
	}
}

func addMypyOverrideFindingsForKey(
	findings map[pyprojectIgnoreFinding]struct{},
	modules []string,
	key string,
	value any,
) {
	switch key {
	case "disable_error_code", "disable_error_codes":
		for _, code := range normalizePyprojectStringList(value) {
			if strings.TrimSpace(code) == "" {
				continue
			}

			for _, module := range modules {
				addPyprojectFinding(findings, "mypy", "override."+key, module, code)
			}
		}
	default:
		if boolean, ok := value.(bool); ok {
			if boolean {
				for _, module := range modules {
					addPyprojectFinding(findings, "mypy", "override."+key, module, "")
				}
			}

			return
		}

		for _, module := range modules {
			addPyprojectFinding(
				findings,
				"mypy",
				"override."+key,
				module,
				fmt.Sprint(value),
			)
		}
	}
}

func addPyprojectPerFileFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	value any,
) {
	if typed, ok := value.(map[string]any); ok {
		for pattern, codes := range typed {
			codeList := normalizePyprojectStringList(codes)
			if len(codeList) == 0 {
				addPyprojectFinding(findings, tool, setting, pattern, "<all>")

				continue
			}

			for _, code := range codeList {
				addPyprojectFinding(findings, tool, setting, pattern, code)
			}
		}

		return
	}

	for _, entry := range normalizePyprojectStringList(value) {
		addPyprojectFinding(findings, tool, setting, entry, "")
	}
}

func addPyprojectPatternFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	value any,
) {
	for _, pattern := range normalizePyprojectStringList(value) {
		addPyprojectFinding(findings, tool, setting, pattern, "")
	}
}

func addPyprojectFinding(
	findings map[pyprojectIgnoreFinding]struct{},
	tool string,
	setting string,
	target string,
	detail string,
) {
	findings[pyprojectIgnoreFinding{
		Tool:    tool,
		Setting: setting,
		Target:  target,
		Detail:  detail,
	}] = struct{}{}
}

func filterAllowedPyprojectFindings(
	findings map[pyprojectIgnoreFinding]struct{},
	options map[string]any,
) []pyprojectIgnoreFinding {
	allowedIgnore := stringSet(
		stringSliceOption(options, "allowed_ignore_patterns", nil),
	)
	allowedExclude := stringSet(
		stringSliceOption(options, "allowed_exclude_patterns", nil),
	)
	allowedMypyMissing := stringSet(
		stringSliceOption(options, "allowed_mypy_missing_imports", nil),
	)

	filtered := make([]pyprojectIgnoreFinding, 0, len(findings))
	for finding := range findings {
		if allowedIgnore[finding.Target] {
			continue
		}

		if isPyprojectExcludeSetting(finding.Setting) &&
			allowedExclude[finding.Target] {
			continue
		}

		if finding.Tool == "mypy" &&
			finding.Setting == "override.ignore_missing_imports" &&
			allowedMypyMissing[finding.Target] {
			continue
		}

		filtered = append(filtered, finding)
	}

	sort.Slice(filtered, func(left, right int) bool {
		return pyprojectFindingSortKey(filtered[left]) <
			pyprojectFindingSortKey(filtered[right])
	})

	return filtered
}

func pyprojectFindingSortKey(finding pyprojectIgnoreFinding) string {
	return fmt.Sprintf(
		"%s|%s|%s|%s",
		finding.Tool,
		finding.Setting,
		finding.Target,
		finding.Detail,
	)
}

func pyprojectMap(value any) map[string]any {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}

	return typed
}

func pyprojectString(root map[string]any, keys ...string) string {
	current := root
	for index, key := range keys {
		if current == nil {
			return ""
		}

		value, ok := current[key]
		if !ok {
			return ""
		}

		if index == len(keys)-1 {
			text, ok := value.(string)
			if !ok {
				return ""
			}

			return text
		}

		current = pyprojectMap(value)
		if current == nil {
			return ""
		}
	}

	return ""
}

func normalizePyprojectStringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		return append([]string{}, typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				items = append(items, fmt.Sprint(item))
			}
		}

		return items
	case map[string]any:
		return []string{fmt.Sprint(typed)}
	default:
		return []string{fmt.Sprint(value)}
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}

	return nil
}

func isPyprojectExcludeSetting(setting string) bool {
	return setting == "exclude" ||
		setting == "extend-exclude" ||
		setting == "extend_exclude"
}
