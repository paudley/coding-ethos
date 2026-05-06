// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolconfigs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path"
	"strconv"
	"strings"
)

const (
	HashManifestPath  = ".code-ethos/tool-config-hashes.json"
	defaultLineLength = 88
	defaultMaxArgs    = 6
	spdxCopyright     = "SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. " +
		"<paudley@blackcat.ca>"
	spdxHeader = "# " + spdxCopyright + "\n" +
		"# SPDX-License-Identifier: MIT\n\n"
)

type Renderer struct {
	Render  func(configMap) (string, error)
	Enabled func(configMap) bool
	Path    string
}

func RenderAll(config configMap) (map[string]string, error) {
	rendered := map[string]string{}

	for _, renderer := range renderers() {
		if !renderer.Enabled(config) {
			continue
		}

		content, err := renderer.Render(config)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", renderer.Path, err)
		}

		rendered[renderer.Path] = content
	}

	return rendered, nil
}

func RenderHashManifest(rendered map[string]string) (string, error) {
	configs := make(map[string]string, len(rendered))
	for name, content := range rendered {
		sum := sha256.Sum256([]byte(content))
		configs[name] = "sha256:" + hex.EncodeToString(sum[:])
	}

	payload := map[string]any{"version": 1, "configs": configs}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", fmt.Errorf("render hash manifest: %w", err)
	}

	return string(data) + "\n", nil
}

func renderers() []Renderer {
	return []Renderer{
		{Path: "pyrightconfig.json", Render: renderPyrightConfig, Enabled: alwaysEnabled},
		{Path: "mypy.ini", Render: renderMypyINI, Enabled: alwaysEnabled},
		{Path: "ruff.toml", Render: renderRuffTOML, Enabled: alwaysEnabled},
		{Path: ".pylintrc", Render: renderPylintrc, Enabled: alwaysEnabled},
		{Path: ".yamllint.yml", Render: renderYamllintConfig, Enabled: alwaysEnabled},
		{Path: ".bandit.yml", Render: renderBanditConfig, Enabled: alwaysEnabled},
		{Path: ".sqlfluff", Render: renderSQLFluffConfig, Enabled: alwaysEnabled},
		{Path: "tombi.toml", Render: renderTombiConfig, Enabled: alwaysEnabled},
		{Path: ".golangci.yml", Render: renderGolangCIConfig, Enabled: alwaysEnabled},
		{
			Path:    ".github/workflows/coding-ethos-sarif.yml",
			Render:  renderGitHubSARIFWorkflow,
			Enabled: githubCIEnabled,
		},
		{Path: ".gitlab-ci.yml", Render: renderGitLabSARIFConfig, Enabled: gitlabCIEnabled},
	}
}

func alwaysEnabled(config configMap) bool {
	_ = config

	return true
}

func githubCIEnabled(config configMap) bool {
	return configuredBool(config, "generated_config.ci.github_actions.enabled", false)
}

func gitlabCIEnabled(config configMap) bool {
	return configuredBool(config, "generated_config.ci.gitlab.enabled", false)
}

func pythonVersion(config configMap) string {
	configured := truthyString(getPath(config, "style.python_version", "3.13"))
	if configured == "" {
		return "3.13"
	}

	return configured
}

func lineLength(config configMap) int {
	return configuredInt(config, "style.line_length", defaultLineLength)
}

func ruffTargetVersion(config configMap) string {
	configured := truthyString(getPath(config, "tooling.ruff.target_version", ""))
	if configured != "" {
		return configured
	}

	return "py" + strings.ReplaceAll(pythonVersion(config), ".", "")
}

func sourcePaths(config configMap) []string {
	paths := stringList(getPath(config, "python.source_paths", []any{}))
	if len(paths) > 0 {
		return paths
	}

	return stringList(getPath(config, "python.direct_imports.packages", []any{}))
}

func testPaths(config configMap) []string {
	return stringList(getPath(config, "python.test_paths", []any{"tests"}))
}

func stubPaths(config configMap) []string {
	return stringList(getPath(config, "python.stub_paths", []any{}))
}

func extraPaths(config configMap) []string {
	return stringList(getPath(config, "python.extra_paths", []any{"."}))
}

func renderPyrightConfig(config configMap) (string, error) {
	payload := map[string]any{
		"typeCheckingMode": configuredString(
			config,
			"tooling.pyright.type_checking_mode",
			"strict",
		),
		"include": configuredList(
			config,
			"tooling.pyright.include",
			sourcePaths(config),
		),
		"exclude": stringList(
			getPath(config, "tooling.pyright.exclude", pyrightExcludes()),
		),
		"extraPaths": configuredList(
			config,
			"tooling.pyright.extra_paths",
			extraPaths(config),
		),
		"venvPath": configuredString(
			config,
			"tooling.pyright.venv_path",
			truthyString(getPath(config, "python.venv_path", ".")),
		),
		"venv": configuredString(
			config,
			"tooling.pyright.venv",
			truthyString(getPath(config, "python.venv", ".venv")),
		),
		"pythonVersion": pythonVersion(config),
	}
	if paths := stubPaths(config); len(paths) > 0 {
		if stubPath := configuredString(
			config,
			"tooling.pyright.stub_path",
			paths[0],
		); stubPath != "" {
			payload["stubPath"] = stubPath
		}
	}

	return marshalIndentedJSON(payload, []string{
		"typeCheckingMode", "include", "exclude", "extraPaths", "venvPath", "venv",
		"pythonVersion", "stubPath",
	})
}

func pyrightExcludes() []any {
	return []any{"**/tests/**", "**/*_test.py", "**/test_*.py", "**/.venv/**"}
}

func marshalIndentedJSON(payload map[string]any, order []string) (string, error) {
	var builder strings.Builder
	builder.WriteString("{\n")

	wrote := 0

	for _, key := range order {
		value, ok := payload[key]
		if !ok {
			continue
		}

		data, err := json.MarshalIndent(value, "    ", "    ")
		if err != nil {
			return "", fmt.Errorf("marshal %s: %w", key, err)
		}

		if wrote > 0 {
			builder.WriteString(",\n")
		}

		builder.WriteString("    ")
		builder.WriteString(jsonString(key))
		builder.WriteString(": ")
		builder.Write(data)

		wrote++
	}

	builder.WriteString("\n}\n")

	return finishINI(&builder), nil
}

func renderMypyINI(config configMap) (string, error) {
	values := map[string]string{
		"strict": titleBool(configuredBool(config, "tooling.mypy.strict", true)),
		"warn_unused_configs": titleBool(
			configuredBool(config, "tooling.mypy.warn_unused_configs", true),
		),
		"python_version": pythonVersion(config),
	}
	if files := configuredList(
		config,
		"tooling.mypy.files",
		sourcePaths(config),
	); len(
		files,
	) > 0 {
		values["files"] = strings.Join(files, ", ")
	}

	plugins := stringList(getPath(config, "tooling.mypy.plugins", []any{"pydantic.mypy"}))
	if len(plugins) > 0 {
		values["plugins"] = strings.Join(plugins, ", ")
	}

	if mypyPath := configuredString(
		config,
		"tooling.mypy.mypy_path",
		strings.Join(stubPaths(config), ","),
	); mypyPath != "" {
		values["mypy_path"] = mypyPath
	}

	if exclude := mypyExcludeRegex(config); exclude != "" {
		values["exclude"] = exclude
	}

	var builder strings.Builder
	builder.WriteString(spdxHeader)
	writeINISection(&builder, "mypy", values, []string{
		"strict", "warn_unused_configs", "python_version", "files", "plugins", "mypy_path",
		"exclude",
	})

	if containsString(plugins, "pydantic.mypy") {
		writeINISection(&builder, "pydantic-mypy", map[string]string{
			"init_forbid_extra":             "True",
			"init_typed":                    "True",
			"warn_required_dynamic_aliases": "True",
		}, []string{"init_forbid_extra", "init_typed", "warn_required_dynamic_aliases"})
	}

	return finishINI(&builder), nil
}

func mypyExcludeRegex(config configMap) string {
	return strings.Join(stringList(getPath(config, "tooling.mypy.exclude_patterns", []any{
		`(^|/)tests/`,
		`(^|/).*_test\.py$`,
		`(^|/)test_.*\.py$`,
		`(^|/)\.venv/`,
	})), "|")
}

func renderRuffTOML(config configMap) (string, error) {
	lines := []string{
		"target-version = " + jsonString(ruffTargetVersion(config)),
		fmt.Sprintf("line-length = %d", lineLength(config)),
	}
	if exclude := stringList(
		getPath(config, "tooling.ruff.exclude", defaultToolExcludes()),
	); len(
		exclude,
	) > 0 {
		lines = append(lines, "exclude = "+tomlList(exclude))
	}

	lines = append(
		lines,
		"",
		"[lint]",
		"select = "+tomlList(
			stringList(getPath(config, "tooling.ruff.select", []any{"ALL"})),
		),
		"ignore = "+tomlList(stringList(getPath(config, "tooling.ruff.ignore", []any{}))),
		"",
		"[lint.pylint]",
		fmt.Sprintf(
			"max-args = %d",
			configuredInt(config, "tooling.ruff.max_args", defaultMaxArgs),
		),
	)
	if ignores := ruffPerFileIgnores(config); len(ignores) > 0 {
		lines = append(lines, "", "[lint.per-file-ignores]")
		for _, pattern := range sortedKeys(ignores) {
			lines = append(lines, jsonString(pattern)+" = "+tomlList(ignores[pattern]))
		}
	}

	if banned := mappingValue(config, "tooling.ruff.banned_api"); len(banned) > 0 {
		lines = append(lines, "", "[lint.flake8-tidy-imports.banned-api]")

		for _, moduleName := range sortedKeysAny(banned) {
			message := truthyString(banned[moduleName])
			if message != "" {
				lines = append(lines, fmt.Sprintf(
					"%s = { msg = %s }",
					jsonString(moduleName),
					jsonString(message),
				))
			}
		}
	}

	return spdxHeader + strings.Join(lines, "\n") + "\n", nil
}

func defaultToolExcludes() []any {
	return []any{
		".git",
		".venv",
		".mypy_cache",
		".ruff_cache",
		"__pycache__",
		"*.egg-info",
		".eggs",
		"build",
		"dist",
		"node_modules",
	}
}

func pathPatterns(paths []string) []string {
	patterns := []string{}

	for _, raw := range paths {
		clean := strings.Trim(strings.TrimSpace(raw), "/")
		if clean == "" {
			continue
		}

		if strings.HasSuffix(clean, "*.py") || strings.HasSuffix(clean, "*.pyi") {
			patterns = append(patterns, clean)
		} else {
			patterns = append(patterns, clean+"/**")
		}
	}

	return patterns
}

func ruffPerFileIgnores(config configMap) map[string][]string {
	ignores := map[string][]string{}
	for _, pattern := range pathPatterns(stubPaths(config)) {
		ignores[pattern] = stringList(
			getPath(config, "tooling.ruff.stub_per_file_ignores", []any{}),
		)
	}

	for _, pattern := range pathPatterns(testPaths(config)) {
		ignores[pattern] = stringList(
			getPath(config, "tooling.ruff.test_per_file_ignores", []any{}),
		)
	}

	maps.Copy(ignores, sqlIgnorePatterns(config))

	for pattern, codes := range mappingValue(
		config,
		"tooling.ruff.extra_per_file_ignores",
	) {
		ignores[pattern] = stringList(codes)
	}

	for pattern, codes := range ignores {
		if len(codes) == 0 {
			delete(ignores, pattern)
		}
	}

	return ignores
}

func sqlIgnorePatterns(config configMap) map[string][]string {
	if !configuredBool(config, "python.sql_centralization.enabled", false) {
		return nil
	}

	ignoreCodes := stringList(
		getPath(config, "tooling.ruff.sql_per_file_ignores", []any{"S608"}),
	)
	if len(ignoreCodes) == 0 {
		return nil
	}

	patterns := map[string][]string{}

	for _, raw := range stringList(
		getPath(config, "python.sql_centralization.central_paths", []any{}),
	) {
		clean := strings.Trim(strings.TrimSpace(raw), "/")
		if clean == "" {
			continue
		}

		pattern := clean
		if !strings.Contains(path.Base(clean), ".") {
			pattern = clean + "/**"
		}

		patterns[pattern] = append([]string(nil), ignoreCodes...)
	}

	return patterns
}

func renderPylintrc(config configMap) (string, error) {
	var builder strings.Builder
	builder.WriteString(spdxHeader)
	writeINISection(&builder, "MAIN", map[string]string{
		"jobs":       strconv.Itoa(configuredInt(config, "tooling.pylint.jobs", 0)),
		"persistent": yesNo(configuredBool(config, "tooling.pylint.persistent", false)),
		"ignore": strings.Join(
			stringList(getPath(config, "tooling.pylint.ignore", []any{})),
			",",
		),
		"ignore-paths": strings.Join(
			stringList(getPath(config, "tooling.pylint.ignore_paths", []any{})),
			",",
		),
	}, []string{"jobs", "persistent", "ignore", "ignore-paths"})
	writeINISection(&builder, "MESSAGES CONTROL", map[string]string{
		"disable": strings.Join(
			stringList(getPath(config, "tooling.pylint.disable", []any{})),
			",",
		),
	}, []string{"disable"})
	writeINISection(&builder, "REPORTS", map[string]string{
		"reports": yesNo(configuredBool(config, "tooling.pylint.reports", false)),
		"score":   yesNo(configuredBool(config, "tooling.pylint.score", false)),
	}, []string{"reports", "score"})
	writeINISection(&builder, "FORMAT", map[string]string{
		"max-line-length": strconv.Itoa(
			configuredInt(config, "tooling.pylint.max_line_length", lineLength(config)),
		),
	}, []string{"max-line-length"})
	writeINISection(&builder, "DESIGN", map[string]string{
		"max-args": strconv.Itoa(
			configuredInt(config, "tooling.pylint.max_args", defaultMaxArgs),
		),
	}, []string{"max-args"})

	if names := stringList(
		getPath(config, "tooling.pylint.good_names", []any{}),
	); len(
		names,
	) > 0 {
		writeINISection(
			&builder,
			"BASIC",
			map[string]string{"good-names": strings.Join(names, ",")},
			[]string{"good-names"},
		)
	}

	return finishINI(&builder), nil
}

func renderYamllintConfig(config configMap) (string, error) {
	rules := cloneMap(mappingValue(config, "tooling.yamllint.rules"))
	lineLengthRule := cloneMap(mapValue(rules["line-length"]))
	lineLengthRule["max"] = lineLength(config)
	rules["line-length"] = lineLengthRule
	payload := orderedMap{
		{
			Key:   "extends",
			Value: configuredString(config, "tooling.yamllint.extends", "default"),
		},
		{Key: "rules", Value: rules},
	}

	return spdxHeader + renderYAML(payload), nil
}
