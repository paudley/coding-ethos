// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

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
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
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
		},
		{
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
		},
		{
			name: "golangci-lint-autofix",
			args: []string{"go"},
			want: []string{
				"run",
				"--fix",
				"--config",
				filepath.Join(consumerRoot, ".golangci.yml"),
				"go",
			},
		},
	}

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
		filepath.Join(consumerRoot, "lbox-platform", "lib", "python", "tests", "app.py"),
		"import os\n",
	)

	uvFixture := filepath.Join(t.TempDir(), "uv")
	writeManagedCaptureFile(t, uvFixture, `#!/usr/bin/env sh
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
`)

	chmodManagedCaptureExecutable(t, uvFixture)

	t.Setenv("UV", uvFixture)
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	output := captureStdout(t, func() {
		exitCode := runManagedCapture(managedCaptureOptions{
			Tool:          "ruff",
			EthosRoot:     ethosRoot,
			ConsumerRoot:  consumerRoot,
			InvocationCwd: consumerRoot,
			Args: []string{
				"check",
				filepath.Join(consumerRoot, "lbox-platform", "lib", "python", "tests", "app.py"),
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

func TestManagedToolCommandPrefersCheckoutVenvTool(t *testing.T) {
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
