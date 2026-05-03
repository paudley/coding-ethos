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

	"blackcat.ca/coding-ethos/go/lintcapture"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

func TestManagedRuffFormatDoesNotForceJsonOutput(t *testing.T) {
	t.Parallel()

	ethosRoot := filepath.Join("tmp", "coding-ethos")
	consumerRoot := filepath.Join("tmp", "consumer")
	tool, found := toolcatalog.HookOwnedTool("ruff")
	if !found {
		t.Fatal("missing ruff tool")
	}

	enforced := enforceManagedToolArgs(
		tool,
		[]string{"format", "--check", "lib/python/pkg.py"},
		consumerRoot,
		ethosRoot,
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

	ethosRoot := filepath.Join("tmp", "coding-ethos")
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
			args: []string{"run", "./..."},
			want: []string{
				"run",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
				"--config",
				filepath.Join(consumerRoot, ".golangci.yml"),
				"./...",
			},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tool, found := toolcatalog.HookOwnedTool(test.name)
			if !found {
				t.Fatalf("missing %s tool", test.name)
			}

			enforced := enforceManagedToolArgs(tool, test.args, consumerRoot, ethosRoot)
			got := capturedToolArgs(test.name, enforced)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("managed %s args = %#v, want %#v", test.name, got, test.want)
			}
		})
	}
}

func TestRunManagedCaptureExecutesFromConsumerRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	consumerParent := t.TempDir()
	consumerRoot := filepath.Join(consumerParent, "consumer")
	ethosRoot := t.TempDir()
	writeManagedCaptureFile(t, filepath.Join(ethosRoot, "config.yaml"), "version: 1\n")
	writeManagedCaptureFile(t, filepath.Join(ethosRoot, "ruff.toml"), "line-length = 88\n")
	writeManagedCaptureFile(t, filepath.Join(consumerRoot, ".code-ethos", "tool-config-hashes.json"), "{}\n")
	writeManagedCaptureFile(
		t,
		filepath.Join(consumerRoot, "lbox-platform", "lib", "python", "tests", "app.py"),
		"import os\n",
	)

	uvFixture := filepath.Join(t.TempDir(), "uv")
	writeManagedCaptureFile(t, uvFixture, `#!/usr/bin/env sh
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
printf '%s\n' '[{"filename":"lbox-platform/lib/python/tests/app.py","code":"F401","message":"unused import","location":{"row":1,"column":8}}]'
exit 1
`)
	if err := os.Chmod(uvFixture, 0o700); err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}
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

func writeManagedCaptureFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}
