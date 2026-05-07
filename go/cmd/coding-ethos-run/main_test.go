// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestRunnerArgsInferGitHookFromExecutableName(t *testing.T) {
	t.Parallel()

	args := runnerArgs([]string{"/repo/.git/hooks/pre-commit"})

	want := []string{"git-hook", "pre-commit"}
	if !slices.Equal(args, want) {
		t.Fatalf("runnerArgs() = %#v, want %#v", args, want)
	}
}

func TestRunnerArgsInferLFSHookFromExecutableName(t *testing.T) {
	t.Parallel()

	args := runnerArgs([]string{"/repo/.git/hooks/post-merge"})

	want := []string{"lfs-hook", "post-merge"}
	if !slices.Equal(args, want) {
		t.Fatalf("runnerArgs() = %#v, want %#v", args, want)
	}
}

func TestRunnerArgsPreserveExplicitCommand(t *testing.T) {
	t.Parallel()

	args := runnerArgs([]string{"coding-ethos-run", "policy-lint", "--json"})

	want := []string{"policy-lint", "--json"}
	if !slices.Equal(args, want) {
		t.Fatalf("runnerArgs() = %#v, want %#v", args, want)
	}
}

func TestSplitCIFileListHandlesCommasAndNewlines(t *testing.T) {
	t.Parallel()

	files := splitCIFileList("a.py,b.py\n c.py \n")

	want := []string{"a.py", "b.py", "c.py"}
	if !slices.Equal(files, want) {
		t.Fatalf("splitCIFileList() = %#v, want %#v", files, want)
	}
}

func TestFilterExistingFilesKeepsOnlyRegularFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := os.WriteFile(filepath.Join(root, "a.py"), []byte("x\n"), 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err = os.Mkdir(filepath.Join(root, "pkg"), 0o700)
	if err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	files := filterExistingFiles(root, []string{"a.py", "missing.py", "pkg"})

	want := []string{"a.py"}
	if !slices.Equal(files, want) {
		t.Fatalf("filterExistingFiles() = %#v, want %#v", files, want)
	}
}

func TestWriteCIFileListWritesNewlineSeparatedPaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	path, err := writeCIFileList(root, []string{"a.py", "b.py"})
	if err != nil {
		t.Fatalf("writeCIFileList: %v", err)
	}

	t.Cleanup(func() {
		_ = os.Remove(path)
	})

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file list: %v", err)
	}

	if strings.TrimSpace(string(payload)) != "a.py\nb.py" {
		t.Fatalf("file list payload = %q", payload)
	}
}

func TestPolicyToolLintArgsPropagateSandboxMode(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")
	t.Setenv("CODING_ETHOS_POLICY_TOOL_SHIM", "1")

	args := policyToolLintArgs(runtimePaths{
		PolicyBundle:  "/repo/build/policy/policy-bundle.json",
		EthosRoot:     "/repo/coding-ethos",
		Root:          "/repo",
		InvocationCWD: "/repo/pkg",
	}, "ruff", []string{"check", "pkg"})

	for _, want := range []string{
		"--bundle",
		"/repo/build/policy/policy-bundle.json",
		"--managed-capture-tool",
		"ruff",
		"--ethos-root",
		"/repo/coding-ethos",
		"--consumer-root",
		"/repo",
		"--invocation-cwd",
		"/repo/pkg",
		"--sandbox-mode",
		"required",
		"--",
		"check",
		"pkg",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("policyToolLintArgs() missing %q: %#v", want, args)
		}
	}

	if slices.Index(args, "--sandbox-mode") > slices.Index(args, "--") {
		t.Fatalf("sandbox flag must precede tool args separator: %#v", args)
	}
}

func TestPolicyToolLintArgsOmitBlankSandboxMode(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", " ")
	t.Setenv("CODING_ETHOS_POLICY_TOOL_SHIM", "1")

	args := policyToolLintArgs(runtimePaths{}, "ruff", []string{"check"})

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf("blank sandbox mode should not be forwarded: %#v", args)
	}
}

func TestPolicyToolLintArgsIgnoreAmbientSandboxModeOutsideShim(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")

	args := policyToolLintArgs(runtimePaths{}, "ruff", []string{"check"})

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf(
			"ambient sandbox mode should not affect direct policy-tool calls: %#v",
			args,
		)
	}
}

func TestCodeIntelArgsInsertRootAfterSubcommand(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs("/repo", []string{"stats", "--db", "/tmp/code-intel.db"})

	want := []string{"stats", "--root", "/repo", "--db", "/tmp/code-intel.db"}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestCodeIntelArgsKeepExplicitRoot(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs("/repo", []string{"stats", "--root", "/other"})

	want := []string{"stats", "--root", "/other"}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestRuntimePathSetDerivesManagedPaths(t *testing.T) {
	t.Parallel()

	inputs := runtimePathInputs{
		RealGit:       "/usr/bin/git",
		InvocationCWD: "/repo/pkg",
		LocalRoot:     "/repo",
		Root:          "/repo",
		HooksDir:      "/repo/.git/hooks",
		BinDir:        "/repo/coding-ethos/bin",
		RunBinary:     "/repo/coding-ethos/bin/coding-ethos-run",
		BundleRoot:    "/repo/coding-ethos/pre-commit",
		EthosRoot:     "/repo/coding-ethos",
		ToolchainDir:  "/repo/coding-ethos/build/toolchain",
	}

	paths := runtimePathSet(inputs)

	if paths.PolicyBundle != "/repo/coding-ethos/build/policy/policy-bundle.json" {
		t.Fatalf("policy bundle = %q", paths.PolicyBundle)
	}

	if paths.ManagedGoBin != "/repo/coding-ethos/build/toolchain/go-bin" {
		t.Fatalf("managed go bin = %q", paths.ManagedGoBin)
	}

	if paths.GitHookRunner != "/repo/coding-ethos/bin/coding-ethos-hook-runner" {
		t.Fatalf("git hook runner = %q", paths.GitHookRunner)
	}
}

func TestRuntimePathResolutionFallbacks(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	t.Setenv("CODE_ETHOS_CONSUMER_ROOT", "/configured/repo")

	root, localRoot := resolveRuntimeRoot("/missing/git", "/cwd/repo")
	if root != "/configured/repo" || localRoot != "/configured/repo" {
		t.Fatalf("configured root = (%q, %q)", root, localRoot)
	}

	hooksDir := resolveRuntimeHooksDir("/missing/git", "/fallback/repo")
	if hooksDir != "/fallback/repo/.git/hooks" {
		t.Fatalf("fallback hooks dir = %q", hooksDir)
	}
}

func TestRuntimePathResolutionUsesGitWhenAvailable(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "coding-ethos-run-env")

	repo := filepath.Join(t.TempDir(), "repo")
	hooks := filepath.Join(repo, ".git", "hooks")
	fakeGit := fakeRuntimeGit(t, repo, hooks)

	root, localRoot := resolveRuntimeRoot(fakeGit, "/cwd/repo")
	if root != repo || localRoot != repo {
		t.Fatalf("git root = (%q, %q), want %q", root, localRoot, repo)
	}

	if got := resolveRuntimeHooksDir(fakeGit, repo); got != hooks {
		t.Fatalf("git hooks dir = %q, want %q", got, hooks)
	}
}

func TestRuntimePathsExportManagedEnvironment(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	restoreEnv := captureRuntimeEnvForTest(
		"INVOCATION_CWD",
		"CODE_ETHOS_PRECOMMIT_ROOT",
		"CODE_ETHOS_CONSUMER_ROOT",
		"CODING_ETHOS_RUN_GO_HOOK",
		"GIT_HOOK_SRC_DIR",
		"TOOLS_SRC_DIR",
		"POLICY_METADATA",
		"MANAGED_TOOLCHAIN_MANIFEST",
		"CODING_ETHOS_REAL_GIT",
		"PATH",
	)
	t.Cleanup(restoreEnv)

	paths := runtimePathSet(runtimePathInputs{
		RealGit:       "/usr/bin/git",
		InvocationCWD: "/repo/pkg",
		LocalRoot:     "/repo",
		Root:          "/repo",
		HooksDir:      "/repo/.git/hooks",
		BinDir:        "/repo/coding-ethos/bin",
		RunBinary:     "/repo/coding-ethos/bin/coding-ethos-run",
		BundleRoot:    "/repo/coding-ethos/pre-commit",
		EthosRoot:     "/repo/coding-ethos",
		ToolchainDir:  "/repo/coding-ethos/build/toolchain",
	})

	t.Setenv("PATH", "/usr/bin")

	paths.export()

	if got := os.Getenv("CODE_ETHOS_CONSUMER_ROOT"); got != "/repo" {
		t.Fatalf("exported consumer root = %q", got)
	}

	if got := os.Getenv("CODING_ETHOS_REAL_GIT"); got != "/usr/bin/git" {
		t.Fatalf("exported real git = %q", got)
	}

	if got := os.Getenv("PATH"); !strings.HasPrefix(
		got,
		"/repo/coding-ethos/build/toolchain/go-bin:",
	) {
		t.Fatalf("exported PATH = %q", got)
	}
}

func captureRuntimeEnvForTest(names ...string) func() {
	type envValue struct {
		value string
		found bool
	}

	values := map[string]envValue{}

	for _, name := range names {
		value, found := os.LookupEnv(name)
		values[name] = envValue{value: value, found: found}
	}

	return func() {
		for name, value := range values {
			if value.found {
				_ = os.Setenv(name, value.value)

				continue
			}

			_ = os.Unsetenv(name)
		}
	}
}

func TestRuntimeFailuresUseStructuredExitCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		run  func()
		name string
		want int
	}{
		{
			name: "runtime failure",
			run: func() {
				runtimeFailure("missing fixture")
			},
			want: exitMissing,
		},
		{
			name: "internal tool direct execution",
			run: func() {
				requireExternalRuntimeTool("coding-ethos-policy")
			},
			want: exitMissing,
		},
		{
			name: "plain error",
			run: func() {
				exitErr(apperror.StaticError("plain failure"))
			},
			want: 1,
		},
		{
			name: "process exit code",
			run: func() {
				err := exec.CommandContext(context.Background(), "sh", "-c", "exit 7").Run()
				exitErr(err)
			},
			want: 7,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := withRuntimeExit(func() int {
				test.run()

				return 0
			})
			if got != test.want {
				t.Fatalf("runtime exit = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRuntimeFileBinaryAndRunToolHappyPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")

	err := os.MkdirAll(binDir, 0o755)
	if err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	filePath := filepath.Join(root, "policy-bundle.json")

	err = os.WriteFile(filePath, []byte("{}\n"), 0o600)
	if err != nil {
		t.Fatalf("write policy bundle: %v", err)
	}

	toolPath := filepath.Join(binDir, "tool")

	writeExecutableFixture(t, toolPath, "#!/usr/bin/env sh\nprintf 'ok\\n'\n")

	paths := runtimePaths{BinDir: binDir, PolicyBundle: filePath}
	requireRuntimeFile(filePath, "policy bundle")
	requireRuntimeBinary(toolPath, "tool")
	requirePolicyBundle(paths)
	runtimeRunTool(paths, "tool")
}

func fakeRuntimeGit(t *testing.T, repo, hooks string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "git")
	body := "#!/usr/bin/env sh\n" +
		"case \"$*\" in\n" +
		"  \"rev-parse --show-toplevel\") printf '%s\\n' " +
		shellQuoteForRuntimeTest(repo) + "; exit 0 ;;\n" +
		"  \"rev-parse --path-format=absolute --git-path hooks\") printf '%s\\n' " +
		shellQuoteForRuntimeTest(hooks) + "; exit 0 ;;\n" +
		"esac\n" +
		"exit 1\n"
	writeExecutableFixture(t, path, body)

	return path
}

func shellQuoteForRuntimeTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func writeExecutableFixture(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write executable fixture %s: %v", path, err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod executable fixture %s: %v", path, err)
	}
}

func TestGitOutputRunsRealGitPath(t *testing.T) {
	t.Parallel()

	gitPath, err := resolveRuntimeGit()
	if err != nil {
		t.Fatalf("resolve runtime git: %v", err)
	}

	output, err := gitOutput(gitPath, "", "--version")
	if err != nil {
		t.Fatalf("gitOutput() returned error: %v", err)
	}

	if !strings.HasPrefix(output, "git version ") {
		t.Fatalf("gitOutput() = %q", output)
	}
}

func TestCIChangedFilesUsesExplicitFileListAndProviderDiffs(t *testing.T) {
	repo := t.TempDir()

	inlineErr1 := os.WriteFile(filepath.Join(repo, "a.py"), []byte("a\n"), 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write a.py: %v", inlineErr1)
	}

	inlineErr2 := os.WriteFile(filepath.Join(repo, "b.go"), []byte("b\n"), 0o600)
	if inlineErr2 != nil {
		t.Fatalf("write b.go: %v", inlineErr2)
	}

	t.Setenv("CODING_ETHOS_FILES", "a.py, missing.py\nb.go")

	files, err := ciChangedFiles("/usr/bin/git", repo, "github")
	if err != nil {
		t.Fatalf("explicit ci files: %v", err)
	}

	if !slices.Equal(files, []string{"a.py", "b.go"}) {
		t.Fatalf("explicit files = %#v", files)
	}

	t.Setenv("CODING_ETHOS_FILES", "")
	t.Setenv("CODING_ETHOS_GITHUB_BASE_REF", "main")
	fakeGit := fakeCIGit(t, "a.py\nmissing.py\n")

	files, err = ciChangedFiles(fakeGit, repo, "github")
	if err != nil {
		t.Fatalf("github ci files: %v", err)
	}

	if !slices.Equal(files, []string{"a.py"}) {
		t.Fatalf("github files = %#v", files)
	}

	_, inlineErrAutoA := ciChangedFiles(fakeGit, repo, "unknown")
	if inlineErrAutoA == nil {
		t.Fatal("unknown provider should fail")
	}
}

func TestCISARIFHelpers(t *testing.T) {
	t.Parallel()

	if !isZeroGitSHA("0000000000000000000000000000000000000000") ||
		isZeroGitSHA("0000000000000000000000000000000000000001") {
		t.Fatal("zero SHA helper misclassified input")
	}

	if got := envOrDefault(
		"CODING_ETHOS_MISSING_FOR_TEST",
		"fallback",
	); got != "fallback" {
		t.Fatalf("env fallback = %q", got)
	}
}

func TestRunCISARIFWritesSARIFThroughDirectLintCLI(t *testing.T) {
	repo := t.TempDir()

	binDir := filepath.Join(repo, "bin")

	inlineErr3 := os.MkdirAll(binDir, 0o755)
	if inlineErr3 != nil {
		t.Fatalf("create bin dir: %v", inlineErr3)
	}

	for _, name := range []string{"a.py", "b.go"} {
		err := os.WriteFile(filepath.Join(repo, name), []byte("x\n"), 0o600)
		if err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	policyBundle := filepath.Join(repo, "policy-bundle.json")

	err := os.WriteFile(
		policyBundle,
		[]byte(`{
  "version": 1,
  "bundle_id": "test",
  "sources": {
    "ethos": {"primary": "coding_ethos.yml"},
    "enforcement": {"primary": "config.yaml"}
  },
  "policies": {},
  "principles": {},
  "skills": {},
  "evidence_maps": []
}`),
		0o600,
	)
	if err != nil {
		t.Fatalf("write policy bundle: %v", err)
	}

	sarifPath := filepath.Join(repo, "reports", "coding-ethos.sarif")

	t.Setenv("CODING_ETHOS_FILES", "a.py,b.go,missing.py")
	t.Setenv("CODING_ETHOS_REPO_ROOT", repo)
	t.Setenv("CODING_ETHOS_SARIF_PATH", sarifPath)
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")
	t.Setenv("CODING_ETHOS_SARIF_CATEGORY", "policy")

	err = runCISARIF(runtimePaths{
		BinDir:       binDir,
		PolicyBundle: policyBundle,
		RealGit:      "/usr/bin/git",
		Root:         filepath.Join(repo, "fallback-root"),
	}, []string{"--provider", "github"})
	if err != nil {
		t.Fatalf("runCISARIF: %v", err)
	}

	payload, err := os.ReadFile(sarifPath)
	if err != nil {
		t.Fatalf("read SARIF: %v", err)
	}

	if !strings.Contains(string(payload), `"version": "2.1.0"`) {
		t.Fatalf("SARIF payload = %q", payload)
	}
}

func TestCISARIFLintArgsPassManagedFlags(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")
	t.Setenv("CODING_ETHOS_SARIF_CATEGORY", "policy")

	repo := t.TempDir()
	paths := runtimePaths{PolicyBundle: filepath.Join(repo, "policy-bundle.json")}
	args := ciSARIFLintArgs(paths, repo, filepath.Join(repo, "files.txt"))

	argsText := strings.Join(args, "\n")
	for _, want := range ciSARIFExpectedLintArgs(repo) {
		if !strings.Contains(argsText, want) {
			t.Fatalf("lint args missing %q:\n%s", want, argsText)
		}
	}

	if strings.Count(argsText+"\n", "--sarif\n") != 1 {
		t.Fatalf("expected one terminal --sarif flag:\n%s", argsText)
	}
}

func ciSARIFExpectedLintArgs(repo string) []string {
	return []string{
		"--bundle",
		filepath.Join(repo, "policy-bundle.json"),
		"--cwd",
		repo,
		"--scope",
		"files",
		"--files-from",
		"--sandbox-mode",
		"required",
		"--sarif-category",
		"policy",
		"--sarif",
	}
}

func TestRunCISARIFRequiresProviderAndOutputPath(t *testing.T) {
	t.Setenv("CODING_ETHOS_SARIF_PATH", "")

	err := runCISARIF(runtimePaths{}, nil)
	if err == nil ||
		!strings.Contains(err.Error(), "requires --provider") {
		t.Fatalf("missing provider error = %v", err)
	}

	err = runCISARIF(runtimePaths{}, []string{"--provider", "github"})
	if err == nil ||
		!strings.Contains(err.Error(), "CODING_ETHOS_SARIF_PATH") {
		t.Fatalf("missing SARIF path error = %v", err)
	}
}

func TestRunDispatchesCriticalCommandsThroughRuntimeOps(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "coding-ethos-run-env")

	restoreEnv := captureRuntimeEnvForTest(
		"CODE_ETHOS_CONSUMER_ROOT",
		"CODING_ETHOS_GIT_SHIM_DIR",
	)
	t.Cleanup(restoreEnv)

	paths := runtimeTestPaths(t)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	cases := []struct {
		name string
		want string
		args []string
	}{
		{
			name: "policy lint",
			args: []string{"policy-lint", "--scope", "staged"},
			want: "exec-lint:--bundle " + paths.PolicyBundle + " --scope staged",
		},
		{
			name: "policy command",
			args: []string{"policy", "validate"},
			want: "direct-exec:coding-ethos-policy validate",
		},
		{
			name: "code intel",
			args: []string{"code-intel", "stats"},
			want: "direct-exec:coding-ethos-code-intel stats --root " + paths.Root,
		},
		{
			name: "policy tool",
			args: []string{"policy-tool", "ruff", "check"},
			want: "exec-lint:--bundle " + paths.PolicyBundle +
				" --managed-capture-tool ruff --ethos-root " + paths.EthosRoot +
				" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
				" -- check",
		},
		{
			name: "policy tool group",
			args: []string{"policy-tool-group", "formatters"},
			want: "run-lint:--bundle " + paths.PolicyBundle +
				" --managed-capture-tool ruff-format --ethos-root " + paths.EthosRoot +
				" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
				" -- format coding_ethos tests\n" +
				"run-lint:--bundle " + paths.PolicyBundle +
				" --managed-capture-tool golangci-lint-format --ethos-root " +
				paths.EthosRoot + " --consumer-root " + paths.Root +
				" --invocation-cwd " + paths.InvocationCWD + " -- go",
		},
		{
			name: "agent hooks",
			args: []string{"agent-hooks", "sync"},
			want: "direct-run:coding-ethos-toolchain install-git-shim " +
				"--dest-dir " + paths.BinDir +
				" --real-git " + paths.RealGit + " --runner " + paths.RunBinary + "\n" +
				"run-lint:--install-shims --tools-bin-dir " + paths.BinDir +
				" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot + "\n" +
				"direct-exec:coding-ethos-agent-hooks sync --hook-command " +
				paths.RunBinary + " agent-hook",
		},
	}
	for _, test := range cases {
		calls = nil

		err := run(paths, test.args)
		if err != nil {
			t.Fatalf("%s run: %v", test.name, err)
		}

		got := strings.Join(calls, "\n")
		if got != test.want {
			t.Fatalf("%s calls = %q, want %q", test.name, got, test.want)
		}
	}
}

func TestRunRuntimePropagatesInProcessRuntimeExit(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls, execLintCode: 37}

	code := runRuntime(paths, []string{"policy-tool", "ruff", "check"})
	if code != 37 {
		t.Fatalf("runRuntime exit = %d, want 37", code)
	}
}

func TestPolicyToolGroupRunsAllEntriesBeforeFailing(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls, runLintCode: 37}

	code := runRuntime(paths, []string{"policy-tool-group", "formatters"})
	if code != 37 {
		t.Fatalf("runRuntime exit = %d, want 37", code)
	}

	if len(calls) != 2 {
		t.Fatalf("policy-tool-group calls = %#v, want both group entries", calls)
	}

	if !strings.Contains(calls[0], "ruff-format") ||
		!strings.Contains(calls[1], "golangci-lint-format") {
		t.Fatalf("policy-tool-group calls = %#v, want formatter group order", calls)
	}
}

func TestMakefileRoutesLintTargetsThroughManagedGroups(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile(filepath.Join("..", "..", "..", "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}

	makefile := string(payload)
	for _, forbidden := range []string{
		"GOLANGCI_LINT",
		"GOLINES",
		"golangci-lint run",
		"golangci-lint fmt",
		"ruff check coding_ethos",
		"ruff format",
	} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("Makefile contains unmanaged tool invocation %q", forbidden)
		}
	}

	for _, want := range []string{
		`"$(GO_HOOK)" policy-tool-group linters`,
		`"$(GO_HOOK)" policy-tool-group formatters`,
		`"$(GO_HOOK)" policy-tool-group autofixers`,
		"lint-fix: format fix",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile missing managed group route %q", want)
		}
	}
}

func fakeCIGit(t *testing.T, diffOutput string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "git")

	payload := "#!/usr/bin/env sh\n" +
		"case \"$*\" in\n" +
		"  *rev-parse*) exit 0 ;;\n" +
		"  *diff*) printf '%s' '" +
		strings.ReplaceAll(diffOutput, "'", "'\\''") +
		"' ; exit 0 ;;\n" +
		"esac\n" +
		"exit 1\n"

	writeExecutableFixture(t, path, payload)

	return path
}

func TestRunGitHookAndCutoverUseRuntimeOps(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	err := runGitHook(paths, []string{"validate"})
	if err != nil {
		t.Fatalf("runGitHook validate: %v", err)
	}

	for _, want := range []string{
		"direct-run:coding-ethos-policy validate-metadata --metadata " + paths.PolicyMetadata,
		"run-lint:--install-shims --tools-bin-dir " + paths.BinDir +
			" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot,
		"direct-exec:coding-ethos-git-hook --bundle " + paths.PolicyBundle + " --runner " +
			paths.GitHookRunner + " --cwd " + paths.Root + " validate",
	} {
		if !slices.Contains(calls, want) {
			t.Fatalf("git hook calls missing %q: %#v", want, calls)
		}
	}

	calls = nil

	err = runCutover(paths, []string{"install"})
	if err != nil {
		t.Fatalf("runCutover install: %v", err)
	}

	for _, want := range []string{
		"direct-run:coding-ethos-toolchain install-git-hooks " +
			"--hooks-dir " + paths.HooksDir +
			" --runner " + paths.RunBinary,
		"direct-run:coding-ethos-agent-hooks sync --root " + paths.Root,
		"direct-exec:coding-ethos-toolchain cutover-verify --action install " +
			"--root " + paths.Root,
	} {
		if !strings.Contains(strings.Join(calls, "\n"), want) {
			t.Fatalf("cutover calls missing %q: %#v", want, calls)
		}
	}
}

func TestRunAgentHookMCPAndLFSUseRuntimeOps(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	runAgentHook(paths, []string{"PreToolUse"})
	runMCP(paths, []string{"--log-level", "debug"})

	paths.RealGit = "/usr/bin/true"

	err := runLFSHook(paths, []string{"post-merge", "arg"})
	if err != nil {
		t.Fatalf("runLFSHook: %v", err)
	}

	joined := strings.Join(calls, "\n")
	for _, want := range []string{
		"direct-exec:coding-ethos-hook --bundle " + paths.PolicyBundle + " --json",
		"direct-exec:coding-ethos-mcp --bundle " + paths.PolicyBundle,
		"external:" + paths.RealGit + " lfs post-merge arg",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("runtime calls missing %q:\n%s", want, joined)
		}
	}
}

func TestRunReportsInvalidCommandsBeforeExec(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	assertInvalidRunCommand(t, "policy-tool missing tool", func() error {
		return runPolicyTool(paths, nil)
	}, "requires a tool name")
	assertInvalidRunCommand(t, "git-hook missing hook", func() error {
		return runGitHook(paths, nil)
	}, "requires a hook name")
	assertInvalidRunCommand(t, "git-hook unknown hook", func() error {
		return runGitHook(paths, []string{"post-merge"})
	}, "unknown git hook")
	assertInvalidRunCommand(t, "cutover unknown action", func() error {
		return runCutover(paths, []string{"explode"})
	}, "unknown cutover action")
	assertInvalidRunCommand(t, "policy-tool-group missing group", func() error {
		return runPolicyToolGroup(paths, nil)
	}, "requires a group name")
	assertInvalidRunCommand(t, "policy-tool-group unknown group", func() error {
		return runPolicyToolGroup(paths, []string{"explode"})
	}, "unknown policy-tool group")
	assertInvalidRunCommand(t, "runner missing command", func() error {
		return run(paths, nil)
	}, "requires a command")
	assertInvalidRunCommand(t, "runner unknown command", func() error {
		return run(paths, []string{"explode"})
	}, "unknown coding-ethos-run command")
}

func assertInvalidRunCommand(
	t *testing.T,
	name string,
	run func() error,
	want string,
) {
	t.Helper()

	err := run()
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("%s error = %v, want %q", name, err, want)
	}
}

func runtimeTestPaths(t *testing.T) runtimePaths {
	t.Helper()

	root := t.TempDir()

	binDir := filepath.Join(root, "bin")

	err := os.MkdirAll(binDir, 0o755)
	if err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	paths := runtimePaths{
		RealGit:        "/usr/bin/git",
		InvocationCWD:  filepath.Join(root, "pkg"),
		Root:           root,
		HooksDir:       filepath.Join(root, ".git", "hooks"),
		BinDir:         binDir,
		RunBinary:      filepath.Join(binDir, "coding-ethos-run"),
		BundleRoot:     filepath.Join(root, "coding-ethos", "pre-commit"),
		EthosRoot:      filepath.Join(root, "coding-ethos"),
		GitHookRunner:  filepath.Join(binDir, "coding-ethos-hook-runner"),
		PolicyBundle:   filepath.Join(root, "policy-bundle.json"),
		PolicyMetadata: filepath.Join(root, "policy-metadata.json"),
	}
	for _, file := range []string{
		paths.RunBinary,
		paths.GitHookRunner,
		paths.PolicyBundle,
		paths.PolicyMetadata,
		filepath.Join(binDir, "coding-ethos-lint"),
		filepath.Join(binDir, "coding-ethos-mcp"),
	} {
		err := os.MkdirAll(filepath.Dir(file), 0o755)
		if err != nil {
			t.Fatalf("create parent for %s: %v", file, err)
		}

		writeExecutableFixture(t, file, "#!/usr/bin/env sh\nexit 0\n")
	}

	return paths
}

type stubRuntimeOps struct {
	calls        *[]string
	runLintCode  int
	execLintCode int
}

func (stub stubRuntimeOps) runLint(args ...string) int {
	*stub.calls = append(*stub.calls, "run-lint:"+strings.Join(args, " "))

	return stub.runLintCode
}

func (stub stubRuntimeOps) execLint(args ...string) {
	*stub.calls = append(*stub.calls, "exec-lint:"+strings.Join(args, " "))
	if stub.execLintCode != 0 {
		requestRuntimeExit(stub.execLintCode)
	}
}

func (stub stubRuntimeOps) runInternalTool(tool string, args ...string) {
	*stub.calls = append(*stub.calls, "direct-run:"+tool+" "+strings.Join(args, " "))
}

func (stub stubRuntimeOps) execInternalTool(tool string, args ...string) {
	*stub.calls = append(*stub.calls, "direct-exec:"+tool+" "+strings.Join(args, " "))
}

func (stub stubRuntimeOps) runTool(_ runtimePaths, tool string, args ...string) {
	*stub.calls = append(*stub.calls, "run:"+tool+" "+strings.Join(args, " "))
}

func (stub stubRuntimeOps) execTool(_ runtimePaths, tool string, args ...string) {
	*stub.calls = append(*stub.calls, "exec:"+tool+" "+strings.Join(args, " "))
}

func (stub stubRuntimeOps) execPath(path string, args ...string) {
	*stub.calls = append(*stub.calls, "execpath:"+path+" "+strings.Join(args, " "))
}

func (stub stubRuntimeOps) execExternal(path string, args ...string) {
	*stub.calls = append(*stub.calls, "external:"+path+" "+strings.Join(args, " "))
}
