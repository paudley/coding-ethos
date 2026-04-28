// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package toolcatalog defines typed external tool metadata.
package toolcatalog

type Runtime string

const (
	RuntimeBinary Runtime = "binary"
	RuntimeGo     Runtime = "go"
	RuntimePython Runtime = "python"
	RuntimeUV     Runtime = "uv"
)

type Tool struct {
	Name                 string   `json:"name"`
	Parser               string   `json:"parser"`
	Category             string   `json:"category"`
	OutputFormat         string   `json:"output_format"`
	Advice               string   `json:"advice,omitempty"`
	Runtime              Runtime  `json:"runtime"`
	RepoConfig           string   `json:"repo_config,omitempty"`
	FallbackBundleConfig string   `json:"fallback_bundle_config,omitempty"`
	Command              []string `json:"command"`
	ConfigFlags          []string `json:"config_flags,omitempty"`
	FileExtensions       []string `json:"file_extensions,omitempty"`
	FilePrefixes         []string `json:"file_prefixes,omitempty"`
	BaseNamePrefixes     []string `json:"base_name_prefixes,omitempty"`
	Languages            []string `json:"languages,omitempty"`
	PostConfigArgs       []string `json:"post_config_args,omitempty"`
	PassFilesAsArgs      bool     `json:"pass_files_as_args"`
	UseHookProject       bool     `json:"use_hook_project"`
	Fast                 bool     `json:"fast"`
	EnabledByDefault     bool     `json:"enabled_by_default"`
}

func PythonStaticTools() []Tool {
	return cloneTools(pythonStaticToolDefinitions())
}

func PythonStaticTool(name string) (Tool, bool) {
	return findTool(name, pythonStaticToolDefinitions())
}

func ToolchainTools() []Tool {
	return cloneTools(toolchainToolDefinitions())
}

func ToolchainTool(name string) (Tool, bool) {
	return findTool(name, toolchainToolDefinitions())
}

func pythonStaticToolDefinitions() []Tool {
	return []Tool{
		ruffTool(),
		pyrightTool(),
		mypyTool(),
		pylintTool(),
	}
}

func toolchainToolDefinitions() []Tool {
	return []Tool{
		hadolintTool(),
		actionlintTool(),
		shellcheckTool(),
		yamllintTool(),
		golangciLintTool(),
	}
}

func ruffTool() Tool {
	return Tool{
		Name:         "ruff",
		Parser:       "ruff",
		Category:     "python-static",
		OutputFormat: "json",
		Advice:       "Fix Ruff diagnostics structurally; do not suppress unless the policy requires a documented exception.",
		Runtime:      RuntimePython,
		Command: []string{
			"ruff",
			"check",
			"--quiet",
			"--ignore-noqa",
			"--output-format",
			"json",
		},
		ConfigFlags:      []string{"--config"},
		Languages:        []string{"python"},
		RepoConfig:       "ruff.toml",
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func pyrightTool() Tool {
	return Tool{
		Name:                 "pyright",
		Parser:               "pyright",
		Category:             "python-static",
		OutputFormat:         "json",
		Advice:               "Fix Pyright type diagnostics with precise interfaces and validated imports.",
		Runtime:              RuntimePython,
		Command:              []string{"pyright", "--outputjson"},
		ConfigFlags:          []string{"--project", "-p"},
		Languages:            []string{"python"},
		RepoConfig:           "pyrightconfig.json",
		FallbackBundleConfig: "hooks/pyproject.toml",
		PassFilesAsArgs:      true,
		UseHookProject:       true,
		EnabledByDefault:     true,
	}
}

func mypyTool() Tool {
	return Tool{
		Name:                 "mypy",
		Parser:               "mypy",
		Category:             "python-static",
		OutputFormat:         "json",
		Advice:               "Fix mypy diagnostics with explicit types rather than weakening required behavior.",
		Runtime:              RuntimePython,
		Command:              []string{"mypy", "--output", "json"},
		ConfigFlags:          []string{"--config-file"},
		Languages:            []string{"python"},
		RepoConfig:           "mypy.ini",
		FallbackBundleConfig: "hooks/pyproject.toml",
		PassFilesAsArgs:      true,
		UseHookProject:       true,
		EnabledByDefault:     true,
	}
}

func pylintTool() Tool {
	return Tool{
		Name:             "pylint",
		Parser:           "pylint",
		Category:         "python-static",
		OutputFormat:     "json",
		Advice:           "Fix Pylint findings by simplifying structure before considering local disables.",
		Runtime:          RuntimePython,
		Command:          []string{"pylint", "--output-format=json"},
		ConfigFlags:      []string{"--rcfile"},
		Languages:        []string{"python"},
		RepoConfig:       ".pylintrc",
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: false,
	}
}

func hadolintTool() Tool {
	return Tool{
		Name:             "hadolint",
		Parser:           "hadolint",
		Category:         "docker",
		OutputFormat:     "json",
		Advice:           "Fix Dockerfile findings with explicit, reproducible image and package choices.",
		Runtime:          RuntimeBinary,
		Command:          []string{"hadolint", "--format", "json"},
		BaseNamePrefixes: []string{"Dockerfile"},
		Languages:        []string{"dockerfile"},
		PassFilesAsArgs:  true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func actionlintTool() Tool {
	return Tool{
		Name:             "actionlint",
		Parser:           "actionlint",
		Category:         "workflow",
		OutputFormat:     "json-lines",
		Advice:           "Fix workflow syntax and action usage before relying on CI behavior.",
		Runtime:          RuntimeBinary,
		Command:          []string{"actionlint", "-format", "{{json .}}"},
		FileExtensions:   []string{".yaml", ".yml"},
		FilePrefixes:     []string{".github/workflows/"},
		Languages:        []string{"github-actions", "yaml"},
		PassFilesAsArgs:  false,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func shellcheckTool() Tool {
	return Tool{
		Name:             "shellcheck",
		Parser:           "shellcheck",
		Category:         "shell",
		OutputFormat:     "json",
		Advice:           "Fix shell diagnostics with quoted variables, explicit error handling, and portable constructs.",
		Runtime:          RuntimeBinary,
		Command:          []string{"shellcheck", "--severity=warning", "-x", "--format=json"},
		FileExtensions:   []string{".sh", ".bash", ".zsh", ".ksh"},
		Languages:        []string{"shell"},
		PassFilesAsArgs:  true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func yamllintTool() Tool {
	return Tool{
		Name:             "yamllint",
		Parser:           "yamllint",
		Category:         "syntax",
		OutputFormat:     "parsable",
		Advice:           "Fix YAML structure, indentation, and style before generated config consumers read it.",
		Runtime:          RuntimeUV,
		Command:          []string{"yamllint"},
		ConfigFlags:      []string{"-c"},
		FileExtensions:   []string{".yaml", ".yml"},
		Languages:        []string{"yaml"},
		RepoConfig:       ".yamllint.yml",
		PostConfigArgs:   []string{"--strict", "-f", "parsable"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func golangciLintTool() Tool {
	return Tool{
		Name:         "golangci-lint",
		Parser:       "golangci-lint",
		Category:     "go-static",
		OutputFormat: "json",
		Advice:       "Fix Go lint findings structurally; do not add exclusions or weaken the shared linter policy.",
		Runtime:      RuntimeGo,
		Command:      []string{"golangci-lint", "run"},
		ConfigFlags:  []string{"--config"},
		FileExtensions: []string{
			".go",
		},
		Languages:  []string{"go"},
		RepoConfig: ".golangci.yml",
		PostConfigArgs: []string{
			"--output.json.path",
			"stdout",
			"--output.text.path",
			"/dev/null",
		},
		PassFilesAsArgs:  false,
		EnabledByDefault: true,
	}
}

func findTool(name string, tools []Tool) (Tool, bool) {
	for _, tool := range tools {
		if tool.Name == name {
			return tool.clone(), true
		}
	}

	return Tool{}, false
}

func cloneTools(tools []Tool) []Tool {
	clones := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		clones = append(clones, tool.clone())
	}

	return clones
}

func (tool Tool) clone() Tool {
	tool.Command = append([]string(nil), tool.Command...)
	tool.ConfigFlags = append([]string(nil), tool.ConfigFlags...)
	tool.FileExtensions = append([]string(nil), tool.FileExtensions...)
	tool.FilePrefixes = append([]string(nil), tool.FilePrefixes...)
	tool.BaseNamePrefixes = append([]string(nil), tool.BaseNamePrefixes...)
	tool.Languages = append([]string(nil), tool.Languages...)
	tool.PostConfigArgs = append([]string(nil), tool.PostConfigArgs...)

	return tool
}
