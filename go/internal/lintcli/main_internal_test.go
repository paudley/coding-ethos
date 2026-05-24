// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	lintTestExecutableMode os.FileMode = 0o755
	lintTestWriteMode      os.FileMode = 0o600
	samplePythonFile                   = "pkg/app.py"
)

func TestStagedFilesListsGitIndexEntries(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")

	path := filepath.Join(repo, samplePythonFile)

	err := os.MkdirAll(filepath.Dir(path), lintTestExecutableMode)
	if err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	err = os.WriteFile(path, []byte("print('x')\n"), lintTestWriteMode)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runGit(t, repo, "add", samplePythonFile)

	files, err := stagedFiles(repo)
	if err != nil {
		t.Fatalf("staged files: %v", err)
	}

	if len(files) != 1 || files[0] != samplePythonFile {
		t.Fatalf("staged files = %#v, want %s", files, samplePythonFile)
	}
}

func TestFilesFromInputsCombinesFlagAndFileLists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "files.txt")

	err := os.WriteFile(
		path,
		[]byte("pkg/app.py\n\npkg/other.py\r\n"),
		lintTestWriteMode,
	)
	if err != nil {
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

	if shouldReturnEmptyExplicitFileScope(
		"files",
		[]string{samplePythonFile},
		"",
		"files.txt",
	) {
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

	_, err = lintOutputFormat(true, true)
	if !errors.Is(err, errOutputFormatConflict) {
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

	err = encodeLintResult(
		file,
		result,
		hookoutput.FormatSARIF,
		"policy",
	)
	if err != nil {
		t.Fatalf("encode SARIF: %v", err)
	}

	err = file.Close()
	if err != nil {
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
	t.Parallel()

	var output bytes.Buffer

	printCapturedTools(&output)

	for _, want := range []string{"ruff", "mypy"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("captured tools missing %q:\n%s", want, output.String())
		}
	}
}

func TestScopeFlagSetSupportsBooleanAliases(t *testing.T) {
	t.Parallel()

	flags := flagSetForScopeTest()

	scope := scopeFlagSet(flags)

	err := flags.Parse([]string{"--staged"})
	if err != nil {
		t.Fatalf("parse scope flags: %v", err)
	}

	if scope.Value() != lint.ScopeStaged {
		t.Fatalf("scope = %q", scope.Value())
	}

	err = scope.Set(lint.ScopeFull)
	if err != nil {
		t.Fatalf("set scope: %v", err)
	}

	if scope.String() != lint.ScopeFull {
		t.Fatalf("scope string = %q", scope.String())
	}
}

func TestRunCLIListCapturedTools(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer

	code := runCLIWithWriter([]string{"--list-captured-tools"}, &stdout)
	if code != 0 {
		t.Fatalf("runCLI list captured tools exit = %d", code)
	}

	for _, want := range []string{"ruff", "mypy", "golangci-lint"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("captured tools output missing %q:\n%s", want, stdout.String())
		}
	}
}

func TestMissingBundleErrorPointsToManagedEntrypoints(t *testing.T) {
	t.Parallel()

	message := errBundleRequired.Error()
	for _, want := range []string{
		"coding-ethos-run lint",
		"./bin/lint",
		"active policy bundle",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("missing bundle error lacks %q: %s", want, message)
		}
	}
}

func TestRunCLIInstallShims(t *testing.T) {
	t.Parallel()

	toolsDir := t.TempDir()
	if code := Run([]string{
		"--install-shims",
		"--tools-bin-dir", toolsDir,
		"--runner", "/repo/bin/coding-ethos-run",
		"--ethos-root", t.TempDir(),
	}); code != 0 {
		t.Fatalf("runCLI install shims exit = %d", code)
	}
}

func TestRunCLIEmptyExplicitFileScope(t *testing.T) {
	t.Parallel()

	bundle := writeLintTestBundle(t)

	var stdout bytes.Buffer

	code := runCLIWithWriter([]string{
		"--bundle", bundle,
		"--scope", "files",
		"--files", " , ",
		"--log=false",
		"--json",
	}, &stdout)
	if code != 0 {
		t.Fatalf("runCLI empty files exit = %d", code)
	}

	if !strings.Contains(stdout.String(), `"status": "resolved"`) {
		t.Fatalf("empty explicit file scope output = %s", stdout.String())
	}
}

func TestRunCLIExplainMode(t *testing.T) {
	t.Parallel()

	bundle := writeLintTestBundle(t)

	var stdout bytes.Buffer

	code := runCLIWithWriter([]string{
		"--bundle", bundle,
		"--explain",
		"--scope", "files",
		"--files", "README.md",
		"--json",
	}, &stdout)
	if code != 0 {
		t.Fatalf("runCLI explain exit = %d", code)
	}

	if !strings.Contains(stdout.String(), `"scope": "files"`) {
		t.Fatalf("explain output = %s", stdout.String())
	}
}

func TestRunCLIReplayAndAnalyzePersistedTrace(t *testing.T) {
	t.Parallel()

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

	var replayOutput bytes.Buffer

	code := runCLIWithWriter([]string{"--replay", tracePath, "--json"}, &replayOutput)
	if code != blockedExitCode {
		t.Fatalf("replay exit = %d, want %d", code, blockedExitCode)
	}

	if !strings.Contains(replayOutput.String(), `"trace_id": "trace-a.json"`) ||
		!strings.Contains(replayOutput.String(), `"status": "blocked"`) {
		t.Fatalf("replay output = %s", replayOutput.String())
	}

	var analyzeOutput bytes.Buffer

	code = runCLIWithWriter([]string{
		"--analyze-log",
		"--log-dir", filepath.Dir(tracePath),
		"--for-files", "pkg/app.py",
		"--json",
	}, &analyzeOutput)
	if code != 0 {
		t.Fatalf("analyze exit = %d", code)
	}

	if !strings.Contains(analyzeOutput.String(), `"runs_analyzed": 1`) ||
		!strings.Contains(analyzeOutput.String(), `"F401"`) {
		t.Fatalf("analyze output = %s", analyzeOutput.String())
	}
}

func TestInstallCapturedToolShimWritesQuotedRunnerAndToolEnv(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	runner := filepath.Join(root, "runner with ' quote")

	err := installCapturedToolShim(root, runner, "golangci-lint")
	if err != nil {
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

	err := installCapturedToolShims("", "/runner", "/ethos")
	if !errors.Is(err, errToolsBinDirRequired) {
		t.Fatalf("missing tools dir error = %v, want %v", err, errToolsBinDirRequired)
	}

	err = installCapturedToolShims(t.TempDir(), "", "/ethos")
	if !errors.Is(err, errRunnerRequired) {
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

	err := os.MkdirAll(filepath.Dir(executable), lintTestExecutableMode)
	if err != nil {
		t.Fatalf("create managed dir: %v", err)
	}

	writeLintTestExecutable(t, executable)

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

	err = policy.EncodeBundle(file, policy.ExampleBundle())
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("close bundle: %v", err)
	}

	return path
}

func flagSetForScopeTest() *flag.FlagSet {
	return flag.NewFlagSet("scope-test", flag.ContinueOnError)
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	command := exec.CommandContext(context.Background(), gitPath, args...)
	command.Dir = dir

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func writeLintTestExecutable(t *testing.T, path string) {
	t.Helper()

	err := os.WriteFile(
		path,
		[]byte("#!/usr/bin/env sh\nexit 0\n"),
		lintTestWriteMode,
	)
	if err != nil {
		t.Fatalf("write executable fixture: %v", err)
	}

	err = os.Chmod(path, lintTestExecutableMode)
	if err != nil {
		t.Fatalf("chmod executable fixture: %v", err)
	}
}
