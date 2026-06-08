// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package toolcatalog defines typed external tool metadata.
package toolcatalog

import (
	"path/filepath"
	"strings"
)

type Runtime string

const (
	RuntimeBinary Runtime = "binary"
	RuntimeGo     Runtime = "go"
	RuntimeNPM    Runtime = "npm"
	RuntimePython Runtime = "python"
	RuntimeUV     Runtime = "uv"

	geminiCheckToolName = "gemini-check"
)

type Tool struct {
	FallbackBundleConfig string         `json:"fallback_bundle_config,omitempty"`
	Name                 string         `json:"name"`
	ManagedBinary        string         `json:"managed_binary,omitempty"`
	Parser               string         `json:"parser"`
	DiagnosticKind       string         `json:"diagnostic_kind,omitempty"`
	Category             string         `json:"category"`
	OutputFormat         string         `json:"output_format"`
	Advice               string         `json:"advice,omitempty"`
	Runtime              Runtime        `json:"runtime"`
	RepoConfig           string         `json:"repo_config,omitempty"`
	CaptureOutputArgs    []string       `json:"capture_output_args,omitempty"`
	FilePrefixes         []string       `json:"file_prefixes,omitempty"`
	BaseNamePrefixes     []string       `json:"base_name_prefixes,omitempty"`
	Command              []string       `json:"command"`
	CaptureAfterFirst    []string       `json:"capture_after_first,omitempty"`
	CaptureMutatingArgs  []string       `json:"capture_mutating_args,omitempty"`
	CaptureMutatingFirst []string       `json:"capture_mutating_first,omitempty"`
	ConfigFlags          []string       `json:"config_flags,omitempty"`
	FileExtensions       []string       `json:"file_extensions,omitempty"`
	CaptureStripFlags    []string       `json:"capture_strip_flags,omitempty"`
	CaptureStripArgs     []string       `json:"capture_strip_args,omitempty"`
	ParserAliases        []string       `json:"parser_aliases,omitempty"`
	Languages            []string       `json:"languages,omitempty"`
	PostConfigArgs       []string       `json:"post_config_args,omitempty"`
	Capabilities         CapabilitySpec `json:"capabilities,omitzero"`
	PassFilesAsArgs      bool           `json:"pass_files_as_args"`
	UseHookProject       bool           `json:"use_hook_project"`
	Fast                 bool           `json:"fast"`
	Captured             bool           `json:"captured"`
	EnabledByDefault     bool           `json:"enabled_by_default"`
}

const (
	DiagnosticKindParser                = "parser"
	DiagnosticKindFormatterChangedFiles = "formatter_changed_files"
	DiagnosticKindInternalStructured    = "internal_structured"
	DiagnosticKindGenericFallback       = "generic_fallback"

	CategoryFormat = "format"
)

func adviceText(parts ...string) string {
	return strings.Join(parts, " ")
}

type CapturedTool struct {
	Name         string
	Description  string
	ModuleNames  []string
	PythonModule bool
}

type CaptureSpec struct {
	OutputArgs    []string
	StripArgs     []string
	StripFlags    []string
	AfterFirst    []string
	MutatingArgs  []string
	MutatingFirst []string
}

type RuntimeSpec struct {
	Runtime Runtime
	Command []string
	Project bool
}

type ConfigSpec struct {
	RepoConfig           string
	FallbackBundleConfig string
	Flags                []string
	PostArgs             []string
}

type FileMatchSpec struct {
	Extensions       []string
	Prefixes         []string
	BaseNamePrefixes []string
	Languages        []string
	PassFilesAsArgs  bool
}

type CapabilitySpec struct {
	SandboxProfile     string   `json:"sandbox_profile,omitempty"`
	SeccompProfilePath string   `json:"seccomp_profile_path,omitempty"`
	SeccompProfile     string   `json:"seccomp_profile,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	ReadPaths          []string `json:"read_paths,omitempty"`
	WritePaths         []string `json:"write_paths,omitempty"`
	CPUQuotaPercent    int      `json:"cpu_quota_percent,omitempty"`
	MemoryMB           int      `json:"memory_mb,omitempty"`
	TimeoutSeconds     int      `json:"timeout_seconds,omitempty"`
	RequiresNetwork    bool     `json:"requires_network,omitempty"`
	RequiresGit        bool     `json:"requires_git,omitempty"`
	RequiresEnv        bool     `json:"requires_env,omitempty"`
	RequiresProcesses  bool     `json:"requires_processes,omitempty"`
}

type CapabilityView struct {
	SeccompProfile     string   `json:"seccomp_profile"`
	SandboxProfile     string   `json:"sandbox_profile"`
	Name               string   `json:"name"`
	SeccompProfilePath string   `json:"seccomp_profile_path"`
	Command            []string `json:"command"`
	Tags               []string `json:"tags"`
	ReadPaths          []string `json:"read_paths"`
	WritePaths         []string `json:"write_paths"`
	TimeoutSeconds     int      `json:"timeout_seconds"`
	CPUQuotaPercent    int      `json:"cpu_quota_percent"`
	MemoryMB           int      `json:"memory_mb"`
	RequiresNetwork    bool     `json:"requires_network"`
	RequiresGit        bool     `json:"requires_git"`
	RequiresEnv        bool     `json:"requires_env"`
	RequiresProcesses  bool     `json:"requires_processes"`
}

type ShimSpec struct {
	ToolName string
	Command  []string
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

func (tool Tool) CaptureSpec() CaptureSpec {
	return CaptureSpec{
		OutputArgs:    append([]string(nil), tool.CaptureOutputArgs...),
		StripArgs:     append([]string(nil), tool.CaptureStripArgs...),
		StripFlags:    append([]string(nil), tool.CaptureStripFlags...),
		AfterFirst:    append([]string(nil), tool.CaptureAfterFirst...),
		MutatingArgs:  append([]string(nil), tool.CaptureMutatingArgs...),
		MutatingFirst: append([]string(nil), tool.CaptureMutatingFirst...),
	}
}

func (tool Tool) RuntimeSpec() RuntimeSpec {
	return RuntimeSpec{
		Runtime: tool.Runtime,
		Command: append([]string(nil), tool.Command...),
		Project: tool.UseHookProject,
	}
}

func (tool Tool) ConfigSpec() ConfigSpec {
	return ConfigSpec{
		RepoConfig:           tool.RepoConfig,
		FallbackBundleConfig: tool.FallbackBundleConfig,
		Flags:                append([]string(nil), tool.ConfigFlags...),
		PostArgs:             append([]string(nil), tool.PostConfigArgs...),
	}
}

func (tool Tool) FileMatchSpec() FileMatchSpec {
	return FileMatchSpec{
		Extensions:       append([]string(nil), tool.FileExtensions...),
		Prefixes:         append([]string(nil), tool.FilePrefixes...),
		BaseNamePrefixes: append([]string(nil), tool.BaseNamePrefixes...),
		Languages:        append([]string(nil), tool.Languages...),
		PassFilesAsArgs:  tool.PassFilesAsArgs,
	}
}

func (tool Tool) CapabilitySpec() CapabilitySpec {
	spec := tool.Capabilities
	spec = withDefaultOfflineCapabilities(tool.Name, spec)
	spec = withCapabilityTags(tool.Name, spec)

	spec.Tags = append([]string(nil), spec.Tags...)
	spec.ReadPaths = append([]string(nil), spec.ReadPaths...)
	spec.WritePaths = append([]string(nil), spec.WritePaths...)

	return spec
}

func withDefaultOfflineCapabilities(
	toolName string,
	spec CapabilitySpec,
) CapabilitySpec {
	if toolName == geminiCheckToolName {
		return spec
	}

	spec.SandboxProfile = defaultString(spec.SandboxProfile, "lint-offline")
	spec.TimeoutSeconds = defaultInt(spec.TimeoutSeconds, defaultTimeoutSeconds)
	spec.MemoryMB = defaultInt(spec.MemoryMB, defaultMemoryMB)
	spec.CPUQuotaPercent = defaultInt(spec.CPUQuotaPercent, defaultCPUQuotaPercent)
	spec.SeccompProfile = defaultString(spec.SeccompProfile, "deny-privilege")
	spec.ReadPaths = append([]string{"."}, spec.ReadPaths...)
	spec.WritePaths = append(
		[]string{".coding-ethos/cache/", ".coding-ethos/lint-runs/"},
		spec.WritePaths...,
	)

	return spec
}

const (
	defaultCPUQuotaPercent = 100
	defaultMemoryMB        = 2048
	defaultTimeoutSeconds  = 300
)

func withCapabilityTags(toolName string, spec CapabilitySpec) CapabilitySpec {
	spec.Tags = appendCapabilityTags(spec.Tags, networkCapabilityTag(toolName))
	spec.Tags = appendCapabilityTags(spec.Tags, gitCapabilityTag(spec.RequiresGit))

	return spec
}

func networkCapabilityTag(toolName string) string {
	if toolName == geminiCheckToolName {
		return "network"
	}

	return "no-network"
}

func gitCapabilityTag(requiresGit bool) string {
	if requiresGit {
		return "git"
	}

	return "no-git"
}

func defaultString(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}

func defaultInt(value, fallback int) int {
	if value != 0 {
		return value
	}

	return fallback
}

func (tool Tool) CapabilityView() CapabilityView {
	spec := tool.CapabilitySpec()

	return CapabilityView{
		Name:               tool.Name,
		Command:            append([]string(nil), tool.Command...),
		Tags:               spec.Tags,
		ReadPaths:          spec.ReadPaths,
		WritePaths:         spec.WritePaths,
		SandboxProfile:     spec.SandboxProfile,
		TimeoutSeconds:     spec.TimeoutSeconds,
		MemoryMB:           spec.MemoryMB,
		CPUQuotaPercent:    spec.CPUQuotaPercent,
		RequiresNetwork:    spec.RequiresNetwork,
		RequiresGit:        spec.RequiresGit,
		RequiresEnv:        spec.RequiresEnv,
		RequiresProcesses:  spec.RequiresProcesses,
		SeccompProfile:     spec.SeccompProfile,
		SeccompProfilePath: spec.SeccompProfilePath,
	}
}

func appendCapabilityTags(existing []string, tags ...string) []string {
	seen := map[string]struct{}{}

	merged := make([]string, 0, len(existing)+len(tags))
	for _, tag := range append(append([]string(nil), existing...), tags...) {
		if tag == "" {
			continue
		}

		if _, ok := seen[tag]; ok {
			continue
		}

		seen[tag] = struct{}{}
		merged = append(merged, tag)
	}

	return merged
}

func ToolCapabilityViews() []CapabilityView {
	tools := HookOwnedTools()

	views := make([]CapabilityView, 0, len(tools))
	for _, tool := range tools {
		views = append(views, tool.CapabilityView())
	}

	return views
}

func DiagnosticParserNames() []string {
	seen := map[string]bool{}
	names := []string{}

	for _, tool := range HookOwnedTools() {
		addDiagnosticParserName(&names, seen, tool.Parser)

		for _, alias := range tool.ParserAliases {
			addDiagnosticParserName(&names, seen, alias)
		}
	}

	return names
}

func addDiagnosticParserName(names *[]string, seen map[string]bool, name string) {
	name = strings.TrimSpace(name)
	if name == "" || seen[name] {
		return
	}

	seen[name] = true
	*names = append(*names, name)
}

func (tool Tool) ManagedExecutablePath(ethosRoot string) string {
	switch tool.Runtime {
	case RuntimeGo:
		return filepath.Join(
			ethosRoot,
			"build",
			"toolchain",
			"go-bin",
			tool.managedBinaryName(),
		)
	case RuntimeBinary:
		return filepath.Join(
			ethosRoot,
			"build",
			"toolchain",
			"github-bin",
			tool.managedBinaryName(),
		)
	case RuntimeNPM:
		return filepath.Join(
			ethosRoot,
			"build",
			"toolchain",
			"github-bin",
			tool.managedBinaryName(),
		)
	case RuntimePython, RuntimeUV:
		return ""
	default:
		return ""
	}
}

func (tool Tool) managedBinaryName() string {
	if tool.ManagedBinary != "" {
		return tool.ManagedBinary
	}

	return tool.Name
}

func CapturedLintTools() []CapturedTool {
	tools := capturedTools(HookOwnedTools())

	captured := make([]CapturedTool, 0, len(tools))
	for _, tool := range tools {
		captured = append(captured, capturedToolFromTool(tool))
	}

	return captured
}

func capturedTools(tools []Tool) []Tool {
	captured := make([]Tool, 0, len(tools))
	for _, tool := range tools {
		if tool.Captured {
			captured = append(captured, tool)
		}
	}

	return captured
}

func CapturedLintShimSpecs(runnerPath string) []ShimSpec {
	tools := CapturedLintTools()

	specs := make([]ShimSpec, 0, len(tools))
	for _, tool := range tools {
		specs = append(specs, ShimSpec{
			ToolName: tool.Name,
			Command:  []string{runnerPath, "policy-tool", tool.Name},
		})
	}

	return specs
}

func CapturedLintTool(name string) (CapturedTool, bool) {
	for _, tool := range CapturedLintTools() {
		if tool.Name == name {
			return tool.clone(), true
		}
	}

	return CapturedTool{}, false
}

func IsLinter(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}

	for _, tool := range HookOwnedTools() {
		if !tool.Captured {
			continue
		}

		if tool.Name == name || tool.Parser == name {
			return true
		}
	}

	return false
}

func pythonStaticToolDefinitions() []Tool {
	return []Tool{
		ruffTool(),
		pyrightTool(),
		mypyTool(),
		pylintTool(),
	}
}

func capturedToolFromTool(tool Tool) CapturedTool {
	return CapturedTool{
		Name:         tool.Name,
		ModuleNames:  moduleNamesForTool(tool),
		Description:  displayNameForTool(tool.Name),
		PythonModule: tool.Runtime == RuntimePython || tool.Runtime == RuntimeUV,
	}
}

func moduleNamesForTool(tool Tool) []string {
	if tool.Runtime != RuntimePython && tool.Runtime != RuntimeUV {
		return nil
	}

	commandName := tool.Name
	if len(tool.Command) > 0 {
		commandName = tool.Command[0]
	}

	return []string{commandName}
}

func displayNameForTool(name string) string {
	for _, entry := range toolDisplayNameEntries() {
		if entry.Name == name {
			return entry.DisplayName
		}
	}

	return name
}

type toolDisplayNameEntry struct {
	Name        string
	DisplayName string
}

func toolDisplayNameEntries() []toolDisplayNameEntry {
	return []toolDisplayNameEntry{
		{Name: "actionlint", DisplayName: "actionlint"},
		{Name: "bandit", DisplayName: "Bandit"},
		{Name: "dotenv-linter", DisplayName: "dotenv-linter"},
		{Name: "eslint", DisplayName: "ESLint"},
		{Name: "golangci-lint", DisplayName: "golangci-lint"},
		{Name: "golangci-lint-autofix", DisplayName: "golangci-lint autofix"},
		{Name: "golangci-lint-format", DisplayName: "golangci-lint format"},
		{Name: "golines", DisplayName: "golines"},
		{Name: "hadolint", DisplayName: "hadolint"},
		{Name: "kube-linter", DisplayName: "kube-linter"},
		{Name: "mypy", DisplayName: "mypy"},
		{Name: "pylint", DisplayName: "Pylint"},
		{Name: "pyright", DisplayName: "Pyright"},
		{Name: "ruff", DisplayName: "Ruff"},
		{Name: "shellcheck", DisplayName: "ShellCheck"},
		{Name: "sqlfluff", DisplayName: "sqlfluff"},
		{Name: "tombi", DisplayName: "Tombi"},
		{Name: "yamllint", DisplayName: "yamllint"},
	}
}

func toolchainToolDefinitions() []Tool {
	return []Tool{
		hadolintTool(),
		actionlintTool(),
		shellcheckTool(),
		shfmtTool(),
		yamllintTool(),
		banditTool(),
		sqlfluffTool(),
		tombiTool(),
		dotenvLinterTool(),
		eslintTool(),
		tscTool(),
		kubeLinterTool(),
		golangciLintTool(),
		golinesTool(),
	}
}

func hookOwnedToolDefinitions() []Tool {
	return []Tool{
		pyupgradeTool(),
		ruffFormatTool(),
		ruffAutofixTool(),
		golangciLintFormatTool(),
		golangciLintAutofixTool(),
		gofmtTool(),
		goVetTool(),
		goTestTool(),
		radonComplexityTool(),
		radonMaintainabilityTool(),
		vultureTool(),
		pytestGateTool(),
		geminiCheckTool(),
	}
}

func ruffTool() Tool {
	return Tool{
		Name:          "ruff",
		Parser:        "ruff",
		ParserAliases: []string{"ruff-autofix", "ruff-format"},
		Captured:      true,
		Category:      "python-static",
		OutputFormat:  "json",
		Advice: adviceText(
			"Fix Ruff diagnostics structurally; do not suppress unless the",
			"policy requires a documented exception.",
		),
		Runtime: RuntimePython,
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
		Name:         "pyright",
		Parser:       "pyright",
		Captured:     true,
		Category:     "python-static",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix Pyright type diagnostics with precise interfaces and",
			"validated imports.",
		),
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
		Name:         "mypy",
		Parser:       "mypy",
		Captured:     true,
		Category:     "python-static",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix mypy diagnostics with explicit types rather than weakening",
			"required behavior.",
		),
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
		Name:         "pylint",
		Parser:       "pylint",
		Captured:     true,
		Category:     "python-static",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix Pylint findings by simplifying structure before",
			"considering local disables.",
		),
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
		Name:         "hadolint",
		Parser:       "hadolint",
		Captured:     true,
		Category:     "docker",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix Dockerfile findings with explicit, reproducible image",
			"and package choices.",
		),
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
		Name:         "actionlint",
		Parser:       "actionlint",
		Captured:     true,
		Category:     "workflow",
		OutputFormat: "json-lines",
		Advice: adviceText(
			"Fix workflow syntax and action usage before relying on CI",
			"behavior.",
		),
		Runtime:             RuntimeGo,
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
		Name:         "shellcheck",
		Parser:       "shellcheck",
		Captured:     true,
		Category:     "shell",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix shell diagnostics with quoted variables, explicit error",
			"handling, and portable constructs.",
		),
		Runtime: RuntimeBinary,
		Command: []string{
			"shellcheck",
			"--severity=warning",
			"-x",
			"--format=json",
		},
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
		Name:         "shfmt",
		Parser:       "shfmt",
		Category:     "shell",
		OutputFormat: "diff",
		Advice: adviceText(
			"Format shell scripts with shfmt so shell changes stay",
			"reviewable and deterministic.",
		),
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
		Name:         "yamllint",
		Parser:       "yamllint",
		Captured:     true,
		Category:     "syntax",
		OutputFormat: "parsable",
		Advice: adviceText(
			"Fix YAML structure, indentation, and style before generated",
			"config consumers read it.",
		),
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

func banditTool() Tool {
	return Tool{
		Name:         "bandit",
		Parser:       "bandit",
		Captured:     true,
		Category:     "security",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix Python security findings with least-privilege,",
			"validated behavior.",
		),
		Runtime:           RuntimeUV,
		Command:           []string{"bandit", "-q", "-f", "json"},
		CaptureOutputArgs: []string{"-f", "json"},
		CaptureStripArgs:  []string{"--format", "-f"},
		ConfigFlags:       []string{"-c"},
		FileExtensions:    []string{".py"},
		Languages:         []string{"python"},
		RepoConfig:        ".bandit.yml",
		PostConfigArgs: []string{
			"--severity-level",
			"medium",
			"--confidence-level",
			"medium",
		},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func sqlfluffTool() Tool {
	return Tool{
		Name:              "sqlfluff",
		Parser:            "sqlfluff",
		Captured:          true,
		Category:          "sql",
		OutputFormat:      "json",
		Advice:            "Fix SQL syntax and style with explicit dialect-aware queries.",
		Runtime:           RuntimeUV,
		Command:           []string{"sqlfluff", "lint", "--format", "json"},
		CaptureOutputArgs: []string{"--format", "json"},
		CaptureStripArgs:  []string{"--format", "-f"},
		CaptureAfterFirst: []string{"lint"},
		ConfigFlags:       []string{"--config"},
		FileExtensions:    []string{".sql"},
		Languages:         []string{"sql"},
		RepoConfig:        ".sqlfluff",
		PassFilesAsArgs:   true,
		UseHookProject:    true,
		EnabledByDefault:  true,
	}
}

func tombiTool() Tool {
	return Tool{
		Name:         "tombi",
		Parser:       "tombi",
		Captured:     true,
		Category:     "syntax",
		OutputFormat: "text",
		Advice: adviceText(
			"Fix TOML syntax and schema issues before tools consume",
			"configuration.",
		),
		Runtime:          RuntimeUV,
		Command:          []string{"tombi", "lint", "--quiet", "--error-on-warnings"},
		CaptureStripArgs: []string{"--format", "-f"},
		FileExtensions:   []string{".toml"},
		Languages:        []string{"toml"},
		PassFilesAsArgs:  true,
		UseHookProject:   true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func dotenvLinterTool() Tool {
	return Tool{
		Name:         "dotenv-linter",
		Parser:       "dotenv-linter",
		Captured:     true,
		Category:     "dotenv",
		OutputFormat: "text",
		Advice: adviceText(
			"Fix dotenv files so local environment contracts stay",
			"explicit and safe.",
		),
		Runtime:          RuntimeBinary,
		Command:          []string{"dotenv-linter", "--plain", "--quiet", "check"},
		CaptureStripArgs: []string{"--format", "-f"},
		BaseNamePrefixes: []string{".env"},
		Languages:        []string{"dotenv"},
		PassFilesAsArgs:  true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func eslintTool() Tool {
	return Tool{
		Name:         "eslint",
		Parser:       "eslint",
		Captured:     true,
		Category:     "javascript-static",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix ESLint findings structurally and keep JavaScript and",
			"TypeScript checks inside the managed lint path.",
		),
		Runtime: RuntimeNPM,
		Command: []string{
			"eslint",
			"--format",
			"json",
		},
		CaptureOutputArgs:   []string{"--format", "json"},
		CaptureStripArgs:    []string{"--format", "-f"},
		CaptureMutatingArgs: []string{"--fix", "--fix-dry-run"},
		FileExtensions: []string{
			".js",
			".jsx",
			".mjs",
			".cjs",
			".ts",
			".tsx",
			".mts",
			".cts",
		},
		Languages:        []string{"javascript", "typescript"},
		PassFilesAsArgs:  true,
		Fast:             true,
		EnabledByDefault: false,
	}
}

func tscTool() Tool {
	return Tool{
		Name:         "tsc",
		Parser:       "tsc",
		Captured:     true,
		Category:     "typescript-static",
		OutputFormat: "text",
		Advice: adviceText(
			"Fix TypeScript compiler diagnostics with explicit types and",
			"project-level configuration instead of relying on runtime checks.",
		),
		Runtime: RuntimeNPM,
		Command: []string{
			"tsc",
			"--noEmit",
			"--pretty",
			"false",
		},
		CaptureOutputArgs: []string{
			"--noEmit",
			"--pretty",
			"false",
		},
		CaptureStripArgs: []string{
			"--pretty",
			"--project",
			"-p",
		},
		CaptureStripFlags: []string{
			"--noEmit",
			"--watch",
			"-w",
			"--build",
			"-b",
		},
		ConfigFlags: []string{"--project", "-p"},
		FileExtensions: []string{
			".ts",
			".tsx",
			".mts",
			".cts",
		},
		Languages:        []string{"typescript"},
		RepoConfig:       "tsconfig.json",
		PassFilesAsArgs:  false,
		Fast:             false,
		EnabledByDefault: false,
	}
}

func kubeLinterTool() Tool {
	return Tool{
		Name:         "kube-linter",
		Parser:       "kube-linter",
		Captured:     true,
		Category:     "kubernetes-security",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix Kubernetes manifest findings with least-privilege pod",
			"security settings and explicit resource boundaries.",
		),
		Runtime: RuntimeGo,
		Command: []string{
			"kube-linter",
			"lint",
			"--format",
			"json",
		},
		CaptureOutputArgs: []string{
			"--format",
			"json",
		},
		CaptureStripArgs: []string{
			"--format",
		},
		CaptureAfterFirst: []string{"lint"},
		FileExtensions: []string{
			".yaml",
			".yml",
		},
		Languages:        []string{"kubernetes"},
		PassFilesAsArgs:  true,
		Fast:             false,
		EnabledByDefault: false,
	}
}

func pyupgradeTool() Tool {
	return Tool{
		Name:             "pyupgrade",
		Parser:           "fallback",
		DiagnosticKind:   DiagnosticKindFormatterChangedFiles,
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
		Parser:           "ruff",
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
		Name:         "ruff-autofix",
		Parser:       "ruff",
		Category:     "format",
		OutputFormat: "json",
		Advice: adviceText(
			"Apply Ruff autofixes, then resolve remaining diagnostics",
			"structurally.",
		),
		Runtime: RuntimePython,
		Command: []string{
			"ruff",
			"check",
			"--fix",
			"--quiet",
			"--ignore-noqa",
			"--output-format",
			"json",
		},
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

func golangciLintAutofixTool() Tool {
	return Tool{
		Name:          "golangci-lint-autofix",
		ManagedBinary: "golangci-lint",
		Parser:        "golangci-lint",
		Category:      "format",
		OutputFormat:  "json",
		Advice: adviceText(
			"Apply golangci-lint autofixes, then resolve remaining",
			"diagnostics structurally.",
		),
		Runtime:          RuntimeGo,
		Command:          []string{"golangci-lint", "run", "--fix"},
		ConfigFlags:      []string{"--config"},
		FileExtensions:   []string{".go"},
		Languages:        []string{"go"},
		RepoConfig:       ".golangci.yml",
		PassFilesAsArgs:  false,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func golangciLintFormatTool() Tool {
	return Tool{
		Name:           "golangci-lint-format",
		ManagedBinary:  "golangci-lint",
		Parser:         "fallback",
		DiagnosticKind: DiagnosticKindFormatterChangedFiles,
		Category:       "format",
		OutputFormat:   "text",
		Advice:         "Run golangci-lint fmt before reviewing Go diffs.",
		Runtime:        RuntimeGo,
		Command:        []string{"golangci-lint", "fmt"},
		ConfigFlags:    []string{"--config"},
		FileExtensions: []string{
			".go",
		},
		Languages:        []string{"go"},
		RepoConfig:       ".golangci.yml",
		PassFilesAsArgs:  false,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func gofmtTool() Tool {
	return Tool{
		Name:             "gofmt",
		Parser:           "gofmt",
		ParserAliases:    []string{"gofmt-check"},
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

func golinesTool() Tool {
	return Tool{
		Name:           "golines",
		Parser:         "fallback",
		DiagnosticKind: DiagnosticKindFormatterChangedFiles,
		Category:       "format",
		OutputFormat:   "text",
		Advice: adviceText(
			"Run golines to keep Go code within the shared line-length",
			"policy.",
		),
		Runtime:          RuntimeGo,
		Command:          []string{"golines", "-w", "-m", "88"},
		FileExtensions:   []string{".go"},
		Languages:        []string{"go"},
		PassFilesAsArgs:  true,
		Fast:             true,
		EnabledByDefault: true,
	}
}

func goVetTool() Tool {
	return Tool{
		Name:             "go-vet",
		Parser:           "go-vet",
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
		Name:           "go-test",
		Parser:         "go-test",
		Category:       "test",
		OutputFormat:   "json",
		Advice:         "Fix Go test failures as executable behavioral contract failures.",
		Runtime:        RuntimeGo,
		Command:        []string{"go", "test", "./..."},
		FileExtensions: []string{".go"},
		Languages:      []string{"go"},
		Capabilities: CapabilitySpec{
			RequiresProcesses: true,
		},
		PassFilesAsArgs:  false,
		EnabledByDefault: true,
	}
}

func radonComplexityTool() Tool {
	return Tool{
		Name:         "python-complexity",
		Parser:       "radon-complexity",
		Category:     "python-quality",
		OutputFormat: "json",
		Advice: adviceText(
			"Reduce cyclomatic complexity by splitting responsibilities",
			"and control flow.",
		),
		Runtime:          RuntimePython,
		Command:          []string{"radon", "cc", "-j", "-e", ".venv/*,node_modules/*"},
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
		Command:          []string{"radon", "mi", ".", "-j", "-e", ".venv/*,node_modules/*"},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  false,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func vultureTool() Tool {
	return Tool{
		Name:         "python-vulture",
		Parser:       "vulture",
		Category:     "python-quality",
		OutputFormat: "text",
		Advice: adviceText(
			"Remove genuinely unused code or add a narrow whitelist entry",
			"for dynamic entry points.",
		),
		Runtime:          RuntimePython,
		Command:          []string{"vulture", "."},
		FileExtensions:   []string{".py"},
		Languages:        []string{"python"},
		PassFilesAsArgs:  false,
		UseHookProject:   true,
		EnabledByDefault: true,
	}
}

func pytestGateTool() Tool {
	return Tool{
		Name:          "pytest-gate",
		Parser:        "pytest-gate",
		ParserAliases: []string{"pytest"},
		Category:      "test",
		OutputFormat:  "text",
		Advice: adviceText(
			"Fix pytest failures before committing; tests are executable",
			"specifications.",
		),
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
		Name:          geminiCheckToolName,
		Parser:        "gemini",
		ParserAliases: []string{"gemini-check"},
		Category:      "ai",
		OutputFormat:  "json",
		Advice: adviceText(
			"Resolve Gemini critical findings or parser/API errors",
			"before committing.",
		),
		Runtime: RuntimeBinary,
		Command: []string{"gemini"},
		Capabilities: CapabilitySpec{
			RequiresNetwork: true,
			SandboxProfile:  "agent-network",
		},
		PassFilesAsArgs:  false,
		EnabledByDefault: true,
	}
}

func golangciLintTool() Tool {
	return Tool{
		Name:   "golangci-lint",
		Parser: "golangci-lint",
		ParserAliases: []string{
			"golangci-lint-autofix",
			"golangci-lint-format",
		},
		Captured:     true,
		Category:     "go-static",
		OutputFormat: "json",
		Advice: adviceText(
			"Fix Go lint findings structurally; do not add exclusions or",
			"weaken the shared linter policy.",
		),
		Runtime: RuntimeGo,
		Command: []string{"golangci-lint", "run"},
		CaptureOutputArgs: []string{
			"--allow-parallel-runners",
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
	tool.Capabilities = tool.CapabilitySpec()

	return tool
}

func (tool CapturedTool) clone() CapturedTool {
	tool.ModuleNames = append([]string(nil), tool.ModuleNames...)

	return tool
}
