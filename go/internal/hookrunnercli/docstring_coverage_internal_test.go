// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:varnamelen // Uses process-global fixtures.
package hookrunnercli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildDocstringCoverageCommand(t *testing.T) {
	t.Parallel()

	command := buildDocstringCoverageCommand(
		docstringCoverageSettings{
			Threshold:                95,
			CheckPaths:               []string{"pkg", "pre-commit/hooks"},
			ExcludePatterns:          []string{`tests`, `\.venv`},
			Command:                  []string{nativeDocstringCoverageCommand},
			UseHookProject:           true,
			HooksProject:             "/tmp/hooks",
			Verbose:                  true,
			IgnoreInitMethod:         true,
			IgnoreInitModule:         true,
			IgnoreMagic:              true,
			IgnorePrivate:            true,
			IgnoreSemiprivate:        true,
			IgnorePropertyDecorators: true,
			IgnoreNestedFunctions:    true,
			IgnoreNestedClasses:      true,
		},
	)

	wantPrefix := []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		"/tmp/hooks",
		nativeDocstringCoverageCommand,
		"--fail-under",
		"95",
		"--verbose",
	}
	if len(command) < len(wantPrefix) {
		t.Fatalf("command = %#v, want prefix %#v", command, wantPrefix)
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

	if !slicesContains(command, "--ignore-regex") || !slicesContains(command, "pkg") ||
		!slicesContains(command, "pre-commit/hooks") {
		t.Fatalf("command missing expected flags or paths: %#v", command)
	}
}

func TestBuildDocstringCoverageCommandHonorsConfigurableFlags(t *testing.T) {
	t.Parallel()

	command := buildDocstringCoverageCommand(
		docstringCoverageSettings{
			Threshold:                95,
			CheckPaths:               []string{"pkg"},
			Command:                  []string{nativeDocstringCoverageCommand},
			Verbose:                  false,
			IgnoreInitMethod:         true,
			IgnoreInitModule:         true,
			IgnoreMagic:              true,
			IgnorePrivate:            false,
			IgnoreSemiprivate:        false,
			IgnorePropertyDecorators: true,
			IgnoreNestedFunctions:    true,
			IgnoreNestedClasses:      false,
		},
	)

	if slicesContains(command, "--verbose") ||
		slicesContains(command, "--ignore-private") ||
		slicesContains(command, "--ignore-semiprivate") ||
		slicesContains(command, "--ignore-nested-classes") {
		t.Fatalf("command contains disabled flags: %#v", command)
	}
}

func TestCheckDocstringCoverageCommandReportsFailure(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  docstring_coverage:
    enabled: true
    threshold: 95
    command:
      - /bin/sh
      - -lc
      - "printf 'Coverage: 10.0\\n'; exit 1"
    use_hook_project: false
    check_paths:
      - pkg
      - pre-commit/hooks
    exclude_patterns:
      - tests
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatHuman)

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			if got := checkDocstringCoverageCommand(Config{}, nil); got != 1 {
				t.Fatalf("checkDocstringCoverageCommand() = %d, want 1", got)
			}
		})
		if strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
	})

	if !strings.Contains(stdout, "DOCSTRING COVERAGE CHECK FAILED") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}

	if !strings.Contains(stdout, "Threshold: 95%") {
		t.Fatalf("missing threshold in stdout: %q", stdout)
	}

	if !strings.Contains(stdout, "Paths: pkg, pre-commit/hooks") {
		t.Fatalf("missing paths in stdout: %q", stdout)
	}

	if !strings.Contains(stdout, "Coverage: 10.0") {
		t.Fatalf("missing command output in stdout: %q", stdout)
	}
}

func TestCheckDocstringCoverageCommandPassesOnSuccessfulRunner(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  docstring_coverage:
    enabled: true
    threshold: 80
    command:
      - /bin/sh
      - -lc
      - "printf 'Coverage: 100.0\\n'; exit 0"
    use_hook_project: false
    check_paths:
      - pkg
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	stdout := captureStdout(t, func() {
		stderr := captureStderr(t, func() {
			if got := checkDocstringCoverageCommand(Config{}, nil); got != 0 {
				t.Fatalf("checkDocstringCoverageCommand() = %d, want 0", got)
			}
		})
		if strings.TrimSpace(stderr) != "" {
			t.Fatalf("unexpected stderr: %q", stderr)
		}
	})
	if strings.TrimSpace(stdout) != "" {
		t.Fatalf("unexpected stdout: %q", stdout)
	}
}

func TestRunNativeDocstringCoverageReportsMissingSymbols(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	pkgDir := filepath.Join(tempDir, "pkg")

	err := os.MkdirAll(pkgDir, 0o755)
	if err != nil {
		t.Fatalf("mkdir package dir: %v", err)
	}

	mustWriteTestFile(
		t,
		filepath.Join(pkgDir, "sample.py"),
		strings.TrimSpace(`
"""Documented module."""

class Documented:
    """Documented class."""

    def missing(self):
        return 1

def documented():
    """Documented function."""
    return 2
`)+"\n",
	)

	exitCode, stdout, stderr, err := runNativeDocstringCoverage(
		docstringCoverageSettings{
			ConsumerRoot:             tempDir,
			Threshold:                100,
			CheckPaths:               []string{"pkg"},
			IgnoreInitMethod:         true,
			IgnoreInitModule:         true,
			IgnoreMagic:              true,
			IgnorePrivate:            true,
			IgnoreSemiprivate:        true,
			IgnoreNestedFunctions:    true,
			IgnoreNestedClasses:      true,
			IgnorePropertyDecorators: true,
		},
	)
	if err != nil {
		t.Fatalf("runNativeDocstringCoverage() error = %v", err)
	}

	if exitCode != 1 {
		t.Fatalf("runNativeDocstringCoverage() exit = %d, want 1", exitCode)
	}

	if stderr != "" {
		t.Fatalf("unexpected stderr: %q", stderr)
	}

	for _, fragment := range []string{
		"Coverage: 66.7% (2/3 documented)",
		"Missing: pkg/sample.py:function_definition:missing",
	} {
		if !strings.Contains(stdout, fragment) {
			t.Fatalf("stdout missing %q:\n%s", fragment, stdout)
		}
	}
}

func TestFormatDocstringCoverageFailureTOON(t *testing.T) {
	t.Parallel()

	output := formatDocstringCoverageFailure(
		docstringCoverageSettings{
			Threshold:  95,
			CheckPaths: []string{"pkg", "pre-commit/hooks"},
		},
		"Coverage: 10.0\n",
		"",
		hookOutputFormatTOON,
	)
	for _, fragment := range []string{
		"format: toon",
		"tool: docstring_coverage",
		"status: FAIL",
		"threshold: 95",
		"paths[2]{path}:",
		"stdout: Coverage: 10.0",
		"guidance[3]{message}:",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("docstring TOON output missing %q:\n%s", fragment, output)
		}
	}
}

func TestFormatDocstringCoverageFailureJSON(t *testing.T) {
	t.Parallel()

	output := formatDocstringCoverageFailure(
		docstringCoverageSettings{
			Threshold:  95,
			CheckPaths: []string{"pkg"},
		},
		"Coverage: 10.0\n",
		"",
		hookOutputFormatJSON,
	)

	var report docstringCoverageFailureReport

	err := json.Unmarshal([]byte(output), &report)
	if err != nil {
		t.Fatalf("docstring JSON output did not decode: %v\n%s", err, output)
	}

	if report.Format != hookOutputFormatJSON || report.Tool != "docstring_coverage" ||
		report.Status != statusFail || report.Threshold != 95 {
		t.Fatalf("unexpected docstring JSON report: %#v", report)
	}
}

func TestFormatDocstringCoverageFailureSARIF(t *testing.T) {
	t.Parallel()

	output := formatDocstringCoverageFailure(
		docstringCoverageSettings{
			Threshold:  95,
			CheckPaths: []string{"pkg"},
		},
		"Coverage: 50.0% (1/2 documented)\n"+
			"Missing: pkg/sample.py:function_definition:missing\n",
		"",
		hookOutputFormatSARIF,
	)

	for _, fragment := range []string{
		`"version": "2.1.0"`,
		`"uri": "pkg/sample.py"`,
		`"id": "python.docstring_coverage"`,
		`"Public symbol is missing a Google-style docstring."`,
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("docstring SARIF output missing %q:\n%s", fragment, output)
		}
	}
}
