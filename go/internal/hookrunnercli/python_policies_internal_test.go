// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

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
	if !strings.Contains(output, "skipped=1") {
		t.Fatalf("unexpected output: %q", output)
	}
}

func TestCheckPytestGateCommandFailsBeforeRunningPytestForBannedMarkers(t *testing.T) {
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
      - printf 'this command should not run\n'; exit 2
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	filePath := filepath.Join(tempDir, "test_sample.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.Join([]string{
			"import pytest",
			"",
			"@pytest.mark.skip(reason='not now')",
			"def test_blocked():",
			"    assert True",
			"",
		}, "\n"),
	)

	output := captureStderr(t, func() {
		if got := checkPytestGateCommand(Config{}, []string{filePath}); got != 1 {
			t.Fatalf("checkPytestGateCommand() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"BANNED PYTEST MARKERS DETECTED",
		"skip",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("pytest marker output missing %q:\n%s", want, output)
		}
	}

	if strings.Contains(output, "this command should not run") {
		t.Fatalf("pytest command ran despite marker violation:\n%s", output)
	}
}

func TestCheckPytestGateCommandIsSilentOnSuccessByDefault(t *testing.T) {
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
      - printf '2 passed in 0.10s\n'
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
	t.Setenv(hookSuccessOutputEnv, "")

	filePath := filepath.Join(tempDir, "test_sample.py")
	mustWriteTestFile(t, filePath, "def test_ok():\n    assert True\n")

	output := captureStderr(t, func() {
		if got := checkPytestGateCommand(Config{}, []string{filePath}); got != 0 {
			t.Fatalf("checkPytestGateCommand() = %d, want 0", got)
		}
	})
	if output != "" {
		t.Fatalf("expected silent success output, got: %q", output)
	}
}

func TestCheckPytestGateCommandCanEmitVerboseSuccess(t *testing.T) {
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
      - printf '2 passed in 0.10s\n'
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
	t.Setenv(hookSuccessOutputEnv, hookSuccessVerbose)

	filePath := filepath.Join(tempDir, "test_sample.py")
	mustWriteTestFile(t, filePath, "def test_ok():\n    assert True\n")

	output := captureStderr(t, func() {
		if got := checkPytestGateCommand(Config{}, []string{filePath}); got != 0 {
			t.Fatalf("checkPytestGateCommand() = %d, want 0", got)
		}
	})
	if !strings.Contains(output, "Running pytest gate...") ||
		!strings.Contains(output, "Pytest gate passed: 2 tests, 0 skipped.") {
		t.Fatalf("unexpected verbose output: %q", output)
	}
}

func TestDirectImportsCommandFlagsInternalModuleImports(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  source_paths:
    - `+filepath.Join(tempDir, "src")+`
  direct_imports:
    enabled: true
    packages:
      - app
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	initPath := filepath.Join(tempDir, "src", "app", "__init__.py")
	mustWriteTestFile(t, initPath, "\n")

	internalPath := filepath.Join(tempDir, "src", "app", "internal.py")
	mustWriteTestFile(t, internalPath, "VALUE = 1\n")

	target := filepath.Join(tempDir, "src", "worker.py")
	mustWriteTestFile(
		t,
		target,
		"from app.internal import VALUE\nimport app.internal as internal\n",
	)

	output := captureStderr(t, func() {
		if got := checkDirectImportsCommand(Config{}, []string{target}); got != 1 {
			t.Fatalf("checkDirectImportsCommand() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"DIRECT MODULE IMPORT DETECTED",
		"from app.internal import VALUE",
		"import app.internal as internal",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("direct imports output missing %q:\n%s", want, output)
		}
	}
}

func TestSQLCentralizationCommandFlagsSQLOutsideCentralModule(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  sql_centralization:
    enabled: true
    module_name: app.sql
    central_paths:
      - src/app/sql.py
    min_string_length: 8
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	target := filepath.Join(tempDir, "src", "app", "repo.py")
	mustWriteTestFile(
		t,
		target,
		"QUERY = \"SELECT id FROM users WHERE id = ?\"\n",
	)

	central := filepath.Join(tempDir, "src", "app", "sql.py")
	mustWriteTestFile(t, central, "QUERY = \"SELECT id FROM users\"\n")

	output := captureStderr(t, func() {
		files := []string{target, central}
		if got := checkSQLCentralizationCommand(Config{}, files); got != 1 {
			t.Fatalf("checkSQLCentralizationCommand() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"SQL STRINGS FOUND OUTSIDE app.sql",
		"SELECT id FROM users",
		"Move the SQL string to",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("SQL output missing %q:\n%s", want, output)
		}
	}
}

func TestPythonPolicyCommandsDispatchHighValueChecks(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  conditional_imports:
    enabled: true
  optional_returns:
    enabled: true
  security_patterns:
    enabled: true
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	target := filepath.Join(tempDir, "src", "app.py")
	mustWriteTestFile(
		t,
		target,
		strings.Join([]string{
			"try:",
			"    import missing",
			"except ImportError:",
			"    missing = None",
			"def token() -> str | None:",
			"    return os.getenv('API_KEY', 'sk-test-key')",
			"SECRET = 'abcdef1234567890abcdef1234567890'",
			"",
		}, "\n"),
	)

	for name, command := range map[string]func(Config, []string) int{
		"conditional-imports": checkConditionalImportsCommand,
		"optional-returns":    checkOptionalReturnsCommand,
		"security-patterns":   checkSecurityPatternsCommand,
	} {
		output := captureStderr(t, func() {
			if got := command(Config{}, []string{target}); got != 1 {
				t.Fatalf("%s command = %d, want 1", name, got)
			}
		})
		if output == "" {
			t.Fatalf("%s command produced no output", name)
		}
	}
}

func TestFindDirectImportViolations(t *testing.T) {
	t.Parallel()

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

func TestIsDirectImportExemptMatchesConfiguredPath(t *testing.T) {
	t.Parallel()

	settings := directImportsSettings{
		ExemptPaths: []string{"lib/python/tests"},
	}

	if !isDirectImportExempt("repo/lib/python/tests/test_module.py", settings) {
		t.Fatal("expected test path to be exempt")
	}

	if isDirectImportExempt("repo/lib/python/project/module.py", settings) {
		t.Fatal("source path should not be exempt")
	}
}

func TestFindUtilityViolations(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
"""SELECT id FROM docs should not count."""

def build():
    raise ValueError(reason="SELECT id FROM docs")
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

func TestFindSQLViolationsIgnoresConfiguredTestPaths(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "tests", "test_queries.py")
	mustWriteTestFile(
		t,
		filePath,
		"query = \"SELECT id FROM users WHERE id = $1\"\n",
	)

	violations, err := findSQLViolations(
		filePath,
		sqlCentralizationSettings{
			Enabled:         true,
			ModuleName:      "project.sql",
			ExemptPaths:     []string{"tests"},
			MinStringLength: 15,
		},
	)
	if err != nil {
		t.Fatalf("findSQLViolations() returned error: %v", err)
	}

	if len(violations) != 0 {
		t.Fatalf("len(violations) = %d, want 0 (%#v)", len(violations), violations)
	}
}

func TestFindStructuredLoggingViolations(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "tests", "test_security.py")
	mustWriteTestFile(
		t,
		filePath,
		strings.TrimSpace(`
import os

def build_query(table: str) -> str:
    query = f"SELECT id FROM {table}"
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

func TestPythonPolicyCommandsReportViolations(t *testing.T) {
	tempDir := t.TempDir()
	bundleRoot := writeTestBundleRoot(t, tempDir)

	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(consumerRootEnv, tempDir)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)

	writePythonPolicyCommandConfig(t, tempDir)

	mustWriteTestFile(t, filepath.Join(tempDir, "project", "__init__.py"), "")
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "project", "internal.py"),
		"def helper():\n    return 1\n",
	)
	filePath := filepath.Join(tempDir, "consumer.py")
	writePythonPolicyViolationFixture(t, filePath)

	for name, run := range pythonPolicyViolationCommands() {
		captureStderr(t, func() {
			if got := run(Config{}, []string{filePath}); got != 1 {
				t.Fatalf("%s command = %d, want 1", name, got)
			}
		})
	}
}

func writePythonPolicyCommandConfig(t *testing.T, tempDir string) {
	t.Helper()

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, strings.TrimSpace(`
python:
  source_paths:
    - project
  direct_imports:
    enabled: true
    packages:
      - project
  util_centralization:
    enabled: true
    banned_modules:
      - module: requests
        alternative: Use project.http
  sql_centralization:
    enabled: true
    module_name: project.sql
    min_string_length: 15
  structured_logging:
    enabled: true
  conditional_imports:
    enabled: true
  type_checking_imports:
    enabled: true
  catch_and_silence:
    enabled: true
  optional_returns:
    enabled: true
  security_patterns:
    enabled: true
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
}

func writePythonPolicyViolationFixture(t *testing.T, filePath string) {
	t.Helper()

	mustWriteTestFile(t, filePath, strings.TrimSpace(`
import os
import requests
from typing import TYPE_CHECKING
from project.internal import helper

if TYPE_CHECKING:
    import project.types

try:
    import fancy_dep
except ImportError:
    HAS_FANCY_DEP = False

try:
    run()
except ValueError:
    pass

def run(logger, table: str, item: bytes | None) -> str | None:
    logger.info("bare message")
    query = f"SELECT id FROM {table}"
    secret = os.getenv("API_KEY", "sk-test-key")
    return query + secret
`)+"\n",
	)
}

func pythonPolicyViolationCommands() map[string]func(Config, []string) int {
	return map[string]func(Config, []string) int{
		"catch":         checkCatchAndSilenceCommand,
		"conditional":   checkConditionalImportsCommand,
		"logging":       checkStructuredLoggingCommand,
		"optional":      checkOptionalReturnsCommand,
		"security":      checkSecurityPatternsCommand,
		"type-checking": checkTypeCheckingImportsCommand,
		"util":          checkUtilCentralizationCommand,
	}
}
