// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

func TestStagedFilesListsGitIndexEntries(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")

	path := filepath.Join(repo, "pkg", "app.py")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte("print('x')\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGit(t, repo, "add", "pkg/app.py")

	files, err := stagedFiles(repo)
	if err != nil {
		t.Fatalf("staged files: %v", err)
	}
	if len(files) != 1 || files[0] != "pkg/app.py" {
		t.Fatalf("staged files = %#v, want pkg/app.py", files)
	}
}

func TestFilesFromInputsCombinesFlagAndFileLists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(
		path,
		[]byte("pkg/app.py\n\npkg/other.py\r\n"),
		0o600,
	); err != nil {
		t.Fatalf("write file list: %v", err)
	}

	files, err := filesFromInputs("README.md, docs/usage.md", path)
	if err != nil {
		t.Fatalf("files from inputs: %v", err)
	}

	want := []string{
		"README.md",
		"docs/usage.md",
		"pkg/app.py",
		"pkg/other.py",
	}
	if len(files) != len(want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
	for index := range want {
		if files[index] != want[index] {
			t.Fatalf("files = %#v, want %#v", files, want)
		}
	}
}

func TestShouldReturnEmptyExplicitFileScope(t *testing.T) {
	t.Parallel()

	if !shouldReturnEmptyExplicitFileScope("files", nil, "", "files.txt") {
		t.Fatal("empty --files-from selection should return an empty files result")
	}
	if !shouldReturnEmptyExplicitFileScope("files", nil, "   ", "files.txt") {
		t.Fatal("--files-from makes an empty selection explicit")
	}
	if shouldReturnEmptyExplicitFileScope("files", []string{"pkg/app.py"}, "", "files.txt") {
		t.Fatal("non-empty files must run policy evaluation")
	}
	if shouldReturnEmptyExplicitFileScope("files", nil, "", "") {
		t.Fatal("implicit files scope must preserve policy explanation behavior")
	}
	if shouldReturnEmptyExplicitFileScope("staged", nil, "", "files.txt") {
		t.Fatal("staged scope resolves files from git index")
	}
}

func TestLintOutputFormatSelectionAndConflicts(t *testing.T) {
	t.Parallel()

	format, err := lintOutputFormat(true, false)
	if err != nil || format != hookoutput.FormatJSON {
		t.Fatalf("json format = %q, %v", format, err)
	}
	format, err = lintOutputFormat(false, true)
	if err != nil || format != hookoutput.FormatSARIF {
		t.Fatalf("sarif format = %q, %v", format, err)
	}
	if _, err := lintOutputFormat(true, true); err != errOutputFormatConflict {
		t.Fatalf("conflict error = %v", err)
	}
	if selectedLintOutputFormat(hookoutput.FormatJSON) != hookoutput.FormatJSON {
		t.Fatal("explicit format should be preserved")
	}
}

func TestParseArgvHandlesNULAndSpaceSeparatedForms(t *testing.T) {
	t.Parallel()

	nul := parseArgv("git\x00commit\x00-m\x00msg")
	if strings.Join(nul, "|") != "git|commit|-m|msg" {
		t.Fatalf("nul argv = %#v", nul)
	}
	spaced := parseArgv("git commit -m msg")
	if strings.Join(spaced, "|") != "git|commit|-m|msg" {
		t.Fatalf("space argv = %#v", spaced)
	}
	if got := parseArgv(""); len(got) != 0 {
		t.Fatalf("empty argv = %#v", got)
	}
}

func TestReadBundleAndCapturePolicyContext(t *testing.T) {
	t.Parallel()

	bundlePath := writeLintTestBundle(t)
	bundle, err := readBundle(bundlePath)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}
	if bundle.BundleID != policy.ExampleBundle().BundleID {
		t.Fatalf("bundle id = %q", bundle.BundleID)
	}

	context := capturePolicyContext(bundlePath)
	if len(context.EvidenceMaps) == 0 {
		t.Fatal("capture policy context should include evidence maps")
	}
	if len(context.Skills) == 0 {
		t.Fatal("capture policy context should include skills")
	}
}

func TestEncodeLintResultWritesSARIFCategory(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "result.sarif")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create sarif: %v", err)
	}
	result := lint.Result{
		Scope:  lint.ScopeFiles,
		Status: "resolved",
	}
	if err := encodeLintResult(file, result, hookoutput.FormatSARIF, "policy"); err != nil {
		t.Fatalf("encode SARIF: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close SARIF: %v", err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read SARIF: %v", err)
	}
	if !strings.Contains(string(payload), `"automationDetails"`) ||
		!strings.Contains(string(payload), `"id": "policy/"`) {
		t.Fatalf("SARIF output missing category:\n%s", payload)
	}
}

func TestPrintCapturedToolsListsManagedTools(t *testing.T) {
	output := captureLintStdout(t, printCapturedTools)
	for _, want := range []string{"ruff", "mypy"} {
		if !strings.Contains(output, want) {
			t.Fatalf("captured tools missing %q:\n%s", want, output)
		}
	}
}

func TestScopeFlagSetSupportsBooleanAliases(t *testing.T) {
	t.Parallel()

	flags := flagSetForScopeTest()
	scope := scopeFlagSet(flags)
	if err := flags.Parse([]string{"--staged"}); err != nil {
		t.Fatalf("parse scope flags: %v", err)
	}
	if scope.Value() != lint.ScopeStaged {
		t.Fatalf("scope = %q", scope.Value())
	}
	if err := scope.Set(lint.ScopeFull); err != nil {
		t.Fatalf("set scope: %v", err)
	}
	if scope.String() != lint.ScopeFull {
		t.Fatalf("scope string = %q", scope.String())
	}
}

func TestManagedToolArgumentEnforcementUsesRepoConfigs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	venvPython := filepath.Join(root, ".venv", "bin", "python")
	if err := os.MkdirAll(filepath.Dir(venvPython), 0o755); err != nil {
		t.Fatalf("create venv bin: %v", err)
	}
	if err := os.WriteFile(venvPython, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write python executable: %v", err)
	}

	tests := []struct {
		name string
		tool string
		args []string
		want []string
	}{
		{
			name: "ruff check",
			tool: "ruff",
			args: []string{"check", "pkg"},
			want: []string{"check", "--config", filepath.Join(root, "ruff.toml"), "pkg"},
		},
		{
			name: "mypy",
			tool: "mypy",
			args: []string{"pkg"},
			want: []string{
				"--config-file", filepath.Join(root, "mypy.ini"),
				"--python-executable", venvPython,
				"pkg",
			},
		},
		{
			name: "dotenv",
			tool: "dotenv-linter",
			args: []string{"check", ".env"},
			want: []string{"--plain", "--quiet", "check", ".env"},
		},
		{
			name: "sqlfluff",
			tool: "sqlfluff",
			args: []string{"lint", "queries"},
			want: []string{"lint", "--config", filepath.Join(root, ".sqlfluff"), "queries"},
		},
		{
			name: "tombi",
			tool: "tombi",
			args: []string{"config.toml"},
			want: []string{"lint", "--quiet", "--error-on-warnings", "config.toml"},
		},
		{
			name: "golangci",
			tool: "golangci-lint",
			args: []string{"run"},
			want: []string{"run", "--config", filepath.Join(root, ".golangci.yml")},
		},
		{
			name: "bandit catalog fallback",
			tool: "bandit",
			args: []string{"pkg"},
			want: []string{
				"-c", filepath.Join(root, ".bandit.yml"),
				"pkg",
				"--severity-level", "medium",
				"--confidence-level", "medium",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			tool, ok := toolcatalog.HookOwnedTool(test.tool)
			if !ok {
				t.Fatalf("missing tool catalog entry %q", test.tool)
			}
			got := enforceManagedToolArgs(tool, test.args, root, "/ethos")
			if strings.Join(got, "\x00") != strings.Join(test.want, "\x00") {
				t.Fatalf("enforceManagedToolArgs() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestManagedToolArgumentEnforcementLeavesInformationalArgsAlone(t *testing.T) {
	t.Parallel()

	tool, ok := toolcatalog.HookOwnedTool("ruff")
	if !ok {
		t.Fatal("missing ruff tool")
	}
	args := []string{"--version"}
	got := enforceManagedToolArgs(tool, args, "/repo", "/ethos")
	if strings.Join(got, " ") != "--version" {
		t.Fatalf("informational args changed: %#v", got)
	}
}

func TestRunCLIListCapturedTools(t *testing.T) {
	stdout := captureLintStdout(t, func() {
		if code := runCLI([]string{"--list-captured-tools"}); code != 0 {
			t.Fatalf("runCLI list captured tools exit = %d", code)
		}
	})
	for _, want := range []string{"ruff", "mypy", "golangci-lint"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("captured tools output missing %q:\n%s", want, stdout)
		}
	}
}

func TestRunCLIInstallShims(t *testing.T) {
	t.Parallel()

	toolsDir := t.TempDir()
	if code := runCLI([]string{
		"--install-shims",
		"--tools-bin-dir", toolsDir,
		"--runner", "/repo/bin/coding-ethos-run",
		"--ethos-root", t.TempDir(),
	}); code != 0 {
		t.Fatalf("runCLI install shims exit = %d", code)
	}
}

func TestRunCLIEmptyExplicitFileScope(t *testing.T) {
	bundle := writeLintTestBundle(t)
	stdout := captureLintStdout(t, func() {
		code := runCLI([]string{
			"--bundle", bundle,
			"--scope", "files",
			"--files", " , ",
			"--log=false",
			"--json",
		})
		if code != 0 {
			t.Fatalf("runCLI empty files exit = %d", code)
		}
	})
	if !strings.Contains(stdout, `"status": "resolved"`) {
		t.Fatalf("empty explicit file scope output = %s", stdout)
	}
}

func TestRunCLIExplainMode(t *testing.T) {
	bundle := writeLintTestBundle(t)
	stdout := captureLintStdout(t, func() {
		code := runCLI([]string{
			"--bundle", bundle,
			"--explain",
			"--scope", "files",
			"--files", "README.md",
			"--json",
		})
		if code != 0 {
			t.Fatalf("runCLI explain exit = %d", code)
		}
	})
	if !strings.Contains(stdout, `"scope": "files"`) {
		t.Fatalf("explain output = %s", stdout)
	}
}

func TestRunCLIReplayAndAnalyzePersistedTrace(t *testing.T) {
	root := t.TempDir()
	tracePath, err := lint.LogResult(root, lint.Result{
		TraceID: "trace-a.json",
		Scope:   lint.ScopeFiles,
		Status:  "blocked",
		Files:   []string{"pkg/app.py"},
		Findings: []lint.Finding{{
			CheckID:    "ruff:F401",
			Code:       "F401",
			File:       "pkg/app.py",
			Message:    "unused import",
			Severity:   "error",
			Status:     "fail",
			SourceTool: "ruff",
			PolicyID:   "python.unused_imports",
			SkillID:    "lint-remediation",
			Blocking:   true,
		}},
	})
	if err != nil {
		t.Fatalf("log lint trace: %v", err)
	}

	replayOutput := captureLintStdout(t, func() {
		code := runCLI([]string{"--replay", tracePath, "--json"})
		if code != blockedExitCode {
			t.Fatalf("replay exit = %d, want %d", code, blockedExitCode)
		}
	})
	if !strings.Contains(replayOutput, `"trace_id": "trace-a.json"`) ||
		!strings.Contains(replayOutput, `"status": "blocked"`) {
		t.Fatalf("replay output = %s", replayOutput)
	}

	analyzeOutput := captureLintStdout(t, func() {
		code := runCLI([]string{
			"--analyze-log",
			"--log-dir", filepath.Dir(tracePath),
			"--for-files", "pkg/app.py",
			"--json",
		})
		if code != 0 {
			t.Fatalf("analyze exit = %d", code)
		}
	})
	if !strings.Contains(analyzeOutput, `"runs_analyzed": 1`) ||
		!strings.Contains(analyzeOutput, `"F401"`) {
		t.Fatalf("analyze output = %s", analyzeOutput)
	}
}

func TestInstallCapturedToolShimWritesQuotedRunnerAndToolEnv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runner := filepath.Join(root, "runner with ' quote")
	if err := installCapturedToolShim(root, runner, "golangci-lint"); err != nil {
		t.Fatalf("install shim: %v", err)
	}

	payload, err := os.ReadFile(filepath.Join(root, "golangci-lint"))
	if err != nil {
		t.Fatalf("read shim: %v", err)
	}
	text := string(payload)
	for _, want := range []string{
		"#!/usr/bin/env bash",
		"unset CODING_ETHOS_REAL_GOLANGCI_LINT",
		"export CODING_ETHOS_POLICY_TOOL_SHIM=1",
		"policy-tool 'golangci-lint' \"$@\"",
		"runner with '\\'' quote",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("shim missing %q:\n%s", want, text)
		}
	}
}

func TestInstallCapturedToolShimsValidatesRequiredArgs(t *testing.T) {
	t.Parallel()

	if err := installCapturedToolShims("", "/runner", "/ethos"); err != errToolsBinDirRequired {
		t.Fatalf("missing tools dir error = %v, want %v", err, errToolsBinDirRequired)
	}
	if err := installCapturedToolShims(t.TempDir(), "", "/ethos"); err != errRunnerRequired {
		t.Fatalf("missing runner error = %v, want %v", err, errRunnerRequired)
	}
}

func TestManagedToolAvailableUsesManagedExecutablePath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	actionlint, ok := toolcatalog.HookOwnedTool("actionlint")
	if !ok {
		t.Fatal("missing actionlint tool")
	}
	if managedToolAvailable(actionlint, root) {
		t.Fatal("managed tool should be unavailable before executable exists")
	}

	executable := actionlint.ManagedExecutablePath(root)
	if err := os.MkdirAll(filepath.Dir(executable), 0o755); err != nil {
		t.Fatalf("create managed dir: %v", err)
	}
	if err := os.WriteFile(executable, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write managed executable: %v", err)
	}
	if !managedToolAvailable(actionlint, root) {
		t.Fatal("managed tool should be available after executable exists")
	}
}

func TestRealToolEnvVarAndShellQuote(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"golangci-lint": "CODING_ETHOS_REAL_GOLANGCI_LINT",
		"ruff":          "CODING_ETHOS_REAL_RUFF",
		"dotenv-linter": "CODING_ETHOS_REAL_DOTENV_LINTER",
	}
	for tool, want := range cases {
		if got := realToolEnvVar(tool); got != want {
			t.Fatalf("realToolEnvVar(%q) = %q, want %q", tool, got, want)
		}
	}
	if got := shellQuote("a'b"); got != "'a'\\''b'" {
		t.Fatalf("shellQuote() = %q", got)
	}
}

func writeLintTestBundle(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "policy-bundle.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}
	if err := policy.EncodeBundle(file, policy.ExampleBundle()); err != nil {
		t.Fatalf("encode bundle: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close bundle: %v", err)
	}

	return path
}

func captureLintStdout(t *testing.T, run func()) string {
	t.Helper()

	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = original
	}()

	run()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var buffer bytes.Buffer
	if _, err := io.Copy(&buffer, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return buffer.String()
}

func flagSetForScopeTest() *flag.FlagSet {
	return flag.NewFlagSet("scope-test", flag.ContinueOnError)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
