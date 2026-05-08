// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:paralleltest // Uses process-global fixtures.
package hookrunnercli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPyupgradeFlagForVersion(t *testing.T) {
	t.Parallel()

	if got := pyupgradeFlagForVersion("3.13"); got != "--py313-plus" {
		t.Fatalf("pyupgradeFlagForVersion() = %q, want %q", got, "--py313-plus")
	}
}

func TestCollectPythonVersionIssues(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(tempDir, ".python-version"), "3.14\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyproject.toml"),
		strings.TrimSpace(`
[project]
requires-python = ">=3.11"
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "mypy.ini"),
		strings.TrimSpace(`
[mypy]
python_version = 3.12
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyrightconfig.json"),
		"{\n  \"pythonVersion\": \"3.14\"\n}\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "ruff.toml"),
		"target-version = \"py312\"\n",
	)

	issues, err := collectPythonVersionIssues("3.13", tempDir)
	if err != nil {
		t.Fatalf("collectPythonVersionIssues() returned error: %v", err)
	}

	if len(issues) != 5 {
		t.Fatalf("len(issues) = %d, want 5 (%#v)", len(issues), issues)
	}
}

func TestCheckPythonVersionConsistencyCommand(t *testing.T) {
	tempDir := t.TempDir()
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "config.yaml"),
		strings.TrimSpace(`
style:
  python_version: "3.13"
python:
  version_consistency:
    enabled: true
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pre-commit", "hooks", "managed-toolchain.tsv"),
		"#!/bin/sh\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pre-commit", "hooks", "pyproject.toml"),
		"",
	)

	err := os.MkdirAll(filepath.Join(tempDir, "pre-commit", "hooks"), 0o755)
	if err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}

	mustWriteTestFile(t, filepath.Join(tempDir, ".python-version"), "3.14\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyproject.toml"),
		strings.TrimSpace(`
[project]
requires-python = ">=3.11"
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "mypy.ini"),
		strings.TrimSpace(`
[mypy]
python_version = 3.12
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyrightconfig.json"),
		"{\n  \"pythonVersion\": \"3.13\"\n}\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "ruff.toml"),
		"target-version = \"py313\"\n",
	)

	t.Chdir(tempDir)

	output := captureStderr(t, func() {
		if got := checkPythonVersionConsistencyCommand(Config{}, nil); got != 1 {
			t.Fatalf("checkPythonVersionConsistencyCommand() = %d, want 1", got)
		}
	})
	if !strings.Contains(output, "PYTHON VERSION CONSISTENCY CHECK FAILED") {
		t.Fatalf("unexpected output: %q", output)
	}

	if !strings.Contains(output, ".python-version: [version]") {
		t.Fatalf("missing .python-version mismatch: %q", output)
	}

	if !strings.Contains(output, "pyproject.toml: [project.requires-python]") {
		t.Fatalf("missing pyproject mismatch: %q", output)
	}

	if !strings.Contains(output, "mypy.ini: [mypy.python_version]") {
		t.Fatalf("missing mypy mismatch: %q", output)
	}
}

func TestCheckPythonVersionConsistencyCommandUsesConsumerRoot(t *testing.T) {
	tempDir := pythonVersionConsumerFixture(t)

	mustWriteTestFile(t, filepath.Join(tempDir, ".python-version"), "3.14\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyproject.toml"),
		strings.TrimSpace(`
[project]
requires-python = ">=3.13"
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "mypy.ini"),
		"[mypy]\npython_version = 3.13\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyrightconfig.json"),
		"{\n  \"pythonVersion\": \"3.13\"\n}\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "ruff.toml"),
		"target-version = \"py313\"\n",
	)

	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)

	stderrOutput := captureStderr(t, func() {
		if got := checkPythonVersionConsistencyCommand(Config{}, nil); got != 1 {
			t.Fatalf("checkPythonVersionConsistencyCommand() = %d, want 1", got)
		}
	})
	if !strings.Contains(stderrOutput, ".python-version: [version]") {
		t.Fatalf(
			"expected consumer-root .python-version mismatch, got %q",
			stderrOutput,
		)
	}
}

func pythonVersionConsumerFixture(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()
	cmd := exec.CommandContext(context.Background(), "/usr/bin/git", "init")
	cmd.Dir = tempDir
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git init failed: %v\n%s", err, string(output))
	}

	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "code-ethos", "config.yaml"),
		strings.TrimSpace(`
style:
  python_version: "3.13"
python:
  version_consistency:
    enabled: true
`)+"\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "code-ethos", "pre-commit", "hooks", "managed-toolchain.tsv"),
		"#!/bin/sh\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "code-ethos", "pre-commit", "hooks", "pyproject.toml"),
		"",
	)

	return tempDir
}

func TestCollectPythonVersionIssuesReportsMissingValues(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyproject.toml"),
		"[project]\nname = \"demo\"\n",
	)
	mustWriteTestFile(t, filepath.Join(tempDir, "mypy.ini"), "[mypy]\nstrict = True\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyrightconfig.json"),
		"{\n  \"include\": [\"src\"]\n}\n",
	)
	mustWriteTestFile(t, filepath.Join(tempDir, "ruff.toml"), "line-length = 88\n")

	issues, err := collectPythonVersionIssues("3.13", tempDir)
	if err != nil {
		t.Fatalf("collectPythonVersionIssues() returned error: %v", err)
	}

	if len(issues) != 4 {
		t.Fatalf("len(issues) = %d, want 4 (%#v)", len(issues), issues)
	}

	for _, issue := range issues {
		if issue.Found != "<missing>" {
			t.Fatalf(
				"issue %s[%s] found = %q, want <missing>",
				issue.Path,
				issue.Field,
				issue.Found,
			)
		}
	}
}
