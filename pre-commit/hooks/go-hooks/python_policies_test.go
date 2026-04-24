// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCheckFileDocstringsCommand(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  file_docstrings:
    enabled: true
    min_sentences: 3
    exempt_filenames:
      - __init__.py
      - conftest.py
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	filePath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(
		t,
		filePath,
		"\"\"\"One sentence. Two sentences.\"\"\"\nvalue = 1\n",
	)

	output := captureStderr(t, func() {
		if got := checkFileDocstringsCommand(Config{}, []string{filePath}); got != 1 {
			t.Fatalf("checkFileDocstringsCommand() = %d, want 1", got)
		}
	})
	if !strings.Contains(output, "module docstring has 2 sentence(s), need 3") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestCheckPytestGateCommandFailsOnSkippedTests(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  pytest_gate:
    enabled: true
    banned_markers:
      - skip
      - skipif
    test_command:
      - /bin/sh
      - -lc
      - printf '2 passed, 1 skipped in 0.10s\n'
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	filePath := filepath.Join(tempDir, "test_sample.py")
	mustWriteTestFile(t, filePath, "def test_ok():\n    assert True\n")

	output := captureStderr(t, func() {
		if got := checkPytestGateCommand(Config{}, []string{filePath}); got != 1 {
			t.Fatalf("checkPytestGateCommand() = %d, want 1", got)
		}
	})
	if !strings.Contains(output, "Skipped tests: 1") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestFindDirectImportViolations(t *testing.T) {
	tempDir := t.TempDir()
	mustWriteTestFile(t, filepath.Join(tempDir, "project", "__init__.py"), "")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "project", "internal.py"),
		"def helper():\n    return 1\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "scripts", "consumer.py"),
		"from project.internal import helper\n",
	)

	violations, err := findDirectImportViolations(
		filepath.Join(tempDir, "scripts", "consumer.py"),
		directImportsSettings{
			Enabled:      true,
			Packages:     []string{"project"},
			SourcePaths:  []string{"project"},
			ConsumerRoot: tempDir,
		},
	)
	if err != nil {
		t.Fatalf("findDirectImportViolations() returned error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 (%#v)", len(violations), violations)
	}
	if got, want := violations[0].Suggestion, "from project import helper"; got != want {
		t.Fatalf("Suggestion = %q, want %q", got, want)
	}
}

func TestFindUtilityViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(t, filePath, "import requests\nfrom google import genai\n")

	violations, err := findUtilityViolations(
		filePath,
		utilCentralizationSettings{
			Enabled: true,
			BannedModules: []bannedUtilityModule{
				{Module: "requests", Alternative: "Use project.http"},
				{Module: "google.genai", Alternative: "Use project.ai"},
			},
		},
	)
	if err != nil {
		t.Fatalf("findUtilityViolations() returned error: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("len(violations) = %d, want 2 (%#v)", len(violations), violations)
	}
	if !reflect.DeepEqual(
		[]string{violations[0].Suggestion, violations[1].Suggestion},
		[]string{"Use project.http", "Use project.ai"},
	) {
		t.Fatalf("suggestions = %#v", violations)
	}
}

func TestFindSQLViolationsIgnoresDocstringsAndKeywordContext(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
"""SELECT * FROM docs should not count."""

def build():
    raise ValueError(reason="SELECT * FROM docs")
    query = "SELECT id FROM users WHERE id = $1"
    return query
`)+"\n",
	)

	violations, err := findSQLViolations(
		filePath,
		sqlCentralizationSettings{
			Enabled:              true,
			ModuleName:           "project.sql",
			MinStringLength:      15,
			ErrorContextKeywords: []string{"reason", "message"},
		},
	)
	if err != nil {
		t.Fatalf("findSQLViolations() returned error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 (%#v)", len(violations), violations)
	}
	if violations[0].Pattern != "SELECT...FROM" {
		t.Fatalf("Pattern = %q, want %q", violations[0].Pattern, "SELECT...FROM")
	}
}

func TestFindStructuredLoggingViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "logging.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
def run(logger, exc):
    logger.info("bare message")
    logger.info("good.event", request_id=123)
    logger.info("percent %s", exc)
    logger.exception("boom")
`)+"\n",
	)

	violations, err := findStructuredLoggingViolations(
		filePath,
		structuredLoggingSettings{
			Enabled:      true,
			Methods:      []string{"debug", "info", "warning", "error", "critical"},
			LoggerNames:  []string{"logger"},
			ExemptKwargs: []string{"exc_info", "stack_info", "stacklevel"},
		},
	)
	if err != nil {
		t.Fatalf("findStructuredLoggingViolations() returned error: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("len(violations) = %d, want 2 (%#v)", len(violations), violations)
	}
	if violations[0].Method != "info" || violations[1].Method != "info" {
		t.Fatalf("unexpected methods: %#v", violations)
	}
}

func TestFindConditionalImportViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "imports.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
try:
    import fancy_dep
except ImportError:
    HAS_FANCY_DEP = False
`)+"\n",
	)

	violations, err := findConditionalImportViolations(
		filePath,
		conditionalImportsSettings{
			Enabled:          true,
			ExceptionNames:   []string{"ImportError", "ModuleNotFoundError"},
			CapabilityPrefix: "HAS_",
		},
	)
	if err != nil {
		t.Fatalf("findConditionalImportViolations() returned error: %v", err)
	}
	if len(violations) != 2 {
		t.Fatalf("len(violations) = %d, want 2 (%#v)", len(violations), violations)
	}
}

func TestFindTypeCheckingImportViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "typing_example.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
from __future__ import annotations
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import project.types
`)+"\n",
	)

	violations, err := findTypeCheckingImportViolations(
		filePath,
		typeCheckingImportsSettings{
			Enabled:           true,
			TypeCheckingNames: []string{"TYPE_CHECKING"},
			FutureImportName:  "annotations",
		},
	)
	if err != nil {
		t.Fatalf("findTypeCheckingImportViolations() returned error: %v", err)
	}
	if len(violations) != 3 {
		t.Fatalf("len(violations) = %d, want 3 (%#v)", len(violations), violations)
	}
}

func TestFindCatchSilenceViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "exceptions.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
try:
    run()
except ValueError:
    "doc"
    pass
`)+"\n",
	)

	violations, err := findCatchSilenceViolations(
		filePath,
		catchSilenceSettings{Enabled: true},
	)
	if err != nil {
		t.Fatalf("findCatchSilenceViolations() returned error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 (%#v)", len(violations), violations)
	}
	if violations[0].HandlerBody != "pass" {
		t.Fatalf("HandlerBody = %q, want %q", violations[0].HandlerBody, "pass")
	}
}

func TestFindOptionalTypeViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "optional_types.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
x: int | None = 1

class Thing:
    value: str | None

async def compute(item: bytes | None) -> str | None:
    return "ok"

def __exit__(exc_type: type[BaseException] | None) -> None:
    return None
`)+"\n",
	)

	violations, err := findOptionalTypeViolations(
		filePath,
		optionalReturnsSettings{
			Enabled:           true,
			ExemptMethodNames: []string{"__exit__", "__aexit__"},
		},
	)
	if err != nil {
		t.Fatalf("findOptionalTypeViolations() returned error: %v", err)
	}
	if len(violations) != 4 {
		t.Fatalf("len(violations) = %d, want 4 (%#v)", len(violations), violations)
	}
}

func TestFindSecurityViolations(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "tests", "test_security.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
import os

def build_query(table: str) -> str:
    query = f"SELECT * FROM {table}"
    secret = os.getenv("API_KEY", "sk-test-key")
    os.environ["API_KEY"] = "override"
    return query + secret
`)+"\n",
	)

	violations, err := findSecurityViolations(
		filePath,
		securityPatternsSettings{
			Enabled: true,
			SQLKeywords: []string{
				"SELECT",
				"INSERT",
				"UPDATE",
				"DELETE",
				"DROP",
				"CREATE",
				"ALTER",
				"TRUNCATE",
				"EXECUTE",
				"EXEC",
			},
			SecretPatterns: []string{
				"sk-",
				"pk-",
				"api_",
				"key_",
				"token_",
				"secret_",
				"password",
				"passwd",
				"credential",
			},
			TestFileMarkers: []string{
				"tests",
				"conftest",
				"test_",
				"_test.py",
			},
			MinGetenvArgsWithDefault: 2,
		},
	)
	if err != nil {
		t.Fatalf("findSecurityViolations() returned error: %v", err)
	}
	if len(violations) != 3 {
		t.Fatalf("len(violations) = %d, want 3 (%#v)", len(violations), violations)
	}
}
