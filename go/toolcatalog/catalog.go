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
	CaptureOutputArgs    []string `json:"capture_output_args,omitempty"`
	CaptureStripArgs     []string `json:"capture_strip_args,omitempty"`
	CaptureStripFlags    []string `json:"capture_strip_flags,omitempty"`
	CaptureAfterFirst    []string `json:"capture_after_first,omitempty"`
	CaptureMutatingArgs  []string `json:"capture_mutating_args,omitempty"`
	CaptureMutatingFirst []string `json:"capture_mutating_first,omitempty"`
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

func HookOwnedTools() []Tool {
	tools := append(PythonStaticTools(), ToolchainTools()...)
	tools = append(tools, hookOwnedToolDefinitions()...)

	return cloneTools(tools)
}

func HookOwnedTool(name string) (Tool, bool) {
	return findTool(name, HookOwnedTools())
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
		shfmtTool(),
		yamllintTool(),
		golangciLintTool(),
	}
}

func hookOwnedToolDefinitions() []Tool {
	return []Tool{
		pyupgradeTool(),
		ruffFormatTool(),
		ruffAutofixTool(),
		gofmtTool(),
		goVetTool(),
		goTestTool(),
		radonComplexityTool(),
		radonMaintainabilityTool(),
		vultureTool(),
		interrogateTool(),
		pytestGateTool(),
		geminiCheckTool(),
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
		CaptureOutputArgs:    []string{"--output-format=json"},
		CaptureStripArgs:     []string{"--output-format"},
		CaptureAfterFirst:    []string{"check"},
		CaptureMutatingArgs:  []string{"--fix", "--fix-only", "--unsafe-fixes"},
		CaptureMutatingFirst: []string{"format"},
		ConfigFlags:          []string{"--config"},
		Languages:            []string{"python"},
		RepoConfig:           "ruff.toml",
		PassFilesAsArgs:      true,
		UseHookProject:       true,
		Fast:                 true,
		EnabledByDefault:     true,
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
		CaptureOutputArgs:    []string{"--outputjson"},
		CaptureStripFlags:    []string{"--outputjson"},
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
		CaptureOutputArgs:    []string{"--output=json"},
		CaptureStripArgs:     []string{"--output", "-O"},
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
		Name:              "pylint",
		Parser:            "pylint",
		Category:          "python-static",
		OutputFormat:      "json",
		Advice:            "Fix Pylint findings by simplifying structure before considering local disables.",
		Runtime:           RuntimePython,
		Command:           []string{"pylint", "--output-format=json"},
		CaptureOutputArgs: []string{"--output-format=json"},
		CaptureStripArgs:  []string{"--output-format", "-f"},
		ConfigFlags:       []string{"--rcfile"},
		Languages:         []string{"python"},
		RepoConfig:        ".pylintrc",
		PassFilesAsArgs:   true,
		UseHookProject:    true,
		EnabledByDefault:  false,
	}
}

func hadolintTool() Tool {
	return Tool{
		Name:              "hadolint",
		Parser:            "hadolint",
		Category:          "docker",
		OutputFormat:      "json",
		Advice:            "Fix Dockerfile findings with explicit, reproducible image and package choices.",
		Runtime:           RuntimeBinary,
		Command:           []string{"hadolint", "--format", "json"},
		CaptureOutputArgs: []string{"--format", "json"},
		CaptureStripArgs:  []string{"--format", "-f"},
		BaseNamePrefixes:  []string{"Dockerfile"},
		Languages:         []string{"dockerfile"},
		PassFilesAsArgs:   true,
		Fast:              true,
		EnabledByDefault:  true,
	}
}

func actionlintTool() Tool {
	return Tool{
		Name:                "actionlint",
		Parser:              "actionlint",
		Category:            "workflow",
		OutputFormat:        "json-lines",
		Advice:              "Fix workflow syntax and action usage before relying on CI behavior.",
		Runtime:             RuntimeBinary,
		Command:             []string{"actionlint", "-format", "{{json .}}"},
		CaptureOutputArgs:   []string{"-format", "{{json .}}"},
		CaptureStripArgs:    []string{"-format"},
		CaptureMutatingArgs: []string{"-init-config"},
		FileExtensions:      []string{".yaml", ".yml"},
		FilePrefixes:        []string{".github/workflows/"},
		Languages:           []string{"github-actions", "yaml"},
		PassFilesAsArgs:     false,
		Fast:                true,
		EnabledByDefault:    true,
	}
}

func shellcheckTool() Tool {
	return Tool{
		Name:              "shellcheck",
		Parser:            "shellcheck",
		Category:          "shell",
		OutputFormat:      "json",
		Advice:            "Fix shell diagnostics with quoted variables, explicit error handling, and portable constructs.",
		Runtime:           RuntimeBinary,
		Command:           []string{"shellcheck", "--severity=warning", "-x", "--format=json"},
		CaptureOutputArgs: []string{"--format=json"},
		CaptureStripArgs:  []string{"--format", "-f"},
		FileExtensions:    []string{".sh", ".bash", ".zsh", ".ksh"},
		Languages:         []string{"shell"},
		PassFilesAsArgs:   true,
		Fast:              true,
		EnabledByDefault:  true,
	}
}

func shfmtTool() Tool {
	return Tool{
		Name:             "shfmt",
		Parser:           "shfmt",
		Category:         "shell",
		OutputFormat:     "diff",
		Advice:           "Format shell scripts with shfmt so shell changes stay reviewable and deterministic.",
		Runtime:          RuntimeGo,
		Command:          []string{"shfmt", "-d", "-i", "2", "-ci", "-sr"},
		FileExtensions:   []string{".sh", ".bash", ".zsh", ".ksh"},
		Languages:        []string{"shell"},
		PassFilesAsArgs:  true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func yamllintTool() Tool {
	return Tool{
		Name:              "yamllint",
		Parser:            "yamllint",
		Category:          "syntax",
		OutputFormat:      "parsable",
		Advice:            "Fix YAML structure, indentation, and style before generated config consumers read it.",
		Runtime:           RuntimeUV,
		Command:           []string{"yamllint"},
		CaptureOutputArgs: []string{"-f", "parsable"},
		CaptureStripArgs:  []string{"--format", "-f"},
		ConfigFlags:       []string{"-c"},
		FileExtensions:    []string{".yaml", ".yml"},
		Languages:         []string{"yaml"},
		RepoConfig:        ".yamllint.yml",
		PostConfigArgs:    []string{"--strict", "-f", "parsable"},
		PassFilesAsArgs:   true,
		UseHookProject:    true,
		Fast:              true,
		EnabledByDefault:  true,
	}
}

func pyupgradeTool() Tool {
	return Tool{
		Name:             "pyupgrade",
		Parser:           "fallback",
		Category:         "format",
		OutputFormat:     "text",
		Advice:           "Apply syntax upgrades for the configured Python version.",
		Runtime:          RuntimePython,
		Command:          []string{"pyupgrade"},
		FileExtensions:   []string{".py", ".pyi"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func ruffFormatTool() Tool {
	return Tool{
		Name:             "ruff-format",
		Parser:           "fallback",
		Category:         "format",
		OutputFormat:     "text",
		Advice:           "Run Ruff format before reviewing Python diffs.",
		Runtime:          RuntimePython,
		Command:          []string{"ruff", "format", "--quiet"},
		ConfigFlags:      []string{"--config"},
		FileExtensions:   []string{".py", ".pyi"},
		Languages:        []string{"python"},
		RepoConfig:       "ruff.toml",
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func ruffAutofixTool() Tool {
	return Tool{
		Name:             "ruff-autofix",
		Parser:           "ruff",
		Category:         "format",
		OutputFormat:     "json",
		Advice:           "Apply Ruff autofixes, then resolve remaining diagnostics structurally.",
		Runtime:          RuntimePython,
		Command:          []string{"ruff", "check", "--fix", "--quiet", "--ignore-noqa", "--output-format", "json"},
		ConfigFlags:      []string{"--config"},
		FileExtensions:   []string{".py", ".pyi"},
		Languages:        []string{"python"},
		RepoConfig:       "ruff.toml",
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func gofmtTool() Tool {
	return Tool{
		Name:             "gofmt",
		Parser:           "fallback",
		Category:         "go",
		OutputFormat:     "text",
		Advice:           "Run gofmt before reviewing Go diffs.",
		Runtime:          RuntimeGo,
		Command:          []string{"gofmt", "-l", "."},
		FileExtensions:   []string{".go"},
		Languages:        []string{"go"},
		PassFilesAsArgs:  false,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func goVetTool() Tool {
	return Tool{
		Name:             "go-vet",
		Parser:           "fallback",
		Category:         "go",
		OutputFormat:     "text",
		Advice:           "Fix go vet findings before relying on runtime behavior.",
		Runtime:          RuntimeGo,
		Command:          []string{"go", "vet", "./..."},
		FileExtensions:   []string{".go"},
		Languages:        []string{"go"},
		PassFilesAsArgs:  false,
		EnabledByDefault: true,
	}
}

func goTestTool() Tool {
	return Tool{
		Name:             "go-test",
		Parser:           "fallback",
		Category:         "test",
		OutputFormat:     "text",
		Advice:           "Fix Go test failures as executable behavioral contract failures.",
		Runtime:          RuntimeGo,
		Command:          []string{"go", "test", "./..."},
		FileExtensions:   []string{".go"},
		Languages:        []string{"go"},
		PassFilesAsArgs:  false,
		EnabledByDefault: true,
	}
}

func radonComplexityTool() Tool {
	return Tool{
		Name:             "python-complexity",
		Parser:           "radon-complexity",
		Category:         "python-quality",
		OutputFormat:     "json",
		Advice:           "Reduce cyclomatic complexity by splitting responsibilities and control flow.",
		Runtime:          RuntimePython,
		Command:          []string{"radon", "cc", "-j"},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func radonMaintainabilityTool() Tool {
	return Tool{
		Name:             "python-maintainability",
		Parser:           "radon-maintainability",
		Category:         "python-quality",
		OutputFormat:     "json",
		Advice:           "Improve maintainability by simplifying dense modules.",
		Runtime:          RuntimePython,
		Command:          []string{"radon", "mi", "-j"},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func vultureTool() Tool {
	return Tool{
		Name:             "python-vulture",
		Parser:           "vulture",
		Category:         "python-quality",
		OutputFormat:     "text",
		Advice:           "Remove genuinely unused code or add a narrow whitelist entry for dynamic entry points.",
		Runtime:          RuntimePython,
		Command:          []string{"vulture", "."},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  false,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func interrogateTool() Tool {
	return Tool{
		Name:             "interrogate",
		Parser:           "fallback",
		Category:         "docs",
		OutputFormat:     "text",
		Advice:           "Add useful docstrings where documentation coverage falls below policy.",
		Runtime:          RuntimePython,
		Command:          []string{"interrogate"},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func pytestGateTool() Tool {
	return Tool{
		Name:             "pytest-gate",
		Parser:           "fallback",
		Category:         "test",
		OutputFormat:     "text",
		Advice:           "Fix pytest failures before committing; tests are executable specifications.",
		Runtime:          RuntimePython,
		Command:          []string{"pytest"},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  false,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func geminiCheckTool() Tool {
	return Tool{
		Name:             "gemini-check",
		Parser:           "gemini",
		Category:         "ai",
		OutputFormat:     "json",
		Advice:           "Resolve Gemini critical findings or parser/API errors before committing.",
		Runtime:          RuntimeBinary,
		Command:          []string{"gemini"},
		PassFilesAsArgs:  false,
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
		CaptureOutputArgs: []string{
			"--output.json.path=stdout",
			"--output.text.path=stderr",
		},
		CaptureStripArgs: []string{
			"--out-format",
			"--output.json.path",
			"--output.text.path",
		},
		CaptureAfterFirst: []string{"run"},
		ConfigFlags:       []string{"--config"},
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
	tool.CaptureOutputArgs = append([]string(nil), tool.CaptureOutputArgs...)
	tool.CaptureStripArgs = append([]string(nil), tool.CaptureStripArgs...)
	tool.CaptureStripFlags = append([]string(nil), tool.CaptureStripFlags...)
	tool.CaptureAfterFirst = append([]string(nil), tool.CaptureAfterFirst...)
	tool.CaptureMutatingArgs = append([]string(nil), tool.CaptureMutatingArgs...)
	tool.CaptureMutatingFirst = append([]string(nil), tool.CaptureMutatingFirst...)
	tool.ConfigFlags = append([]string(nil), tool.ConfigFlags...)
	tool.FileExtensions = append([]string(nil), tool.FileExtensions...)
	tool.FilePrefixes = append([]string(nil), tool.FilePrefixes...)
	tool.BaseNamePrefixes = append([]string(nil), tool.BaseNamePrefixes...)
	tool.Languages = append([]string(nil), tool.Languages...)
	tool.PostConfigArgs = append([]string(nil), tool.PostConfigArgs...)

	return tool
}
