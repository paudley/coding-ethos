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
	Runtime              Runtime  `json:"runtime"`
	RepoConfig           string   `json:"repo_config,omitempty"`
	FallbackBundleConfig string   `json:"fallback_bundle_config,omitempty"`
	Command              []string `json:"command"`
	ConfigFlags          []string `json:"config_flags,omitempty"`
	PostConfigArgs       []string `json:"post_config_args,omitempty"`
	PassFilesAsArgs      bool     `json:"pass_files_as_args"`
	UseHookProject       bool     `json:"use_hook_project"`
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
		Name:    "ruff",
		Parser:  "ruff",
		Runtime: RuntimePython,
		Command: []string{
			"ruff",
			"check",
			"--quiet",
			"--ignore-noqa",
			"--output-format",
			"json",
		},
		ConfigFlags:      []string{"--config"},
		RepoConfig:       "ruff.toml",
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func pyrightTool() Tool {
	return Tool{
		Name:                 "pyright",
		Parser:               "pyright",
		Runtime:              RuntimePython,
		Command:              []string{"pyright", "--outputjson"},
		ConfigFlags:          []string{"--project", "-p"},
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
		Runtime:              RuntimePython,
		Command:              []string{"mypy", "--output", "json"},
		ConfigFlags:          []string{"--config-file"},
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
		Runtime:          RuntimePython,
		Command:          []string{"pylint", "--output-format=json"},
		ConfigFlags:      []string{"--rcfile"},
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
		Runtime:          RuntimeBinary,
		Command:          []string{"hadolint", "--format", "json"},
		PassFilesAsArgs:  true,
		EnabledByDefault: true,
	}
}

func actionlintTool() Tool {
	return Tool{
		Name:             "actionlint",
		Parser:           "actionlint",
		Runtime:          RuntimeBinary,
		Command:          []string{"actionlint", "-format", "{{json .}}"},
		PassFilesAsArgs:  false,
		EnabledByDefault: true,
	}
}

func shellcheckTool() Tool {
	return Tool{
		Name:             "shellcheck",
		Parser:           "shellcheck",
		Runtime:          RuntimeBinary,
		Command:          []string{"shellcheck", "--severity=warning", "-x", "--format=json"},
		PassFilesAsArgs:  true,
		EnabledByDefault: true,
	}
}

func yamllintTool() Tool {
	return Tool{
		Name:             "yamllint",
		Parser:           "yamllint",
		Runtime:          RuntimeUV,
		Command:          []string{"yamllint"},
		ConfigFlags:      []string{"-c"},
		RepoConfig:       ".yamllint.yml",
		PostConfigArgs:   []string{"--strict", "-f", "parsable"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func golangciLintTool() Tool {
	return Tool{
		Name:        "golangci-lint",
		Parser:      "golangci-lint",
		Runtime:     RuntimeGo,
		Command:     []string{"golangci-lint", "run"},
		ConfigFlags: []string{"--config"},
		RepoConfig:  ".golangci.yml",
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
	tool.PostConfigArgs = append([]string(nil), tool.PostConfigArgs...)

	return tool
}
