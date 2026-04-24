package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPyupgradeFlagForVersion(t *testing.T) {
	if got := pyupgradeFlagForVersion("3.13"); got != "--py313-plus" {
		t.Fatalf("pyupgradeFlagForVersion() = %q, want %q", got, "--py313-plus")
	}
}

func TestCollectPythonVersionIssues(t *testing.T) {
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
	mustWriteTestFile(t, filepath.Join(tempDir, "pyrightconfig.json"), "{\n  \"pythonVersion\": \"3.14\"\n}\n")
	mustWriteTestFile(t, filepath.Join(tempDir, "ruff.toml"), "target-version = \"py312\"\n")

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
	mustWriteTestFile(t, filepath.Join(tempDir, "pre-commit", "lefthook.yml"), "min_version: 1.13.6\n")
	if err := os.MkdirAll(filepath.Join(tempDir, "pre-commit", "hooks"), 0o755); err != nil {
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
	mustWriteTestFile(t, filepath.Join(tempDir, "pyrightconfig.json"), "{\n  \"pythonVersion\": \"3.13\"\n}\n")
	mustWriteTestFile(t, filepath.Join(tempDir, "ruff.toml"), "target-version = \"py313\"\n")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previous); chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	output := captureStderr(t, func() {
		if got := checkPythonVersionConsistencyCommand(Config{}, nil); got != 1 {
			t.Fatalf("checkPythonVersionConsistencyCommand() = %d, want 1", got)
		}
	})
	if !strings.Contains(output, "PYTHON VERSION CONSISTENCY CHECK FAILED") {
		t.Fatalf("unexpected output: %q", output)
	}
	if !strings.Contains(output, ".python-version [version]") {
		t.Fatalf("missing .python-version mismatch: %q", output)
	}
	if !strings.Contains(output, "pyproject.toml [project.requires-python]") {
		t.Fatalf("missing pyproject mismatch: %q", output)
	}
	if !strings.Contains(output, "mypy.ini [mypy.python_version]") {
		t.Fatalf("missing mypy mismatch: %q", output)
	}
}

func TestCheckPythonVersionConsistencyCommandUsesConsumerRoot(t *testing.T) {
	tempDir := t.TempDir()
	cmd := exec.Command("git", "init")
	cmd.Dir = tempDir
	if output, err := cmd.CombinedOutput(); err != nil {
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
`)+"\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "code-ethos", "pre-commit", "lefthook.yml"),
		"min_version: 1.13.6\n",
	)
	if err := os.MkdirAll(filepath.Join(tempDir, "code-ethos", "pre-commit", "hooks"), 0o755); err != nil {
		t.Fatalf("os.MkdirAll() failed: %v", err)
	}

	mustWriteTestFile(t, filepath.Join(tempDir, ".python-version"), "3.14\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyproject.toml"),
		strings.TrimSpace(`
[project]
requires-python = ">=3.13"
`)+"\n",
	)
	mustWriteTestFile(t, filepath.Join(tempDir, "mypy.ini"), "[mypy]\npython_version = 3.13\n")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "pyrightconfig.json"),
		"{\n  \"pythonVersion\": \"3.13\"\n}\n",
	)
	mustWriteTestFile(t, filepath.Join(tempDir, "ruff.toml"), "target-version = \"py313\"\n")

	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		if chdirErr := os.Chdir(previous); chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	output := captureStderr(t, func() {
		if got := checkPythonVersionConsistencyCommand(Config{}, nil); got != 1 {
			t.Fatalf("checkPythonVersionConsistencyCommand() = %d, want 1", got)
		}
	})
	if !strings.Contains(output, ".python-version [version]") {
		t.Fatalf("expected consumer-root .python-version mismatch, got %q", output)
	}
}

func TestCollectPythonVersionIssuesReportsMissingValues(t *testing.T) {
	tempDir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(tempDir, "pyproject.toml"), "[project]\nname = \"demo\"\n")
	mustWriteTestFile(t, filepath.Join(tempDir, "mypy.ini"), "[mypy]\nstrict = True\n")
	mustWriteTestFile(t, filepath.Join(tempDir, "pyrightconfig.json"), "{\n  \"include\": [\"src\"]\n}\n")
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
			t.Fatalf("issue %s[%s] found = %q, want <missing>", issue.Path, issue.Field, issue.Found)
		}
	}
}
