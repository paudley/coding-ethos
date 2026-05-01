// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package toolcatalog_test

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/toolcatalog"
)

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

	assertToolCommand(t, tools["hadolint"], []string{"hadolint", "--format", "json"})
	assertToolCommand(
		t,
		tools["actionlint"],
		[]string{"actionlint", "-format", "{{json .}}"},
	)
	assertToolCommand(
		t,
		tools["shellcheck"],
		[]string{"shellcheck", "--severity=warning", "-x", "--format=json"},
	)
	assertToolCommand(t, tools["shfmt"], []string{"shfmt", "-d", "-i", "2", "-ci", "-sr"})
	assertToolCommand(t, tools["yamllint"], []string{"yamllint"})
	assertToolCommand(t, tools["bandit"], []string{"bandit", "-q", "-f", "json"})
	assertToolCommand(t, tools["sqlfluff"], []string{"sqlfluff", "lint", "--format", "json"})
	assertToolCommand(t, tools["tombi"], []string{"tombi", "lint", "--quiet", "--error-on-warnings"})
	assertToolCommand(t, tools["dotenv-linter"], []string{"dotenv-linter", "--plain", "--quiet", "check"})
	assertToolCommand(t, tools["golangci-lint"], []string{"golangci-lint", "run"})

	for name, want := range map[string]string{
		"hadolint":      "docker",
		"actionlint":    "workflow",
		"shellcheck":    "shell",
		"shfmt":         "shell",
		"yamllint":      "syntax",
		"bandit":        "security",
		"sqlfluff":      "sql",
		"tombi":         "syntax",
		"dotenv-linter": "dotenv",
		"golangci-lint": "go-static",
	} {
		tool := tools[name]
		if tool.Category != want || tool.OutputFormat == "" || tool.Advice == "" {
			t.Fatalf("%s typed metadata mismatch: %#v", name, tool)
		}
	}

	assertToolFileMetadata(t, tools["hadolint"], nil, nil, []string{"Dockerfile"})
	assertToolFileMetadata(
		t,
		tools["actionlint"],
		[]string{".yaml", ".yml"},
		[]string{".github/workflows/"},
		nil,
	)
	assertToolFileMetadata(
		t,
		tools["shellcheck"],
		[]string{".sh", ".bash", ".zsh", ".ksh"},
		nil,
		nil,
	)
	assertToolFileMetadata(
		t,
		tools["shfmt"],
		[]string{".sh", ".bash", ".zsh", ".ksh"},
		nil,
		nil,
	)
	assertToolFileMetadata(t, tools["yamllint"], []string{".yaml", ".yml"}, nil, nil)
	assertToolFileMetadata(t, tools["bandit"], []string{".py"}, nil, nil)
	assertToolFileMetadata(t, tools["sqlfluff"], []string{".sql"}, nil, nil)
	assertToolFileMetadata(t, tools["tombi"], []string{".toml"}, nil, nil)
	assertToolFileMetadata(t, tools["dotenv-linter"], nil, nil, []string{".env"})
	assertToolFileMetadata(t, tools["golangci-lint"], []string{".go"}, nil, nil)

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

	golangci := tools["golangci-lint"]
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
		"gofmt",
		"go-vet",
		"go-test",
		"python-complexity",
		"python-maintainability",
		"python-vulture",
		"interrogate",
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
				t.Fatalf("%s missing Python module capture metadata: %#v", name, capture)
			}
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
	capture.OutputArgs[0] = "--broken"
	if tool.CaptureOutputArgs[0] == "--broken" {
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
	config.Flags = append(config.Flags, "--broken")
	if len(tool.ConfigFlags) > 0 && tool.ConfigFlags[len(tool.ConfigFlags)-1] == "--broken" {
		t.Fatal("ConfigSpec shared backing array with Tool")
	}
}

func TestManagedExecutablePathUsesCheckoutToolchain(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "repo", "coding-ethos")
	shellcheck, found := toolcatalog.HookOwnedTool("shellcheck")
	if !found {
		t.Fatal("missing shellcheck")
	}
	if got := shellcheck.ManagedExecutablePath(root); got != filepath.Join(root, "build", "toolchain", "github-bin", "shellcheck") {
		t.Fatalf("ManagedExecutablePath(shellcheck) = %q", got)
	}

	ruff, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff")
	}
	if got := ruff.ManagedExecutablePath(root); got != "" {
		t.Fatalf("ManagedExecutablePath(ruff) = %q, want empty for Python wrapper tools", got)
	}
}

func TestCapturedLintShimSpecsUseCatalogTools(t *testing.T) {
	t.Parallel()

	specs := toolcatalog.CapturedLintShimSpecs("/repo/run-go-hook.sh")
	byTool := map[string]toolcatalog.ShimSpec{}
	for _, spec := range specs {
		byTool[spec.ToolName] = spec
	}

	spec, found := byTool["ruff"]
	if !found {
		t.Fatal("missing ruff shim spec")
	}
	want := []string{"/repo/run-go-hook.sh", "policy-tool", "ruff"}
	if !reflect.DeepEqual(spec.Command, want) {
		t.Fatalf("ruff shim command = %#v, want %#v", spec.Command, want)
	}
}

func TestToolCaptureArgsForceCatalogOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want []string
	}{
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
			name: "golangci-lint",
			args: []string{"run", "--out-format", "colored-line-number", "./..."},
			want: []string{
				"run",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
				"./...",
			},
		},
	}

	for _, test := range tests {
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
