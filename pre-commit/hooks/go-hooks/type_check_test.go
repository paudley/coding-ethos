// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTypeCheckerCommandInjectsRepoConfig(t *testing.T) {
	tempDir := t.TempDir()
	bundleRoot := filepath.Join(tempDir, "pre-commit")
	consumerRoot := filepath.Join(tempDir, "repo")
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "pyproject.toml"),
		"[tool.mypy]\n",
	)
	mustWriteTestFile(t, filepath.Join(consumerRoot, "pyrightconfig.json"), "{}\n")

	command := resolveTypeCheckerCommand(
		typeCheckerConfig{
			Name:                 "pyright",
			Command:              []string{"pyright"},
			PassFilesAsArgs:      true,
			UseHookProject:       true,
			ConfigFlags:          []string{"--project", "-p"},
			RepoConfig:           "pyrightconfig.json",
			FallbackBundleConfig: "hooks/pyproject.toml",
		},
		typeCheckSettings{
			BundleRoot:   bundleRoot,
			ConsumerRoot: consumerRoot,
			HooksProject: filepath.Join(bundleRoot, "hooks"),
		},
	)

	wantPrefix := []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		filepath.Join(bundleRoot, "hooks"),
		"pyright",
		"--project",
		filepath.Join(consumerRoot, "pyrightconfig.json"),
	}
	if len(command) < len(wantPrefix) {
		t.Fatalf(
			"resolveTypeCheckerCommand() = %#v, want prefix %#v",
			command,
			wantPrefix,
		)
	}
	for i := range wantPrefix {
		if command[i] != wantPrefix[i] {
			t.Fatalf(
				"command[%d] = %q, want %q (%#v)",
				i,
				command[i],
				wantPrefix[i],
				command,
			)
		}
	}
}

func TestNormalizeTypeCheckFiles(t *testing.T) {
	tempDir := t.TempDir()
	pythonFile := filepath.Join(tempDir, "pkg", "module.py")
	dockerFile := filepath.Join(tempDir, "pkg", "docker", "script.py")
	venvFile := filepath.Join(tempDir, ".venv", "lib.py")
	whitelist := filepath.Join(tempDir, "vulture_whitelist.py")
	mustWriteTestFile(t, pythonFile, "value = 1\n")
	mustWriteTestFile(t, dockerFile, "value = 1\n")
	mustWriteTestFile(t, venvFile, "value = 1\n")
	mustWriteTestFile(t, whitelist, "value\n")

	files := normalizeTypeCheckFiles(
		[]string{pythonFile, dockerFile, venvFile, whitelist, pythonFile},
		[]string{"/docker/", "vulture_whitelist"},
	)
	if len(files) != 1 || files[0] != pythonFile {
		t.Fatalf("normalizeTypeCheckFiles() = %#v, want [%q]", files, pythonFile)
	}
}

func TestNormalizeTypeCheckFilesHonorsConfiguredExcludedPathFragments(t *testing.T) {
	tempDir := t.TempDir()
	pythonFile := filepath.Join(tempDir, "pkg", "module.py")
	generatedFile := filepath.Join(tempDir, "pkg", "generated", "script.py")
	mustWriteTestFile(t, pythonFile, "value = 1\n")
	mustWriteTestFile(t, generatedFile, "value = 1\n")

	files := normalizeTypeCheckFiles(
		[]string{pythonFile, generatedFile},
		[]string{"/generated/"},
	)
	if len(files) != 1 || files[0] != pythonFile {
		t.Fatalf("normalizeTypeCheckFiles() = %#v, want [%q]", files, pythonFile)
	}
}

func TestCheckTypeCheckersCommandReportsFailures(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  type_check:
    enabled: true
    checkers:
      - name: ok
        command:
          - /bin/sh
          - -lc
          - exit 0
        pass_files_as_args: false
        use_hook_project: false
      - name: fail
        command:
          - /bin/sh
          - -lc
          - printf 'type failure\n'; exit 1
        pass_files_as_args: false
        use_hook_project: false
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	pythonPath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(t, pythonPath, "value = 1\n")

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			if got := checkTypeCheckersCommand(Config{}, []string{pythonPath}); got != 1 {
				t.Fatalf("checkTypeCheckersCommand() = %d, want 1", got)
			}
		})
		if !strings.Contains(
			stderr,
			"type checking failed in one or more configured checkers",
		) {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
		if !strings.Contains(
			stderr,
			"Fix the reported checker output above and run the hook again.",
		) {
			t.Fatalf("missing remediation guidance in stderr: %q", stderr)
		}
	})
	if !strings.Contains(stdout, "TYPE CHECKING (PARALLEL)") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
	if !strings.Contains(stdout, "fail: FAIL") {
		t.Fatalf("missing failing checker report: %q", stdout)
	}
}
