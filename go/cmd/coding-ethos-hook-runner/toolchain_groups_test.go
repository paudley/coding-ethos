// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:lll // Uses process-global fixtures.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseHadolintFindings(t *testing.T) {
	t.Parallel()

	findings := parseCatalogFindings("hadolint", toolOutputFixture(t, "hadolint.json"))
	if len(findings) != 1 {
		t.Fatalf("parseHadolintFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.File != "Dockerfile" || got.Line != 3 || got.Code != "DL3008" ||
		got.Severity != "warning" || got.Message != "Pin versions in apt get install." {
		t.Fatalf("unexpected finding: %#v", got)
	}

	textFindings := parseCatalogFindings(
		"hadolint",
		"Dockerfile:3 DL3008 warning: Pin versions in apt get install.",
	)
	if len(textFindings) != 1 {
		t.Fatalf("parseHadolintFindings(text) = %#v, want one finding", textFindings)
	}
}

func TestParseActionlintFindings(t *testing.T) {
	t.Parallel()

	findings := parseCatalogFindings(
		"actionlint",
		toolOutputFixture(t, "actionlint.jsonl"),
	)
	if len(findings) != 1 {
		t.Fatalf("parseActionlintFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.File != ".github/workflows/ci.yml" || got.Line != 12 || got.Column != 5 ||
		got.Code != "syntax-check" || got.Message != "property \"run\" is not defined" {
		t.Fatalf("unexpected finding: %#v", got)
	}

	textFindings := parseCatalogFindings(
		"actionlint",
		".github/workflows/ci.yml:12:5: property \"run\" is not defined [syntax-check]",
	)
	if len(textFindings) != 1 {
		t.Fatalf("parseActionlintFindings(text) = %#v, want one finding", textFindings)
	}
}

func TestParseGolangciFindings(t *testing.T) {
	t.Parallel()

	findings := parseCatalogFindings(
		"golangci-lint",
		"level=warning msg=\"runner warning\"\n"+toolOutputFixture(t, "golangci.json"),
	)
	if len(findings) != 1 {
		t.Fatalf("parseGolangciFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.File != "pkg/app.go" || got.Line != 8 || got.Column != 2 ||
		got.Code != "ineffassign" || got.Message != "ineffectual assignment to err" {
		t.Fatalf("unexpected finding: %#v", got)
	}

	textFindings := parseCatalogFindings(
		"golangci-lint",
		"pkg/app.go:8:2: ineffectual assignment to err (ineffassign)",
	)
	if len(textFindings) != 1 {
		t.Fatalf("parseGolangciFindings(text) = %#v, want one finding", textFindings)
	}
}

func TestParseNewParityToolFindings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		output   string
		wantTool string
		wantFile string
		wantCode string
	}{
		{
			name:     "bandit",
			output:   `{"results":[{"filename":"pkg/app.py","line_number":10,"issue_severity":"HIGH","test_id":"B602","issue_text":"subprocess call with shell=True"}]}`,
			wantTool: "bandit",
			wantFile: "pkg/app.py",
			wantCode: "B602",
		},
		{
			name:     "sqlfluff",
			output:   `[{"filepath":"queries/app.sql","violations":[{"line_no":2,"line_pos":7,"code":"LT01","description":"Expected single whitespace."}]}]`,
			wantTool: "sqlfluff",
			wantFile: "queries/app.sql",
			wantCode: "LT01",
		},
		{
			name:     "tombi",
			output:   "Error: invalid key\n    at config.toml:2:4\n",
			wantTool: "tombi",
			wantFile: "config.toml",
		},
		{
			name:     "dotenv-linter",
			output:   ".env.example:3 LowercaseKey: The key should be uppercase",
			wantTool: "dotenv-linter",
			wantFile: ".env.example",
			wantCode: "LowercaseKey",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			findings := parseCatalogFindings(test.name, test.output)
			if len(findings) != 1 {
				t.Fatalf("%s findings = %#v, want one", test.name, findings)
			}

			got := findings[0]
			if got.Tool != test.wantTool || got.File != test.wantFile ||
				got.Code != test.wantCode {
				t.Fatalf("unexpected %s finding: %#v", test.name, got)
			}
		})
	}
}

func TestParseGofmtCheckFindings(t *testing.T) {
	t.Parallel()

	findings := parseGofmtCheckFindings("pkg/app.go\ncmd/main.go\n")
	if len(findings) != 2 {
		t.Fatalf("parseGofmtCheckFindings() = %#v, want two findings", findings)
	}

	got := findings[0]
	if got.Tool != "gofmt-check" ||
		got.File != toolCatalogGoFile ||
		got.Severity != "error" ||
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

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "uv"),
		`#!/usr/bin/env sh
case "$*" in
  *" radon cc "*)
    printf '{"pkg/app.py":[{"type":"function","rank":"C","lineno":3,"name":"complex","complexity":19}]}'
    exit 0
    ;;
  *" radon mi "*)
    printf '{"pkg/app.py":{"mi":42.5,"rank":"C"}}'
    exit 0
    ;;
  *" vulture "*)
    printf "pkg/app.py:17: unused function 'helper' (90%% confidence)\n"
    exit 1
    ;;
esac
printf 'unexpected uv invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout := captureStdout(t, func() {
		if got := runPythonComplexity(Config{}, []string{"pkg/app.py"}); got != 1 {
			t.Fatalf("runPythonComplexity() = %d, want 1", got)
		}

		if got := runPythonMaintainability(Config{}, []string{"pkg/app.py"}); got != 0 {
			t.Fatalf("runPythonMaintainability() = %d, want 0", got)
		}

		if got := runPythonVulture(Config{}, []string{"pkg/app.py"}); got != 1 {
			t.Fatalf("runPythonVulture() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"COMPLEXITY FAILED",
		"complex",
		"VULTURE FAILED",
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

	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
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
		"HADOLINT FAILED",
		"ACTIONLINT FAILED",
		"BANDIT FAILED",
		"SQLFLUFF FAILED",
		"TOMBI FAILED",
		"DOTENV-LINTER FAILED",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("catalog lint output missing %q:\n%s", want, stdout)
		}
	}
}

func writeManagedToolchainBundle(t *testing.T, root string) string {
	t.Helper()

	bundleRoot := filepath.Join(root, "code-ethos", "pre-commit")
	mustWriteTestFile(t, filepath.Join(root, "code-ethos", "config.yaml"), "{}\n")
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "managed-toolchain.tsv"),
		"",
	)
	mustWriteTestFile(t, filepath.Join(bundleRoot, "hooks", "pyproject.toml"), "")

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
printf '{"line":3,"column":1,"file":"Dockerfile","level":"warning","code":"DL3008","message":"Pin versions in apt get install."}\n'
exit 1
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(goBin, "actionlint"),
		`#!/usr/bin/env sh
printf '{"filepath":".github/workflows/ci.yml","line":12,"column":5,"kind":"syntax-check","message":"property \"run\" is not defined"}\n'
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

func toolOutputFixture(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join("testdata", "tool_outputs", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}

	return string(content)
}
