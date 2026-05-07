// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

	root := t.TempDir()

	gitPath := filepath.Join(root, "git")

	writeExecutableFixture(t, gitPath, "#!/usr/bin/env sh\nprintf 'main\\n'\n")

	output, err := gitOutput(gitPath, root, "branch", "--show-current")
	if err != nil {
		t.Fatalf("gitOutput() returned error: %v", err)
	}

	if output != "main" {
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

func TestRunCISARIFWritesSARIFAndPassesManagedFlags(t *testing.T) {
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

	argsPath := filepath.Join(repo, "lint-args.txt")

	lintPath := filepath.Join(binDir, "coding-ethos-lint")

	writeExecutableFixture(
		t,
		lintPath,
		"#!/usr/bin/env sh\n"+
			"printf '%s\\n' \"$@\" > \"$LINT_ARGS_PATH\"\n"+
			"printf '{\"version\":\"2.1.0\",\"runs\":[]}'\n",
	)

	sarifPath := filepath.Join(repo, "reports", "coding-ethos.sarif")

	t.Setenv("CODING_ETHOS_FILES", "a.py,b.go,missing.py")
	t.Setenv("CODING_ETHOS_REPO_ROOT", repo)
	t.Setenv("CODING_ETHOS_SARIF_PATH", sarifPath)
	t.Setenv("CODING_ETHOS_SANDBOX_MODE", "required")
	t.Setenv("CODING_ETHOS_SARIF_CATEGORY", "policy")
	t.Setenv("LINT_ARGS_PATH", argsPath)

	err := runCISARIF(runtimePaths{
		BinDir:       binDir,
		PolicyBundle: filepath.Join(repo, "policy-bundle.json"),
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

	if !strings.Contains(string(payload), `"version":"2.1.0"`) {
		t.Fatalf("SARIF payload = %q", payload)
	}

	argsPayload, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read lint args: %v", err)
	}

	argsText := string(argsPayload)
	for _, want := range ciSARIFExpectedLintArgs(repo) {
		if !strings.Contains(argsText, want) {
			t.Fatalf("lint args missing %q:\n%s", want, argsText)
		}
	}

	if strings.Count(argsText, "--sarif\n") != 1 {
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
			want: "exec:coding-ethos-lint --bundle " + paths.PolicyBundle + " --scope staged",
		},
		{
			name: "policy command",
			args: []string{"policy", "validate"},
			want: "exec:coding-ethos-policy validate",
		},
		{
			name: "code intel",
			args: []string{"code-intel", "stats"},
			want: "exec:coding-ethos-code-intel stats --root " + paths.Root,
		},
		{
			name: "policy tool",
			args: []string{"policy-tool", "ruff", "check"},
			want: "exec:coding-ethos-lint --bundle " + paths.PolicyBundle +
				" --managed-capture-tool ruff --ethos-root " + paths.EthosRoot +
				" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
				" -- check",
		},
		{
			name: "agent hooks",
			args: []string{"agent-hooks", "sync"},
			want: "run:coding-ethos-toolchain install-git-shim --dest-dir " + paths.BinDir +
				" --real-git " + paths.RealGit + " --runner " + paths.RunBinary + "\n" +
				"run:coding-ethos-lint --install-shims --tools-bin-dir " + paths.BinDir +
				" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot + "\n" +
				"exec:coding-ethos-agent-hooks sync --hook-command " +
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
		"run:coding-ethos-policy validate-metadata --metadata " + paths.PolicyMetadata,
		"run:coding-ethos-lint --install-shims --tools-bin-dir " + paths.BinDir +
			" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot,
		"exec:coding-ethos-git-hook --bundle " + paths.PolicyBundle + " --runner " +
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
		"run:coding-ethos-toolchain install-git-hooks --hooks-dir " + paths.HooksDir +
			" --runner " + paths.RunBinary,
		"run:coding-ethos-agent-hooks sync --root " + paths.Root,
		"exec:coding-ethos-toolchain cutover-verify --action install --root " + paths.Root,
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
		"exec:coding-ethos-hook --bundle " + paths.PolicyBundle + " --json",
		"exec:coding-ethos-mcp --bundle " + paths.PolicyBundle,
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

	err := runPolicyTool(paths, nil)
	if err == nil || !strings.Contains(err.Error(), "requires a tool name") {
		t.Fatalf("runPolicyTool(nil) error = %v", err)
	}

	err = runGitHook(paths, nil)
	if err == nil || !strings.Contains(err.Error(), "requires a hook name") {
		t.Fatalf("runGitHook(nil) error = %v", err)
	}

	err = runGitHook(paths, []string{"post-merge"})
	if err == nil || !strings.Contains(err.Error(), "unknown git hook") {
		t.Fatalf("runGitHook unknown error = %v", err)
	}

	err = runCutover(paths, []string{"explode"})
	if err == nil || !strings.Contains(err.Error(), "unknown cutover action") {
		t.Fatalf("runCutover unknown error = %v", err)
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
	calls *[]string
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
