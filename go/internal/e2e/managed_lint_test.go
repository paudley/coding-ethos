// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package e2e_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/e2e"
)

const windowsGOOS = "windows"

func TestManagedRuffCaptureRunsRealToolAgainstReferenceRepo(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	clean := repo.CodingEthosRun(t, "policy-tool", "ruff", "check", "pkg/clean.py")
	clean.RequireExit(t, 0)

	assertCleanRuffOutput(t, clean.Combined)

	if strings.TrimSpace(clean.Combined) != "" {
		t.Fatalf("clean output should be silent:\n%s", clean.Combined)
	}

	cleanTrace := repo.SingleTrace(t)
	assertCleanRuffTrace(t, cleanTrace)

	repo.ResetTraces(t)
	repo.Touch(t, "pkg/unused_import.py", unusedImportPython())
	finding := repo.CodingEthosRun(
		t,
		"policy-tool",
		"ruff",
		"check",
		"pkg/unused_import.py",
	)
	finding.RequireExit(t, 1)
	assertRuffFindingOutput(t, finding)

	findingTrace := repo.SingleTrace(t)
	assertRuffFindingTrace(t, findingTrace)
}

func preparedManagedLintRepo(t *testing.T) e2e.Repo {
	t.Helper()

	if testing.Short() {
		t.Skip("real managed tool e2e is skipped in short mode")
	}

	if runtime.GOOS == windowsGOOS {
		t.Skip("real managed tool e2e uses POSIX paths")
	}

	sourceRoot := repoRootFromWorkingDirectory(t)
	e2e.RequireRuntime(t, sourceRoot)
	runtimeRoot := e2e.InstrumentedEthosRoot(t, sourceRoot)
	repo := e2e.FromReference(t, sourceRoot, "policy-lint-basic")
	repo.EthosRoot = runtimeRoot

	sync := repo.CodingEthosRun(
		t,
		"policy",
		"sync-tool-configs",
		"--ethos-root",
		runtimeRoot,
		"--repo",
		repo.Root,
	)
	sync.RequireExit(t, 0)

	return repo
}

func assertCleanRuffOutput(t *testing.T, output string) {
	t.Helper()

	for _, unwanted := range []string{
		"tool.output_visible",
		"ruff emitted output while passing",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("clean output contained %q:\n%s", unwanted, output)
		}
	}
}

func assertCleanRuffTrace(t *testing.T, cleanTrace string) {
	t.Helper()

	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"tool": "ruff"`,
		`"parser": "ruff"`,
		`"parse_status": "empty"`,
		`"exit_code": 0`,
		`"--output-format=json"`,
	} {
		if !strings.Contains(cleanTrace, want) {
			t.Fatalf("clean trace missing %q:\n%s", want, cleanTrace)
		}
	}

	if strings.Contains(cleanTrace, `"policy_id": "tool.output_visible"`) {
		t.Fatalf(
			"clean trace should not contain output-visible policy:\n%s",
			cleanTrace,
		)
	}
}

func assertRuffFindingOutput(t *testing.T, finding e2e.CommandResult) {
	t.Helper()

	for _, want := range []string{
		"format:",
		"tool: ruff",
		"status: FAIL",
		"pkg/unused_import.py",
		"F401",
		"imported but unused",
	} {
		finding.RequireContains(t, want)
	}
}

func assertRuffFindingTrace(t *testing.T, findingTrace string) {
	t.Helper()

	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"file": "pkg/unused_import.py"`,
		`"code": "F401"`,
		`"message": "`,
	} {
		if !strings.Contains(findingTrace, want) {
			t.Fatalf("finding trace missing %q:\n%s", want, findingTrace)
		}
	}
}

func TestManagedRuffCaptureProducesRealSARIF(t *testing.T) {
	t.Parallel()

	repo := preparedManagedLintRepo(t)
	repo.Touch(t, "pkg/unused_import.py", unusedImportPython())
	result := repo.CodingEthosRun(
		t,
		"policy-lint",
		"--sarif",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		repo.EthosRoot,
		"--consumer-root",
		repo.Root,
		"--invocation-cwd",
		repo.Root,
		"--",
		"check",
		"pkg/unused_import.py",
	)
	result.RequireExit(t, 1)
	assertRuffSARIFOutput(t, result)
}

func assertRuffSARIFOutput(t *testing.T, result e2e.CommandResult) {
	t.Helper()

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"tool": {`,
		`"ruleId": "`,
		`"uri": "pkg/unused_import.py"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
	} {
		result.RequireContains(t, want)
	}
}

func unusedImportPython() string {
	return `# SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
# SPDX-License-Identifier: MIT

"""Temporary e2e fixture containing a real Ruff F401 finding."""

import json


def answer() -> int:
    """Return a stable value."""
    return 42
`
}

func repoRootFromWorkingDirectory(t *testing.T) string {
	t.Helper()

	workingDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for current := workingDirectory; ; current = filepath.Dir(current) {
		_, inlineErrAutoA := os.Stat(filepath.Join(current, "coding_ethos.yml"))
		if inlineErrAutoA == nil {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			t.Fatalf("could not find coding-ethos root from %s", workingDirectory)
		}
	}
}
