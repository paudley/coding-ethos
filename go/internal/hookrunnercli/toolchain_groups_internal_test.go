// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:lll // Uses process-global fixtures.
package hookrunnercli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

func TestParseGofmtCheckFindings(t *testing.T) {
	t.Parallel()

	findings := parseGofmtCheckFindings("pkg/app.go\ncmd/main.go\n")
	if len(findings) != 2 {
		t.Fatalf("parseGofmtCheckFindings() = %#v, want two findings", findings)
	}

	got := findings[0]
	if got.Tool != "gofmt-check" ||
		got.File != toolCatalogGoFile ||
		got.Severity != testSeverityError ||
		got.Message != "Go file is not gofmt-formatted." {
		t.Fatalf("unexpected finding: %#v", got)
	}
}

func TestParsePythonQualityFindings(t *testing.T) {
	t.Parallel()

	assertComplexityFinding(t)
	assertMaintainabilityFinding(t)
	assertMaintainabilityTimeoutFinding(t)
	assertVultureFinding(t)
}

func TestPythonQualityCommandsRunExternalToolsAndReportFindings(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	mustWriteTestFile(t, "pkg/app.py", "def helper():\n    return 1\n")

	fakeBin := writePythonQualityToolFixtures(t, tempDir)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var (
		complexityExit      int
		maintainabilityExit int
		vultureExit         int
	)

	stdout := captureStdout(t, func() {
		complexityExit = runPythonComplexity(Config{}, []string{"pkg/app.py"})
		maintainabilityExit = runPythonMaintainability(Config{}, []string{"pkg/app.py"})
		vultureExit = runPythonVulture(Config{}, []string{"pkg/app.py"})
	})

	assertPythonQualityExits(t, complexityExit, maintainabilityExit, vultureExit, stdout)
	assertPythonQualityOutput(t, stdout)
}

func writePythonQualityToolFixtures(t *testing.T, tempDir string) string {
	t.Helper()

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", ".venv", "bin", "radon"),
		`#!/usr/bin/env sh
case "$*" in
  *"cc -j -e"*)
    printf '{"pkg/app.py":[{"type":"function","rank":"C","lineno":3,"name":"complex","complexity":19}]}'
    exit 0
    ;;
  *"mi . -j -e"*)
    printf '{"pkg/app.py":{"mi":42.5,"rank":"C"}}'
    exit 0
    ;;
esac
printf 'unexpected radon invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", ".venv", "bin", "vulture"),
		`#!/usr/bin/env sh
printf "pkg/app.py:17: unused function 'helper' (90%% confidence)\n"
exit 1
`,
	)
	mustWriteExecutable(t, filepath.Join(fakeBin, "uv"), `#!/usr/bin/env sh
printf 'unexpected uv invocation: %s\n' "$*" >&2
exit 2
`)

	return fakeBin
}

func assertPythonQualityExits(
	t *testing.T,
	complexityExit int,
	maintainabilityExit int,
	vultureExit int,
	stdout string,
) {
	t.Helper()

	if complexityExit != managedcapture.BlockedExitCode {
		t.Fatalf(
			"runPythonComplexity() = %d, want %d:\n%s",
			complexityExit,
			managedcapture.BlockedExitCode,
			stdout,
		)
	}

	if maintainabilityExit != 0 {
		t.Fatalf("runPythonMaintainability() = %d, want 0:\n%s", maintainabilityExit, stdout)
	}

	if vultureExit != 1 {
		t.Fatalf("runPythonVulture() = %d, want 1:\n%s", vultureExit, stdout)
	}
}

func assertPythonQualityOutput(t *testing.T, stdout string) {
	t.Helper()

	for _, want := range []string{
		"tool: python-complexity",
		"complex",
		"tool: python-vulture",
		"unused function",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("quality output missing %q:\n%s", want, stdout)
		}
	}
}

func TestGoToolchainCommandsRunConfiguredWorktree(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, "go:\n  worktree: go\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
	managedLintLog := filepath.Join(tempDir, "managed-lint.log")
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin", "golangci-lint"),
		`#!/usr/bin/env sh
printf '%s\n' "$*" >> `+shellQuoteForTest(managedLintLog)+`
exit 0
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "go"),
		`#!/usr/bin/env sh
case "$1 $2" in
  "vet ./..."|"test -json")
    exit 0
    ;;
esac
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "gofmt"),
		"#!/usr/bin/env sh\nprintf 'go/main.go\\n'\nexit 0\n",
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := runGoVet(Config{}, []string{"go/main.go"}); got != 0 {
		t.Fatalf("runGoVet() = %d, want 0", got)
	}

	if got := runGoTests(Config{}, []string{"go/main.go"}); got != 0 {
		t.Fatalf("runGoTests() = %d, want 0", got)
	}

	if got := runGolangciLint(Config{}, []string{"go/main.go"}); got != 0 {
		t.Fatalf("runGolangciLint() = %d, want 0", got)
	}

	assertManagedGolangciInvocation(t, managedLintLog)

	stdout := captureStdout(t, func() {
		if got := runGoFormatCheck(Config{}, []string{"go/main.go"}); got != 1 {
			t.Fatalf("runGoFormatCheck() = %d, want 1", got)
		}
	})
	if !strings.Contains(stdout, "GOFMT CHECK FAILED") ||
		!strings.Contains(stdout, "go/main.go") {
		t.Fatalf("gofmt output missing expected finding:\n%s", stdout)
	}
}

func assertManagedGolangciInvocation(t *testing.T, managedLintLog string) {
	t.Helper()

	managedLintArgs, err := os.ReadFile(managedLintLog)
	if err != nil {
		t.Fatalf("read managed lint log: %v", err)
	}

	for _, want := range []string{
		"run --output.json.path=stdout --output.text.path=stderr --config " + filepath.Join(
			filepath.Dir(managedLintLog),
			".golangci.yml",
		),
		"./...",
	} {
		if !strings.Contains(string(managedLintArgs), want) {
			t.Fatalf(
				"managed golangci invocation missing %q:\n%s",
				want,
				string(managedLintArgs),
			)
		}
	}
}

func TestPathWithoutHookGitShimsRemovesRuntimeAndLocalShims(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	realGitDir := filepath.Join(tempDir, "usr", "bin")
	runtimeDir := filepath.Join(tempDir, ".git", "coding-ethos-hooks", "bin")
	localBinDir := filepath.Join(tempDir, "coding-ethos", "bin")
	normalDir := filepath.Join(tempDir, "tools", "bin")

	for _, dir := range []string{realGitDir, runtimeDir, localBinDir, normalDir} {
		err := os.MkdirAll(dir, 0o700)
		if err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	mustWriteExecutable(
		t,
		filepath.Join(localBinDir, "git"),
		"#!/usr/bin/env sh\nexport CODING_ETHOS_REAL_GIT=/usr/bin/git\nexec /repo/bin/coding-ethos-run policy-git \"$@\"\n",
	)

	path := strings.Join(
		[]string{runtimeDir, localBinDir, normalDir, realGitDir},
		string(os.PathListSeparator),
	)

	got := pathWithoutHookGitShims(path, filepath.Join(realGitDir, "git"))

	want := strings.Join([]string{realGitDir, normalDir}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("pathWithoutHookGitShims() = %q, want %q", got, want)
	}
}

func TestCatalogLintCommandsRunExternalToolsAndParseFindings(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	writeCatalogLintFixtures(t)
	writeCatalogLintTools(t, tempDir)

	stdout := captureStdout(t, func() {
		assertCatalogCommand(t, "hadolint", runHadolint, "Dockerfile")
		assertCatalogCommand(
			t,
			"actionlint",
			runActionlint,
			".github/workflows/ci.yml",
		)
		assertCatalogCommand(t, "bandit", runBandit, "pkg/app.py")
		assertCatalogCommand(t, "sqlfluff", runSQLFluff, "queries/app.sql")
		assertCatalogCommand(t, "tombi", runTombi, "config.toml")
		assertCatalogCommand(t, "dotenv-linter", runDotenvLinter, ".env.example")
	})

	for _, want := range []string{
		"tool: hadolint",
		"tool: actionlint",
		"tool: bandit",
		"tool: sqlfluff",
		"tool: tombi",
		"tool: dotenv-linter",
		"Dockerfile",
		".github/workflows/ci.yml",
		"pkg/app.py",
		"queries/app.sql",
		"config.toml",
		".env.example",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("catalog lint output missing %q:\n%s", want, stdout)
		}
	}
}

func writeManagedToolchainBundle(t *testing.T, root string) string {
	t.Helper()

	ethosRoot := filepath.Join(root, "code-ethos")
	bundleRoot := filepath.Join(ethosRoot, "pre-commit")
	mustWriteTestFile(t, filepath.Join(ethosRoot, "config.yaml"), "version: 1\n")
	mustWriteTestFile(
		t,
		filepath.Join(ethosRoot, "build", "policy", "policy-bundle.json"),
		`{"version":1,"policies":{},"skills":{},"evidence_maps":[]}`,
	)
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "managed-toolchain.tsv"),
		"",
	)
	mustWriteTestFile(t, filepath.Join(bundleRoot, "hooks", "pyproject.toml"), "")

	_, err := toolconfigs.Sync(ethosRoot, root, "")
	if err != nil {
		t.Fatalf("sync generated tool configs: %v", err)
	}

	return bundleRoot
}

func writeCatalogLintFixtures(t *testing.T) {
	t.Helper()

	mustWriteTestFile(t, "Dockerfile", "FROM ubuntu\n")
	mustWriteTestFile(t, ".github/workflows/ci.yml", "name: ci\n")
	mustWriteTestFile(t, "pkg/app.py", "print('ok')\n")
	mustWriteTestFile(t, "queries/app.sql", "select 1\n")
	mustWriteTestFile(t, "config.toml", "bad = true\n")
	mustWriteTestFile(t, ".env.example", "lower=value\n")
}

func writeCatalogLintTools(t *testing.T, tempDir string) {
	t.Helper()

	fakeBin := filepath.Join(tempDir, "bin")
	goBin := filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin")
	githubBin := filepath.Join(
		tempDir,
		"code-ethos",
		"build",
		"toolchain",
		"github-bin",
	)
	mustWriteExecutable(
		t,
		filepath.Join(githubBin, "hadolint"),
		`#!/usr/bin/env sh
printf '[{"line":3,"column":1,"file":"Dockerfile","level":"warning","code":"DL3008","message":"Pin versions in apt get install."}]\n'
exit 1
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(goBin, "actionlint"),
		`#!/usr/bin/env sh
printf '%s\n' '{"filepath":".github/workflows/ci.yml","line":12,"column":5,"kind":"syntax-check","message":"property \"run\" is not defined"}'
exit 1
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(githubBin, "dotenv-linter"),
		"#!/usr/bin/env sh\nprintf '.env.example:3 LowercaseKey: The key should be uppercase\\n'\nexit 1\n",
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "uv"),
		`#!/usr/bin/env sh
case "$*" in
  *" bandit "*)
    printf '{"results":[{"filename":"pkg/app.py","line_number":10,"issue_severity":"HIGH","test_id":"B602","issue_text":"subprocess call with shell=True"}]}'
    exit 1
    ;;
  *" sqlfluff "*)
    printf '[{"filepath":"queries/app.sql","violations":[{"line_no":2,"line_pos":7,"code":"LT01","description":"Expected single whitespace."}]}]'
    exit 1
    ;;
  *" tombi "*)
    printf 'Error: invalid key\n    at config.toml:2:4\n'
    exit 1
    ;;
esac
printf 'unexpected uv invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "gofmt"),
		`#!/usr/bin/env sh
exit 1
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func assertCatalogCommand(
	t *testing.T,
	name string,
	run func(Config, []string) int,
	file string,
) {
	t.Helper()

	if got := run(Config{}, []string{file}); got != 1 {
		t.Fatalf("%s command = %d, want 1", name, got)
	}
}

func assertComplexityFinding(t *testing.T) {
	t.Helper()

	complexity := parseComplexityFindings(
		"  pkg/app.py:42 build_payload (complexity: 19)",
	)
	if len(complexity) != 1 || complexity[0].Code != "cyclomatic-complexity" ||
		complexity[0].Line != 42 {
		t.Fatalf("parseComplexityFindings() = %#v", complexity)
	}

	radon := parseRadonComplexityFindings(
		`{"pkg/app.py":[{"type":"function","rank":"C","lineno":42,"name":"build_payload","complexity":19}]}`,
		15,
	)
	if len(radon) != 1 ||
		radon[0].Code != "cyclomatic-complexity" ||
		radon[0].Message != "build_payload" ||
		radon[0].Detail != "complexity: 19" {
		t.Fatalf("parseRadonComplexityFindings() = %#v", radon)
	}
}

func assertMaintainabilityFinding(t *testing.T) {
	t.Helper()

	maintainability := parseMaintainabilityFindings("  pkg/app.py (MI: 42.50)")
	if len(maintainability) != 1 || maintainability[0].Code != "maintainability-index" {
		t.Fatalf("parseMaintainabilityFindings() = %#v", maintainability)
	}

	radon := parseRadonMaintainabilityFindings(
		`{"pkg/app.py":{"mi":42.5,"rank":"C"}}`,
		50,
	)
	if len(radon) != 1 ||
		radon[0].Code != "maintainability-index" ||
		radon[0].Detail != "MI: 42.50" {
		t.Fatalf("parseRadonMaintainabilityFindings() = %#v", radon)
	}
}

func assertMaintainabilityTimeoutFinding(t *testing.T) {
	t.Helper()

	timeout := parseMaintainabilityFindings("Error: radon timed out after 60s")
	if len(timeout) != 1 ||
		timeout[0].Code != timeoutCode ||
		timeout[0].Message != "radon timed out after 60s" ||
		timeout[0].Advice == "" {
		t.Fatalf("parseMaintainabilityFindings(timeout) = %#v", timeout)
	}
}

func assertVultureFinding(t *testing.T) {
	t.Helper()

	vulture := parseVultureFindings(
		"pkg/app.py:17: unused function 'helper' (60% confidence)",
	)
	if len(vulture) != 1 || vulture[0].Code != "unused-code" || vulture[0].Line != 17 {
		t.Fatalf("parseVultureFindings() = %#v", vulture)
	}
}
