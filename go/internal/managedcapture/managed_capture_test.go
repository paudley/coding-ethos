// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package managedcapture //nolint:testpackage

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
	"blackcat.ca/coding-ethos/go/lintcapture"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	managedCaptureExecutableMode os.FileMode = 0o700
	windowsGOOS                              = "windows"
)

func TestManagedRuffFormatDoesNotForceJsonOutput(t *testing.T) {
	t.Parallel()

	consumerRoot := filepath.Join("tmp", "consumer")

	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	enforced := enforceManagedToolArgs(
		tool,
		[]string{"format", "--check", "lib/python/pkg.py"},
		consumerRoot,
	)
	got := capturedToolArgs("ruff", enforced)

	want := []string{
		"format",
		"--config",
		filepath.Join(consumerRoot, "ruff.toml"),
		"--check",
		"lib/python/pkg.py",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("managed ruff format args = %#v, want %#v", got, want)
	}
}

func TestManagedSubcommandConfigPlacement(t *testing.T) {
	t.Parallel()

	consumerRoot := filepath.Join("tmp", "consumer")
	tests := managedSubcommandConfigPlacementCases(consumerRoot)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tool, found := toolcatalog.HookOwnedTool(test.name)
			if !found {
				t.Fatalf("missing %s tool", test.name)
			}

			enforced := enforceManagedToolArgs(tool, test.args, consumerRoot)

			got := capturedToolArgs(test.name, enforced)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("managed %s args = %#v, want %#v", test.name, got, test.want)
			}
		})
	}
}

type managedSubcommandConfigPlacementCase struct {
	name string
	args []string
	want []string
}

func managedSubcommandConfigPlacementCases(
	consumerRoot string,
) []managedSubcommandConfigPlacementCase {
	return []managedSubcommandConfigPlacementCase{
		sqlfluffManagedArgsCase(consumerRoot),
		golangciLintManagedArgsCase(consumerRoot),
		golangciLintAutofixManagedArgsCase(consumerRoot),
		goTestManagedArgsCase(),
	}
}

func sqlfluffManagedArgsCase(consumerRoot string) managedSubcommandConfigPlacementCase {
	return managedSubcommandConfigPlacementCase{
		name: "sqlfluff",
		args: []string{"lint", "queries/report.sql"},
		want: []string{
			"lint",
			"--format",
			"json",
			"--config",
			filepath.Join(consumerRoot, ".sqlfluff"),
			"queries/report.sql",
		},
	}
}

func golangciLintManagedArgsCase(
	consumerRoot string,
) managedSubcommandConfigPlacementCase {
	return managedSubcommandConfigPlacementCase{
		name: "golangci-lint",
		args: []string{"go"},
		want: []string{
			"run",
			"--output.json.path=stdout",
			"--output.text.path=stderr",
			"--config",
			filepath.Join(consumerRoot, ".golangci.yml"),
			"go",
		},
	}
}

func golangciLintAutofixManagedArgsCase(
	consumerRoot string,
) managedSubcommandConfigPlacementCase {
	return managedSubcommandConfigPlacementCase{
		name: "golangci-lint-autofix",
		args: []string{"go"},
		want: []string{
			"run",
			"--fix",
			"--config",
			filepath.Join(consumerRoot, ".golangci.yml"),
			"go",
		},
	}
}

func goTestManagedArgsCase() managedSubcommandConfigPlacementCase {
	return managedSubcommandConfigPlacementCase{
		name: "go-test",
		args: []string{"go"},
		want: []string{
			"test",
			"-json",
			"-cover",
			"-buildvcs=false",
			"-timeout=30s",
			"-short",
			"go",
		},
	}
}

func TestNormalizeGolangciLintWorktreeRunsInsideModule(t *testing.T) {
	t.Parallel()

	consumerRoot := t.TempDir()
	goRoot := filepath.Join(consumerRoot, "go")
	writeManagedCaptureFile(
		t,
		filepath.Join(goRoot, "go.mod"),
		"module example.test/repo\n",
	)

	cwd, args := normalizeGolangciLintWorktree(
		consumerRoot,
		consumerRoot,
		[]string{
			"run",
			"--output.json.path=stdout",
			"--config",
			filepath.Join(consumerRoot, ".golangci.yml"),
			"go",
		},
	)

	if cwd != goRoot {
		t.Fatalf("normalized cwd = %q, want %q", cwd, goRoot)
	}

	wantArgs := []string{
		"run",
		"--output.json.path=stdout",
		"--config",
		filepath.Join(consumerRoot, ".golangci.yml"),
		"./...",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized args = %#v, want %#v", args, wantArgs)
	}
}

func TestNormalizeGolangciLintWorktreeDefaultsToNestedModule(t *testing.T) {
	t.Parallel()

	consumerRoot := t.TempDir()
	goRoot := filepath.Join(consumerRoot, "go")
	writeManagedCaptureFile(
		t,
		filepath.Join(goRoot, "go.mod"),
		"module example.test/repo\n",
	)

	cwd, args := normalizeGolangciLintWorktree(
		consumerRoot,
		consumerRoot,
		[]string{
			"run",
			"--output.json.path=stdout",
			"--config",
			filepath.Join(consumerRoot, ".golangci.yml"),
		},
	)

	if cwd != goRoot {
		t.Fatalf("normalized cwd = %q, want %q", cwd, goRoot)
	}

	wantArgs := []string{
		"run",
		"--output.json.path=stdout",
		"--config",
		filepath.Join(consumerRoot, ".golangci.yml"),
		"./...",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized args = %#v, want %#v", args, wantArgs)
	}
}

func TestNormalizeGolangciLintWorktreeDefaultsToInvocationNestedModule(
	t *testing.T,
) {
	t.Parallel()

	consumerRoot := t.TempDir()
	invocationCwd := filepath.Join(consumerRoot, "coding-ethos")
	goRoot := filepath.Join(invocationCwd, "go")
	writeManagedCaptureFile(
		t,
		filepath.Join(goRoot, "go.mod"),
		"module example.test/repo\n",
	)

	cwd, args := normalizeGolangciLintWorktree(
		consumerRoot,
		invocationCwd,
		[]string{
			"fmt",
			"--config",
			filepath.Join(consumerRoot, ".golangci.yml"),
		},
	)

	if cwd != goRoot {
		t.Fatalf("normalized cwd = %q, want %q", cwd, goRoot)
	}

	wantArgs := []string{
		"fmt",
		"--config",
		filepath.Join(consumerRoot, ".golangci.yml"),
		"./...",
	}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized args = %#v, want %#v", args, wantArgs)
	}
}

func TestNormalizeGoToolWorktreeRunsInsideModule(t *testing.T) {
	t.Parallel()

	consumerRoot := t.TempDir()
	goRoot := filepath.Join(consumerRoot, "go")
	writeManagedCaptureFile(
		t,
		filepath.Join(goRoot, "go.mod"),
		"module example.test/repo\n",
	)

	cwd, args := normalizeGoToolWorktree(
		consumerRoot,
		consumerRoot,
		[]string{"test", "-json", "go", "-run", "TestThing"},
	)

	if cwd != goRoot {
		t.Fatalf("normalized cwd = %q, want %q", cwd, goRoot)
	}

	wantArgs := []string{"test", "-json", "./...", "-run", "TestThing"}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized args = %#v, want %#v", args, wantArgs)
	}
}

func TestNormalizeGoToolWorktreeDefaultsToNestedModule(t *testing.T) {
	t.Parallel()

	consumerRoot := t.TempDir()
	goRoot := filepath.Join(consumerRoot, "go")
	writeManagedCaptureFile(
		t,
		filepath.Join(goRoot, "go.mod"),
		"module example.test/repo\n",
	)

	cwd, args := normalizeGoToolWorktree(
		consumerRoot,
		consumerRoot,
		[]string{"vet"},
	)

	if cwd != goRoot {
		t.Fatalf("normalized cwd = %q, want %q", cwd, goRoot)
	}

	wantArgs := []string{"vet", "./..."}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized args = %#v, want %#v", args, wantArgs)
	}
}

func TestNormalizeGoToolWorktreeDefaultsToInvocationNestedModule(t *testing.T) {
	t.Parallel()

	consumerRoot := t.TempDir()
	invocationCwd := filepath.Join(consumerRoot, "coding-ethos")
	goRoot := filepath.Join(invocationCwd, "go")
	writeManagedCaptureFile(
		t,
		filepath.Join(goRoot, "go.mod"),
		"module example.test/repo\n",
	)

	cwd, args := normalizeGoToolWorktree(
		consumerRoot,
		invocationCwd,
		[]string{"vet"},
	)

	if cwd != goRoot {
		t.Fatalf("normalized cwd = %q, want %q", cwd, goRoot)
	}

	wantArgs := []string{"vet", "./..."}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Fatalf("normalized args = %#v, want %#v", args, wantArgs)
	}
}

func TestManagedCaptureEnforcesToolSpecificArgs(t *testing.T) {
	t.Parallel()

	consumerRoot := t.TempDir()
	venvPython := filepath.Join(consumerRoot, ".venv", "bin", "python")
	writeManagedCaptureFile(t, venvPython, "#!/bin/sh\nexit 0\n")
	chmodManagedCaptureExecutable(t, venvPython)

	tests := []struct {
		tool string
		args []string
		want []string
	}{
		{
			tool: "mypy",
			args: []string{"pkg"},
			want: []string{
				"--config-file",
				filepath.Join(consumerRoot, "mypy.ini"),
				"--python-executable",
				venvPython,
				"pkg",
			},
		},
		{
			tool: "dotenv-linter",
			args: []string{"check", ".env"},
			want: []string{"--plain", "--quiet", "check", ".env"},
		},
		{
			tool: "tombi",
			args: []string{"lint", "--quiet", "--error-on-warnings", "config.toml"},
			want: []string{"lint", "--quiet", "--error-on-warnings", "config.toml"},
		},
		{
			tool: "yamllint",
			args: []string{"."},
			want: []string{
				"-c",
				filepath.Join(consumerRoot, ".yamllint.yml"),
				".",
				"--strict",
				"-f",
				"parsable",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.tool, func(t *testing.T) {
			t.Parallel()

			tool, found := toolcatalog.HookOwnedTool(test.tool)
			if !found {
				t.Fatalf("missing %s tool", test.tool)
			}

			got := enforceManagedToolArgs(tool, test.args, consumerRoot)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("%s enforced args = %#v, want %#v", test.tool, got, test.want)
			}
		})
	}
}

func TestFormatterCandidateWalkSkipsCachesAndBuildArtifacts(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, path := range []string{
		"pkg/a.go",
		"pkg/b.py",
		".git/ignored.go",
		".coding-ethos/cache/ignored.go",
		".ruff_cache/ignored.py",
		"build/ignored.go",
		"node_modules/ignored.go",
	} {
		writeManagedCaptureFile(t, filepath.Join(root, path), "package example\n")
	}

	got := walkFormatterCandidateFiles(root, []string{".go"})
	want := []string{filepath.Join(root, "pkg", "a.go")}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatter candidates = %#v, want %#v", got, want)
	}
}

func TestCapturedResultBlocksOnParsedErrorDiagnostics(t *testing.T) {
	t.Parallel()

	result := capturedToolResult(
		captureRequest{Tool: "golangci-lint"},
		captureExecution{ExitCode: 0},
	)
	result.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     "golangci-lint",
		Severity: "error",
		Message:  "parsed linter diagnostic",
	}}
	result.Findings = nil

	if !capturedResultBlocks(result) {
		t.Fatal("parsed error diagnostics must block even when findings are absent")
	}
}

func TestRunManagedCaptureExecutesFromConsumerRoot(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	consumerParent := t.TempDir()
	consumerRoot := filepath.Join(consumerParent, "consumer")
	ethosRoot := t.TempDir()
	writeManagedCaptureFile(t, filepath.Join(ethosRoot, "config.yaml"), "version: 1\n")

	_, err := toolconfigs.Sync(ethosRoot, consumerRoot, "")
	if err != nil {
		t.Fatalf("sync generated tool configs: %v", err)
	}

	writeManagedCaptureFile(
		t,
		filepath.Join(
			consumerRoot,
			"lbox-platform",
			"lib",
			"python",
			"tests",
			"app.py",
		),
		"import os\n",
	)

	uvFixture := filepath.Join(t.TempDir(), "uv")
	writeManagedCaptureFile(t, uvFixture, managedCaptureUVFixture())

	chmodManagedCaptureExecutable(t, uvFixture)

	t.Setenv("UV", uvFixture)
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	output := captureStdout(t, func() {
		exitCode := Run(Options{
			Tool:          "ruff",
			EthosRoot:     ethosRoot,
			ConsumerRoot:  consumerRoot,
			InvocationCwd: consumerRoot,
			Args: []string{
				"check",
				filepath.Join(
					consumerRoot,
					"lbox-platform",
					"lib",
					"python",
					"tests",
					"app.py",
				),
			},
		})
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
	})
	if !strings.Contains(output, "lbox-platform/lib/python/tests/app.py") {
		t.Fatalf("output missing repo-relative file:\n%s", output)
	}
}

func managedCaptureUVFixture() string {
	return `#!/usr/bin/env sh
case " $* " in
  *" --quiet "*) ;;
  *) echo "missing quiet uv mode: $*" >&2; exit 2 ;;
esac
case " $* " in
  *" --check-tool-configs"*) exit 0 ;;
esac
case "$PWD" in
  *"/consumer") ;;
  *) echo "wrong cwd: $PWD" >&2; exit 2 ;;
esac
case " $* " in
  *" lbox-platform/lib/python/tests/app.py "*) ;;
  *) echo "missing repo-relative target: $*" >&2; exit 2 ;;
esac
cat <<'JSON'
[
  {
    "filename": "lbox-platform/lib/python/tests/app.py",
    "code": "F401",
    "message": "unused import",
    "location": {"row": 1, "column": 8}
  }
]
JSON
exit 1
`
}

func TestManagedToolCommandPrefersCheckoutVenvTool(t *testing.T) {
	t.Parallel()

	ethosRoot := t.TempDir()
	ruffPath := filepath.Join(ethosRoot, ".venv", "bin", "ruff")
	writeManagedCaptureFile(t, ruffPath, "#!/bin/sh\nexit 0\n")
	chmodManagedCaptureExecutable(t, ruffPath)

	pythonPath := filepath.Join(ethosRoot, ".venv", "bin", "python")
	writeManagedCaptureFile(t, pythonPath, "#!/bin/sh\nexit 1\n")
	chmodManagedCaptureExecutable(t, pythonPath)

	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	command := managedToolCommandFor(tool, ethosRoot)
	if command.Path != ruffPath {
		t.Fatalf("managed command path = %q, want %q", command.Path, ruffPath)
	}

	if len(command.Prefix) != 0 {
		t.Fatalf("managed command prefix = %#v, want empty", command.Prefix)
	}
}

func TestManagedToolCommandFallsBackToPythonModule(t *testing.T) {
	t.Parallel()

	ethosRoot := t.TempDir()
	pythonPath := filepath.Join(ethosRoot, ".venv", "bin", "python")
	writeManagedCaptureFile(t, pythonPath, `#!/bin/sh
case " $* " in
  *" -m ruff --version "*) exit 0 ;;
esac
exit 1
`)

	chmodManagedCaptureExecutable(t, pythonPath)

	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	command := managedToolCommandFor(tool, ethosRoot)
	if command.Path != pythonPath {
		t.Fatalf("managed command path = %q, want %q", command.Path, pythonPath)
	}

	wantPrefix := []string{"-m", "ruff"}
	if !reflect.DeepEqual(command.Prefix, wantPrefix) {
		t.Fatalf("managed command prefix = %#v, want %#v", command.Prefix, wantPrefix)
	}
}

func TestManagedActionlintUsesManagedExecutableWhenPresent(t *testing.T) {
	t.Parallel()

	ethosRoot := t.TempDir()
	actionlintPath := filepath.Join(
		ethosRoot,
		"build",
		"toolchain",
		"go-bin",
		"actionlint",
	)
	writeManagedCaptureFile(t, actionlintPath, "#!/bin/sh\nexit 0\n")

	chmodManagedCaptureExecutable(t, actionlintPath)

	tool, found := toolcatalog.HookOwnedTool("actionlint")
	if !found {
		t.Fatal("missing actionlint tool")
	}

	command := managedToolCommandFor(tool, ethosRoot)
	if command.Path != actionlintPath {
		t.Fatalf("managed command path = %q, want %q", command.Path, actionlintPath)
	}

	if len(command.Prefix) != 0 {
		t.Fatalf("managed command prefix = %#v, want empty", command.Prefix)
	}
}

func TestManagedGoToolResolutionUsesRuntimeOrFallback(t *testing.T) {
	t.Parallel()

	goVet, found := toolcatalog.HookOwnedTool("go-vet")
	if !found {
		t.Fatal("missing go-vet tool")
	}

	goCommand := managedGoRuntimeCommand(goVet)
	if filepath.Base(goCommand.Path) != "go" {
		t.Fatalf("go-vet command = %#v, want go runtime", goCommand)
	}

	actionlint, found := toolcatalog.HookOwnedTool("actionlint")
	if !found {
		t.Fatal("missing actionlint tool")
	}

	fallback := managedGoToolFallback(actionlint)
	if filepath.Base(fallback.Path) != "go" ||
		!slices.Contains(fallback.Prefix, "run") {
		t.Fatalf("actionlint fallback = %#v, want go run fallback", fallback)
	}

	ruff, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	if command := managedGoRuntimeCommand(ruff); command.Path != "" {
		t.Fatalf("ruff go runtime command = %#v, want empty", command)
	}

	if command := managedGoToolFallback(ruff); command.Path != "" {
		t.Fatalf("ruff go fallback command = %#v, want empty", command)
	}
}

func TestSandboxCapabilitiesIncludeConsumerReadWritePaths(t *testing.T) {
	t.Parallel()

	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	config := lintcapture.RuntimeConfig{
		Merged: map[string]any{
			"sandbox": map[string]any{
				"read_write_paths": []any{"/opt/foundation", "/opt/src/vllm"},
				"rw_paths":         []any{"/scratch/lbox"},
			},
		},
	}

	capabilities := sandboxCapabilities(tool, config)
	if !slices.Contains(capabilities.Tags, "no-network") {
		t.Fatalf("sandbox capabilities missing no-network tag: %#v", capabilities)
	}

	for _, want := range []string{"/opt/foundation", "/opt/src/vllm", "/scratch/lbox"} {
		if !slices.Contains(capabilities.WritePaths, want) {
			t.Fatalf("sandbox write paths missing %q: %#v", want, capabilities)
		}
	}
}

func TestManagedToolEnabledHonorsPrincipleBanditDisable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethosRoot := filepath.Join(root, "coding-ethos")
	consumerRoot := filepath.Join(root, "consumer")
	writeManagedCaptureFile(t, filepath.Join(ethosRoot, "config.yaml"), `
version: 1
tooling:
  bandit: {}
`)
	writeManagedCaptureFile(
		t,
		filepath.Join(ethosRoot, "coding_ethos.yml"),
		"principles: []\n",
	)
	writeManagedCaptureFile(t, filepath.Join(consumerRoot, "repo_ethos.yml"), `
principles:
  additional:
    - id: repo-security-tools
      tool_config:
        bandit:
          enabled: false
`)

	enabled, err := managedToolEnabled("bandit", ethosRoot, consumerRoot)
	if err != nil {
		t.Fatalf("managedToolEnabled(): %v", err)
	}
	if enabled {
		t.Fatal("managedToolEnabled() = true, want false")
	}
}

func writeManagedCaptureFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
}

func chmodManagedCaptureExecutable(t *testing.T, path string) {
	t.Helper()

	err := os.Chmod(path, managedCaptureExecutableMode)
	if err != nil {
		t.Fatalf("chmod executable fixture: %v", err)
	}
}
