// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolcatalog_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const brokenFlag = "--broken"

func TestPythonStaticToolsExposeExpectedTools(t *testing.T) {
	t.Parallel()

	tools := toolcatalog.PythonStaticTools()

	names := make(map[string]bool, len(tools))
	for _, tool := range tools {
		names[tool.Name] = true
	}

	for _, name := range []string{"ruff", "pyright", "mypy", "pylint"} {
		if !names[name] {
			t.Fatalf("PythonStaticTools() missing %q: %#v", name, tools)
		}
	}
}

func TestPythonStaticToolsRequestStructuredOutput(t *testing.T) {
	t.Parallel()

	for name, expected := range map[string]struct {
		token        string
		outputFormat string
		fast         bool
	}{
		"ruff":    {token: "--output-format", outputFormat: "json", fast: true},
		"pyright": {token: "--outputjson", outputFormat: "json"},
		"mypy":    {token: "--output", outputFormat: "json"},
		"pylint":  {token: "--output-format=json", outputFormat: "json"},
	} {
		tool, ok := toolcatalog.PythonStaticTool(name)
		if !ok {
			t.Fatalf("PythonStaticTool(%q) missing", name)
		}

		if !slices.Contains(tool.Command, expected.token) {
			t.Fatalf("%s command missing %q: %#v", name, expected.token, tool.Command)
		}

		if tool.Category != "python-static" ||
			tool.OutputFormat != expected.outputFormat ||
			!slices.Contains(tool.Languages, "python") ||
			tool.Fast != expected.fast ||
			tool.Advice == "" {
			t.Fatalf("%s typed metadata mismatch: %#v", name, tool)
		}
	}
}

func TestHookProjectToolsAreDeclaredDependencies(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", ".."))
	pyprojectPath := filepath.Join(repoRoot, "pre-commit", "hooks", "pyproject.toml")

	content, err := os.ReadFile(pyprojectPath)
	if err != nil {
		t.Fatalf("read hook pyproject: %v", err)
	}

	pyproject := string(content)

	for _, captured := range toolcatalog.CapturedLintTools() {
		tool, found := toolcatalog.HookOwnedTool(captured.Name)
		if !found {
			t.Fatalf("captured tool %q missing hook-owned metadata", captured.Name)
		}

		runtimeSpec := tool.RuntimeSpec()
		if !runtimeSpec.Project || len(runtimeSpec.Command) == 0 {
			continue
		}

		if runtimeSpec.Runtime != toolcatalog.RuntimePython &&
			runtimeSpec.Runtime != toolcatalog.RuntimeUV {
			continue
		}

		commandName := runtimeSpec.Command[0]
		if !strings.Contains(pyproject, `"`+commandName) {
			t.Fatalf(
				"hook-project tool %q is not declared in %s",
				commandName,
				pyprojectPath,
			)
		}
	}
}

func TestPythonStaticToolReturnsDefensiveCopy(t *testing.T) {
	t.Parallel()

	tool, found := toolcatalog.PythonStaticTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	tool.Command[0] = "changed"

	again, found := toolcatalog.PythonStaticTool("ruff")
	if !found {
		t.Fatal("missing ruff tool on second lookup")
	}

	if again.Command[0] != "ruff" {
		t.Fatalf("catalog command mutated: %#v", again.Command)
	}
}

func TestToolchainToolsExposeCurrentHookCommands(t *testing.T) {
	t.Parallel()

	tools := mapByName(toolcatalog.ToolchainTools())

	assertToolchainCommands(t, tools)
	assertToolchainTypedMetadata(t, tools)
	assertToolchainFileMetadata(t, tools)
	assertToolchainConfigMetadata(t, tools)
}

func assertToolchainCommands(
	t *testing.T,
	tools map[string]toolcatalog.Tool,
) {
	t.Helper()

	for name, want := range toolchainCommandExpectations() {
		assertToolCommand(t, tools[name], want)
	}
}

func toolchainCommandExpectations() map[string][]string {
	return map[string][]string{
		"hadolint":      {"hadolint", "--format", "json"},
		"actionlint":    {"actionlint", "-format", "{{json .}}"},
		"shellcheck":    {"shellcheck", "--severity=warning", "-x", "--format=json"},
		"shfmt":         {"shfmt", "-d", "-i", "2", "-ci", "-sr"},
		"yamllint":      {"yamllint"},
		"bandit":        {"bandit", "-q", "-f", "json"},
		"sqlfluff":      {"sqlfluff", "lint", "--format", "json"},
		"tombi":         {"tombi", "lint", "--quiet", "--error-on-warnings"},
		"dotenv-linter": {"dotenv-linter", "--plain", "--quiet", "check"},
		"eslint":        {"eslint", "--format", "json"},
		"tsc":           {"tsc", "--noEmit", "--pretty", "false"},
		"kube-linter":   {"kube-linter", "lint", "--format", "json"},
		"golangci-lint": {"golangci-lint", "run"},
		"golines":       {"golines", "-w", "-m", "88"},
	}
}

func assertToolchainTypedMetadata(
	t *testing.T,
	tools map[string]toolcatalog.Tool,
) {
	t.Helper()

	for name, want := range toolchainCategoryExpectations() {
		tool := tools[name]
		if tool.Category != want || tool.OutputFormat == "" || tool.Advice == "" {
			t.Fatalf("%s typed metadata mismatch: %#v", name, tool)
		}
	}
}

func toolchainCategoryExpectations() map[string]string {
	return map[string]string{
		"hadolint":      "docker",
		"actionlint":    "workflow",
		"shellcheck":    "shell",
		"shfmt":         "shell",
		"yamllint":      "syntax",
		"bandit":        "security",
		"sqlfluff":      "sql",
		"tombi":         "syntax",
		"dotenv-linter": "dotenv",
		"eslint":        "javascript-static",
		"tsc":           "typescript-static",
		"kube-linter":   "kubernetes-security",
		"golangci-lint": "go-static",
		"golines":       "format",
	}
}

func assertToolchainFileMetadata(
	t *testing.T,
	tools map[string]toolcatalog.Tool,
) {
	t.Helper()

	for name, want := range toolchainFileMetadataExpectations() {
		assertToolFileMetadata(
			t,
			tools[name],
			want.extensions,
			want.includeDirs,
			want.basenames,
		)
	}
}

type toolFileMetadataExpectation struct {
	extensions  []string
	includeDirs []string
	basenames   []string
}

func toolchainFileMetadataExpectations() map[string]toolFileMetadataExpectation {
	return map[string]toolFileMetadataExpectation{
		"hadolint": {
			basenames: []string{"Dockerfile"},
		},
		"actionlint": {
			extensions:  []string{".yaml", ".yml"},
			includeDirs: []string{".github/workflows/"},
		},
		"shellcheck": {
			extensions: []string{".sh", ".bash", ".zsh", ".ksh"},
		},
		"shfmt": {
			extensions: []string{".sh", ".bash", ".zsh", ".ksh"},
		},
		"yamllint": {
			extensions: []string{".yaml", ".yml"},
		},
		"bandit": {
			extensions: []string{".py"},
		},
		"sqlfluff": {
			extensions: []string{".sql"},
		},
		"tombi": {
			extensions: []string{".toml"},
		},
		"dotenv-linter": {
			basenames: []string{".env"},
		},
		"eslint": {
			extensions: []string{
				".js",
				".jsx",
				".mjs",
				".cjs",
				".ts",
				".tsx",
				".mts",
				".cts",
			},
		},
		"tsc": {
			extensions: []string{".ts", ".tsx", ".mts", ".cts"},
		},
		"kube-linter": {
			extensions: []string{".yaml", ".yml"},
		},
		"golangci-lint": {
			extensions: []string{".go"},
		},
		"golines": {
			extensions: []string{".go"},
		},
	}
}

func assertToolchainConfigMetadata(
	t *testing.T,
	tools map[string]toolcatalog.Tool,
) {
	t.Helper()

	if tools["yamllint"].RepoConfig != ".yamllint.yml" ||
		tools["yamllint"].ConfigFlags[0] != "-c" {
		t.Fatalf("yamllint config metadata = %#v", tools["yamllint"])
	}

	if !reflect.DeepEqual(
		tools["yamllint"].PostConfigArgs,
		[]string{"--strict", "-f", "parsable"},
	) {
		t.Fatalf("yamllint post-config args = %#v", tools["yamllint"].PostConfigArgs)
	}

	assertGolangCIConfigMetadata(t, tools["golangci-lint"])
}

func assertGolangCIConfigMetadata(t *testing.T, golangci toolcatalog.Tool) {
	t.Helper()

	if golangci.RepoConfig != ".golangci.yml" || golangci.ConfigFlags[0] != "--config" {
		t.Fatalf("golangci-lint config metadata = %#v", golangci)
	}

	wantPostConfig := []string{
		"--output.json.path",
		"stdout",
		"--output.text.path",
		"/dev/null",
	}
	if !reflect.DeepEqual(golangci.PostConfigArgs, wantPostConfig) {
		t.Fatalf(
			"golangci-lint post-config args = %#v, want %#v",
			golangci.PostConfigArgs,
			wantPostConfig,
		)
	}
}

func TestHookOwnedToolsExposeSpecialHookCommands(t *testing.T) {
	t.Parallel()

	tools := mapByName(toolcatalog.HookOwnedTools())
	for _, name := range []string{
		"pyupgrade",
		"ruff-format",
		"ruff-autofix",
		"golangci-lint-format",
		"golangci-lint-autofix",
		"gofmt",
		"go-vet",
		"go-test",
		"python-complexity",
		"python-maintainability",
		"python-vulture",
		"pytest-gate",
		"gemini-check",
	} {
		tool, ok := tools[name]
		if !ok {
			t.Fatalf("HookOwnedTools() missing %q", name)
		}

		if tool.Category == "" || tool.OutputFormat == "" || tool.Advice == "" {
			t.Fatalf("%s metadata is incomplete: %#v", name, tool)
		}
	}
}

func TestHookOwnedCapturedToolsExposeCaptureMetadata(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"ruff",
		"pyright",
		"mypy",
		"pylint",
		"shellcheck",
		"golangci-lint",
		"actionlint",
		"yamllint",
		"hadolint",
		"bandit",
		"sqlfluff",
		"eslint",
		"tsc",
		"kube-linter",
	} {
		tool, found := toolcatalog.HookOwnedTool(name)
		if !found {
			t.Fatalf("HookOwnedTool(%q) missing", name)
		}

		if tool.Parser == "" || len(tool.CaptureOutputArgs) == 0 {
			t.Fatalf("%s missing parser or capture metadata: %#v", name, tool)
		}

		if len(tool.CaptureStripArgs) == 0 && len(tool.CaptureStripFlags) == 0 {
			t.Fatalf("%s cannot strip caller output flags: %#v", name, tool)
		}
	}
}

func TestCapturedLintToolsAreDerivedFromCatalog(t *testing.T) {
	t.Parallel()

	captured := map[string]toolcatalog.CapturedTool{}
	for _, tool := range toolcatalog.CapturedLintTools() {
		captured[tool.Name] = tool
	}

	for _, name := range []string{
		"ruff",
		"mypy",
		"pyright",
		"pylint",
		"bandit",
		"sqlfluff",
		"tombi",
		"shellcheck",
		"golangci-lint",
		"actionlint",
		"yamllint",
		"hadolint",
		"dotenv-linter",
		"eslint",
		"tsc",
		"kube-linter",
	} {
		tool, found := toolcatalog.HookOwnedTool(name)
		if !found {
			t.Fatalf("HookOwnedTool(%q) missing", name)
		}

		capture, found := captured[name]
		if !found {
			t.Fatalf("CapturedLintTools() missing %q", name)
		}

		if capture.Description == "" {
			t.Fatalf("CapturedLintTools(%q) missing description", name)
		}

		if tool.Runtime == toolcatalog.RuntimePython ||
			tool.Runtime == toolcatalog.RuntimeUV {
			if !capture.PythonModule || len(capture.ModuleNames) == 0 {
				t.Fatalf(
					"%s missing Python module capture metadata: %#v",
					name,
					capture,
				)
			}
		}
	}
}

func TestIsLinterUsesCapturedToolMetadata(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"ruff",
		"pyright",
		"mypy",
		"pylint",
		"golangci-lint",
		"eslint",
		"tsc",
		"kube-linter",
	} {
		if !toolcatalog.IsLinter(name) {
			t.Fatalf("IsLinter(%q) = false, want true", name)
		}
	}

	for _, name := range []string{
		"go-test",
		"pytest-gate",
		"gemini-check",
		"ruff-format",
		"golangci-lint-format",
	} {
		if toolcatalog.IsLinter(name) {
			t.Fatalf("IsLinter(%q) = true, want false", name)
		}
	}
}

func TestHookOwnedToolsDeclareDiagnosticContract(t *testing.T) {
	t.Parallel()

	for _, tool := range toolcatalog.HookOwnedTools() {
		if tool.Parser == "" {
			t.Fatalf("%s missing parser declaration", tool.Name)
		}

		if diagnostics.HasParser(tool.Parser) {
			continue
		}

		switch tool.DiagnosticKind {
		case toolcatalog.DiagnosticKindFormatterChangedFiles,
			toolcatalog.DiagnosticKindInternalStructured:
			continue
		default:
		}

		t.Fatalf(
			"%s parser %q is not centrally registered and has no explicit diagnostic kind: %#v",
			tool.Name,
			tool.Parser,
			tool,
		)
	}
}

func TestHookOwnedToolsDoNotUseGenericFallbackDiagnostics(t *testing.T) {
	t.Parallel()

	for _, tool := range toolcatalog.HookOwnedTools() {
		if tool.DiagnosticKind == toolcatalog.DiagnosticKindGenericFallback {
			t.Fatalf("%s uses generic fallback diagnostics: %#v", tool.Name, tool)
		}
	}
}

func TestRegisteredParsersAreCatalogDeclared(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	for _, parser := range toolcatalog.DiagnosticParserNames() {
		declared[parser] = true
	}

	for _, parser := range diagnostics.RegisteredParsers() {
		if !declared[parser] {
			t.Fatalf("registered parser %q is not declared by the tool catalog", parser)
		}
	}
}

func TestToolCapabilityViewsAreDefensiveCopies(t *testing.T) {
	t.Parallel()

	tool, found := toolcatalog.HookOwnedTool("bandit")
	if !found {
		t.Fatal("missing bandit")
	}

	capture := tool.CaptureSpec()
	capture.OutputArgs[0] = brokenFlag

	if tool.CaptureOutputArgs[0] == brokenFlag {
		t.Fatal("CaptureSpec shared backing array with Tool")
	}

	runtime := tool.RuntimeSpec()

	runtime.Command[0] = "broken"
	if tool.Command[0] == "broken" {
		t.Fatal("RuntimeSpec shared backing array with Tool")
	}

	files := tool.FileMatchSpec()
	files.Extensions[0] = ".broken"

	if tool.FileExtensions[0] == ".broken" {
		t.Fatal("FileMatchSpec shared backing array with Tool")
	}

	config := tool.ConfigSpec()
	config.Flags = append(config.Flags, brokenFlag)

	if len(tool.ConfigFlags) > 0 &&
		tool.ConfigFlags[len(tool.ConfigFlags)-1] == brokenFlag {
		t.Fatal("ConfigSpec shared backing array with Tool")
	}

	capabilities := tool.CapabilitySpec()

	capabilities.ReadPaths = append(capabilities.ReadPaths, "/broken")
	if len(tool.Capabilities.ReadPaths) > 0 &&
		tool.Capabilities.ReadPaths[len(tool.Capabilities.ReadPaths)-1] == "/broken" {
		t.Fatal("CapabilitySpec shared backing array with Tool")
	}
}

func TestToolCapabilitiesAreDenyByDefaultForNetwork(t *testing.T) {
	t.Parallel()

	for _, tool := range toolcatalog.HookOwnedTools() {
		capabilities := tool.CapabilitySpec()
		if tool.Name == "gemini-check" {
			assertGeminiNetworkCapabilities(t, capabilities)

			continue
		}

		assertNoNetworkCapabilities(t, tool.Name, capabilities)
	}
}

func assertGeminiNetworkCapabilities(
	t *testing.T,
	capabilities toolcatalog.CapabilitySpec,
) {
	t.Helper()

	if !capabilities.RequiresNetwork ||
		capabilities.SandboxProfile != "agent-network" ||
		!slices.Contains(capabilities.Tags, "network") ||
		slices.Contains(capabilities.Tags, "no-network") ||
		!slices.Contains(capabilities.Tags, "no-git") {
		t.Fatalf("gemini-check capabilities = %#v", capabilities)
	}
}

func assertNoNetworkCapabilities(
	t *testing.T,
	name string,
	capabilities toolcatalog.CapabilitySpec,
) {
	t.Helper()

	if capabilities.RequiresNetwork {
		t.Fatalf("%s unexpectedly requires network: %#v", name, capabilities)
	}

	if !slices.Contains(capabilities.Tags, "no-network") ||
		slices.Contains(capabilities.Tags, "network") {
		t.Fatalf("%s missing no-network tag: %#v", name, capabilities)
	}

	if !capabilities.RequiresGit && !slices.Contains(capabilities.Tags, "no-git") {
		t.Fatalf("%s missing no-git tag: %#v", name, capabilities)
	}

	if capabilities.TimeoutSeconds <= 0 ||
		capabilities.MemoryMB <= 0 ||
		capabilities.CPUQuotaPercent <= 0 ||
		capabilities.SeccompProfile == "" {
		t.Fatalf("%s missing default sandbox limits: %#v", name, capabilities)
	}

	for _, want := range []string{".coding-ethos/cache/", ".coding-ethos/lint-runs/"} {
		if !slices.Contains(capabilities.WritePaths, want) {
			t.Fatalf("%s missing default write path %q: %#v", name, want, capabilities)
		}
	}
}

func TestToolCapabilityViewsExposeCommandsAndDefensiveCopies(t *testing.T) {
	t.Parallel()

	views := toolcatalog.ToolCapabilityViews()
	if len(views) == 0 {
		t.Fatal("ToolCapabilityViews() returned no tools")
	}

	views[0].Command = append(views[0].Command, "mutated")

	again := toolcatalog.ToolCapabilityViews()
	if reflect.DeepEqual(views[0].Command, again[0].Command) {
		t.Fatal("ToolCapabilityViews() shared command backing array")
	}
}

func TestManagedExecutablePathUsesCheckoutToolchain(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "repo", "coding-ethos")

	assertManagedExecutablePath(
		t,
		root,
		"shellcheck",
		filepath.Join(root, "build", "toolchain", "github-bin", "shellcheck"),
	)
	assertManagedExecutablePath(
		t,
		root,
		"actionlint",
		filepath.Join(root, "build", "toolchain", "go-bin", "actionlint"),
	)
	assertManagedExecutablePath(
		t,
		root,
		"golangci-lint-autofix",
		filepath.Join(root, "build", "toolchain", "go-bin", "golangci-lint"),
	)
	assertManagedExecutablePath(
		t,
		root,
		"golangci-lint-format",
		filepath.Join(root, "build", "toolchain", "go-bin", "golangci-lint"),
	)

	ruff, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff")
	}

	if got := ruff.ManagedExecutablePath(root); got != "" {
		t.Fatalf(
			"ManagedExecutablePath(ruff) = %q, want empty for Python wrapper tools",
			got,
		)
	}
}

func assertManagedExecutablePath(
	t *testing.T,
	root string,
	toolName string,
	want string,
) {
	t.Helper()

	tool, found := toolcatalog.HookOwnedTool(toolName)
	if !found {
		t.Fatalf("missing %s", toolName)
	}

	if got := tool.ManagedExecutablePath(root); got != want {
		t.Fatalf("ManagedExecutablePath(%s) = %q, want %q", toolName, got, want)
	}
}

func TestCapturedLintShimSpecsUseCatalogTools(t *testing.T) {
	t.Parallel()

	specs := toolcatalog.CapturedLintShimSpecs("/repo/coding-ethos-run")

	byTool := map[string]toolcatalog.ShimSpec{}
	for _, spec := range specs {
		byTool[spec.ToolName] = spec
	}

	spec, found := byTool["ruff"]
	if !found {
		t.Fatal("missing ruff shim spec")
	}

	want := []string{"/repo/coding-ethos-run", "policy-tool", "ruff"}
	if !reflect.DeepEqual(spec.Command, want) {
		t.Fatalf("ruff shim command = %#v, want %#v", spec.Command, want)
	}
}

func TestToolCaptureArgsForceCatalogOutput(t *testing.T) {
	t.Parallel()

	for _, test := range toolCaptureArgsForceCatalogOutputCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tool, found := toolcatalog.HookOwnedTool(test.name)
			if !found {
				t.Fatalf("missing tool %q", test.name)
			}

			got, ok := tool.CaptureArgs(test.args)
			if !ok {
				t.Fatalf("CaptureArgs(%s) did not apply", test.name)
			}

			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("CaptureArgs(%s) = %#v, want %#v", test.name, got, test.want)
			}
		})
	}
}

type toolCaptureArgsCase struct {
	name string
	args []string
	want []string
}

func toolCaptureArgsForceCatalogOutputCases() []toolCaptureArgsCase {
	return []toolCaptureArgsCase{
		{
			name: "ruff",
			args: []string{"check", "--output-format=github", "pkg"},
			want: []string{"check", "--output-format=json", "pkg"},
		},
		{
			name: "pyright",
			args: []string{"--outputjson", "pkg"},
			want: []string{"--outputjson", "pkg"},
		},
		{
			name: "mypy",
			args: []string{"--output", "pretty", "pkg"},
			want: []string{"--output=json", "pkg"},
		},
		{
			name: "pylint",
			args: []string{"-f", "text", "pkg"},
			want: []string{"--output-format=json", "pkg"},
		},
		{
			name: "bandit",
			args: []string{"-f", "txt", "pkg"},
			want: []string{"-f", "json", "pkg"},
		},
		{
			name: "sqlfluff",
			args: []string{"lint", "--format", "human", "query.sql"},
			want: []string{"lint", "--format", "json", "query.sql"},
		},
		{
			name: "shellcheck",
			args: []string{"-f", "gcc", "script.sh"},
			want: []string{"--format=json", "script.sh"},
		},
		{
			name: "yamllint",
			args: []string{"--format", "standard", "config.yaml"},
			want: []string{"-f", "parsable", "config.yaml"},
		},
		{
			name: "hadolint",
			args: []string{"--format=tty", "Dockerfile"},
			want: []string{"--format", "json", "Dockerfile"},
		},
		{
			name: "actionlint",
			args: []string{"-format", "{{.Message}}", ".github/workflows/ci.yml"},
			want: []string{"-format", "{{json .}}", ".github/workflows/ci.yml"},
		},
		{
			name: "eslint",
			args: []string{"-f", "stylish", "web/app.js"},
			want: []string{"--format", "json", "web/app.js"},
		},
		{
			name: "tsc",
			args: []string{"--pretty", "true", "--project", "tsconfig.dev.json"},
			want: []string{"--noEmit", "--pretty", "false"},
		},
		{
			name: "kube-linter",
			args: []string{"lint", "--format", "plain", "deploy/pod.yaml"},
			want: []string{"lint", "--format", "json", "deploy/pod.yaml"},
		},
		{
			name: "golangci-lint",
			args: []string{"run", "--out-format", "colored-line-number", "./..."},
			want: []string{
				"run",
				"--allow-parallel-runners",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
				"./...",
			},
		},
	}
}

func TestToolCaptureArgsSkipsMutatingCommands(t *testing.T) {
	t.Parallel()

	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff")
	}

	got, ok := tool.CaptureArgs([]string{"format", "pkg"})
	if ok {
		t.Fatalf("CaptureArgs applied to mutating ruff format: %#v", got)
	}
}

func TestToolCaptureArgsSkipsInformationalCommands(t *testing.T) {
	t.Parallel()

	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff")
	}

	got, ok := tool.CaptureArgs([]string{"--version"})
	if ok {
		t.Fatalf("CaptureArgs applied to informational ruff call: %#v", got)
	}
}

func mapByName(tools []toolcatalog.Tool) map[string]toolcatalog.Tool {
	byName := make(map[string]toolcatalog.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	return byName
}

func assertToolCommand(t *testing.T, tool toolcatalog.Tool, want []string) {
	t.Helper()

	if !reflect.DeepEqual(tool.Command, want) {
		t.Fatalf("%s command = %#v, want %#v", tool.Name, tool.Command, want)
	}
}

func assertToolFileMetadata(
	t *testing.T,
	tool toolcatalog.Tool,
	extensions []string,
	prefixes []string,
	basePrefixes []string,
) {
	t.Helper()

	if !reflect.DeepEqual(tool.FileExtensions, extensions) ||
		!reflect.DeepEqual(tool.FilePrefixes, prefixes) ||
		!reflect.DeepEqual(tool.BaseNamePrefixes, basePrefixes) {
		t.Fatalf("file metadata for %s = %#v", tool.Name, tool)
	}
}
