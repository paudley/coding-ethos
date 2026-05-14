// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
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

func TestParentWorkflowFlagsResolveParentInputs(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	repoEthos := filepath.Join(repo, "repo_ethos.yaml")
	repoConfig := filepath.Join(repo, "repo_config.yml")

	err := os.WriteFile(repoEthos, []byte("repo: {}\n"), 0o600)
	if err != nil {
		t.Fatalf("write repo ethos: %v", err)
	}

	err = os.WriteFile(repoConfig, []byte("hooks: {}\n"), 0o600)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	options, err := parseParentWorkflowFlags(
		runtimePaths{Root: "/fallback"},
		"parent-lint",
		[]string{"--repo", repo},
	)
	if err != nil {
		t.Fatalf("parse parent flags: %v", err)
	}

	if options.Repo != repo {
		t.Fatalf("repo = %q, want %q", options.Repo, repo)
	}

	if options.RepoEthos != repoEthos {
		t.Fatalf("repo ethos = %q, want %q", options.RepoEthos, repoEthos)
	}

	if options.RepoConfig != repoConfig {
		t.Fatalf("repo config = %q, want %q", options.RepoConfig, repoConfig)
	}

	if options.Scope != parentDefaultLintScope {
		t.Fatalf("scope = %q, want %q", options.Scope, parentDefaultLintScope)
	}
}

func TestParentCommandsRejectInvalidFlags(t *testing.T) {
	t.Parallel()

	paths := runtimePaths{Root: "/repo"}
	for name, runCommand := range map[string]func(runtimePaths, []string) error{
		"parent-install": runParentInstall,
		"parent-check":   runParentCheck,
		"parent-lint":    runParentLint,
	} {
		err := runCommand(paths, []string{"--missing-flag"})
		if err == nil || !strings.Contains(err.Error(), "parse "+name+" flags") {
			t.Fatalf("%s error = %v", name, err)
		}
	}
}

func TestParentLintArgsUseParentRepoAndTOONScope(t *testing.T) {
	t.Parallel()

	args := parentLintArgs(runtimePaths{
		PolicyBundle:  "/ethos/build/policy/policy-bundle.json",
		EthosRoot:     "/ethos",
		InvocationCWD: "/parent/site",
	}, parentWorkflowOptions{
		Repo:       "/parent",
		RepoEthos:  "/parent/repo_ethos.yaml",
		RepoConfig: "/parent/repo_config.yml",
		Scope:      "changed",
	})

	for _, want := range []string{
		"--bundle",
		"/ethos/build/policy/policy-bundle.json",
		"--ethos-root",
		"/ethos",
		"--consumer-root",
		"/parent",
		"--invocation-cwd",
		"/parent/site",
		"--cwd",
		"/parent",
		"--scope",
		"changed",
		"--repo-ethos",
		"/parent/repo_ethos.yaml",
		"--repo-config",
		"/parent/repo_config.yml",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("parentLintArgs() missing %q: %#v", want, args)
		}
	}
}

func TestParentStepStatusFailsOnAnyFailedStep(t *testing.T) {
	t.Parallel()

	steps := []parentWorkflowStep{
		{Name: "tool_configs", Status: parentStepPass},
		{Name: "agent_hooks", Status: parentStepFail, Detail: "missing setting"},
	}

	if got := parentStepStatus(steps); got != parentStepFail {
		t.Fatalf("parentStepStatus() = %q, want fail", got)
	}
}

func TestParentStepHelpersReportPassAndFail(t *testing.T) {
	t.Parallel()

	passed := runParentStep("agent_hooks", func() error { return nil })
	if passed.Status != parentStepPass || passed.Detail != "" {
		t.Fatalf("passed step = %#v", passed)
	}

	failed := runParentStep("agent_hooks", func() error {
		return apperror.StaticError("missing setting")
	})
	if failed.Status != parentStepFail || failed.Detail != "missing setting" {
		t.Fatalf("failed step = %#v", failed)
	}

	if got := parentStepStatus([]parentWorkflowStep{passed}); got != parentStepPass {
		t.Fatalf("parentStepStatus(pass) = %q", got)
	}

	exitForFailedParentSteps([]parentWorkflowStep{passed})
}

func TestParentGoToolsCheckFailsWhenBinaryIsStale(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	sourceRoot := parentGoToolsSourceFixture(t, paths.EthosRoot)
	paths.ToolsSource = sourceRoot

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)

	touchParentGoTools(t, paths, oldTime)
	writeParentGoSource(t, sourceRoot, "internal/runtime/source.go", newTime)

	err := checkParentGoTools(paths, parentWorkflowOptions{Repo: paths.Root})
	if err == nil ||
		!strings.Contains(err.Error(), "parent Go tools are stale") ||
		!strings.Contains(err.Error(), "coding-ethos-run(stale)") {
		t.Fatalf("checkParentGoTools error = %v", err)
	}
}

func TestParentGoToolsCheckPassesWhenBinariesAreCurrent(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	sourceRoot := parentGoToolsSourceFixture(t, paths.EthosRoot)
	paths.ToolsSource = sourceRoot

	oldTime := time.Now().Add(-2 * time.Hour)
	newTime := time.Now().Add(-1 * time.Hour)

	writeParentGoSource(t, sourceRoot, "internal/runtime/source.go", oldTime)
	touchParentGoTools(t, paths, newTime)

	err := checkParentGoTools(paths, parentWorkflowOptions{Repo: paths.Root})
	if err != nil {
		t.Fatalf("checkParentGoTools: %v", err)
	}
}

func TestPrintParentWorkflowReportEmitsTOON(t *testing.T) { //nolint:paralleltest
	output := captureRuntimeStdout(t, func() {
		printParentWorkflowReport(
			"parent-check",
			parentStepFail,
			"/repo",
			[]parentWorkflowStep{
				{Name: "tool_configs", Status: parentStepPass},
				{Name: "agent_hooks", Status: parentStepFail, Detail: "missing hook"},
			},
		)
	})

	for _, want := range []string{
		"format: toon",
		"tool: parent-check",
		"status: fail",
		"repo: /repo",
		"steps[2]{name,status,detail}:",
		"agent_hooks,fail,missing hook",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("parent report missing %q:\n%s", want, output)
		}
	}
}

func TestParentDriftErrorSkipsEmptyDrift(t *testing.T) {
	t.Parallel()

	err := parentDriftError(
		runtimePaths{},
		parentWorkflowOptions{Repo: "/repo"},
		"tool_configs",
		nil,
	)
	if err != nil {
		t.Fatalf("parentDriftError(empty) = %v, want nil", err)
	}
}

func TestParentDriftErrorIncludesRepairCommandAndLocation(t *testing.T) {
	t.Parallel()

	err := parentDriftError(
		runtimePaths{RealGit: "/missing/git", EthosRoot: "/repo/coding-ethos"},
		parentWorkflowOptions{Repo: "/repo"},
		"gemini_prompts",
		[]string{"/repo/.code-ethos/gemini/prompt-pack.json"},
	)
	if err == nil {
		t.Fatal("expected drift error")
	}

	message := err.Error()
	for _, want := range []string{
		"gemini_prompts out of sync in parent checkout",
		"coding-ethos/bin/coding-ethos-run parent-install --repo /repo",
		".code-ethos/gemini/prompt-pack.json(working_tree)",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("drift error missing %q: %s", want, message)
		}
	}
}

func TestParentCheckoutLocationLabelsSameRepoAsCodingEthos(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	got := parentCheckoutLocation(
		runtimePaths{EthosRoot: repo},
		parentWorkflowOptions{Repo: repo},
	)
	if got != "coding-ethos" {
		t.Fatalf("checkout location = %q", got)
	}
}

func TestParentPathHelpersClassifyGitStates(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	git := fakeStatusGit(t, " M changed\n")
	if got := gitPathState(git, repo, "changed"); got != parentWorkingTreeState {
		t.Fatalf("working tree state = %q", got)
	}

	git = fakeStatusGit(t, "M  staged\n")
	if got := gitPathState(git, repo, "staged"); got != "index" {
		t.Fatalf("index state = %q", got)
	}

	git = fakeStatusGit(t, "MM both\n")
	if got := gitPathState(git, repo, "both"); got != "index+working_tree" {
		t.Fatalf("combined state = %q", got)
	}

	git = fakeStatusGit(t, "?? new\n")
	if got := gitPathState(git, repo, "new"); got != "untracked" {
		t.Fatalf("untracked state = %q", got)
	}

	if got := relativeToRepo("/repo", "/repo/sub/file.txt"); got != "sub/file.txt" {
		t.Fatalf("relativeToRepo() = %q", got)
	}

	if !sameCleanPath("/repo/.", "/repo") {
		t.Fatal("sameCleanPath() rejected equivalent paths")
	}

	if got := shellquote.Arg("plain"); got != "plain" {
		t.Fatalf("plain shell quote = %q", got)
	}

	if got := shellquote.Arg("has space"); got != "'has space'" {
		t.Fatalf("spaced shell quote = %q", got)
	}
}

func TestParentOptionHelpersResolveCandidates(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	ethos := filepath.Join(repo, "coding-ethos")

	err := os.MkdirAll(ethos, 0o700)
	if err != nil {
		t.Fatalf("create ethos root: %v", err)
	}

	repoEthos := filepath.Join(repo, "repo_ethos.yml")
	repoConfig := filepath.Join(repo, "repo_config.yaml")

	err = os.WriteFile(repoEthos, []byte("repo: {}\n"), 0o600)
	if err != nil {
		t.Fatalf("write repo ethos: %v", err)
	}

	err = os.WriteFile(repoConfig, []byte("profiles: []\n"), 0o600)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	paths := runtimePaths{EthosRoot: ethos}
	options := parentWorkflowOptions{
		Repo:       repo,
		RepoEthos:  repoEthos,
		RepoConfig: repoConfig,
	}

	gemini := parentGeminiOptions(paths, options)
	if gemini.RepoRoot != repo ||
		gemini.RepoEthos != repoEthos ||
		gemini.RepoConfig != repoConfig {
		t.Fatalf("parentGeminiOptions() = %#v", gemini)
	}

	skills := parentAgentSkillOptions(paths, options)
	if skills.RepoRoot != repo || skills.RepoEthos != repoEthos {
		t.Fatalf("parentAgentSkillOptions() = %#v", skills)
	}
}

func TestParentPathHelpersResolveCandidates(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	repoEthos := filepath.Join(repo, "repo_ethos.yml")
	repoConfig := filepath.Join(repo, "repo_config.yaml")

	err := os.WriteFile(repoEthos, []byte("repo: {}\n"), 0o600)
	if err != nil {
		t.Fatalf("write repo ethos: %v", err)
	}

	err = os.WriteFile(repoConfig, []byte("profiles: []\n"), 0o600)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	got, err := firstExistingPath("", parentRepoEthosCandidates(repo))
	if err != nil {
		t.Fatalf("first repo ethos: %v", err)
	}

	if got != repoEthos {
		t.Fatalf("first repo ethos = %q, want %q", got, repoEthos)
	}

	got, err = firstExistingPath(
		repoConfig,
		parentRepoConfigCandidates(repo),
	)
	if err != nil {
		t.Fatalf("explicit repo config: %v", err)
	}

	if got != repoConfig {
		t.Fatalf("explicit repo config = %q", got)
	}

	if got := firstNonBlank("", " ", "changed"); got != "changed" {
		t.Fatalf("firstNonBlank() = %q", got)
	}
}

func TestFirstExistingPathRejectsMissingExplicitPath(t *testing.T) {
	t.Parallel()

	_, err := firstExistingPath(filepath.Join(t.TempDir(), "missing.yml"), nil)
	if err == nil || !strings.Contains(err.Error(), "no such file") {
		t.Fatalf("missing explicit path error = %v", err)
	}
}

func TestForceTOONOutputRestoresPreviousFormat(t *testing.T) {
	t.Setenv(hookoutput.FormatEnv, hookoutput.FormatJSON)

	restore := forceTOONOutput()

	if got := os.Getenv(hookoutput.FormatEnv); got != hookoutput.FormatTOON {
		t.Fatalf("forced hook output = %q", got)
	}

	restore()

	if got := os.Getenv(hookoutput.FormatEnv); got != hookoutput.FormatJSON {
		t.Fatalf("restored hook output = %q", got)
	}
}

func TestGitlabChangedFilesUsesMergeRequestTarget(t *testing.T) {
	repo := t.TempDir()
	writeChangedGoFile(t, repo)

	t.Setenv("CI_MERGE_REQUEST_TARGET_BRANCH_SHA", "base123")
	git := fakeDiffGit(t, "changed.go\nmissing.go\n")

	files, err := gitlabChangedFiles(git, repo)
	if err != nil {
		t.Fatalf("gitlabChangedFiles() returned error: %v", err)
	}

	if !slices.Equal(files, []string{"changed.go"}) {
		t.Fatalf("gitlab changed files = %#v", files)
	}
}

func TestGitlabChangedFilesUsesPushBeforeSHA(t *testing.T) {
	repo := t.TempDir()
	writeChangedGoFile(t, repo)

	t.Setenv("CI_COMMIT_BEFORE_SHA", "before123")
	t.Setenv("CI_COMMIT_SHA", "after123")
	git := fakeDiffGit(t, "changed.go\n")

	files, err := gitlabChangedFiles(git, repo)
	if err != nil {
		t.Fatalf("gitlabChangedFiles() returned error: %v", err)
	}

	if !slices.Equal(files, []string{"changed.go"}) {
		t.Fatalf("gitlab changed files = %#v", files)
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

//nolint:paralleltest // Serializes process-global runtime environment state.
func TestRuntimePathResolutionUsesGitWhenAvailable(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	repo := filepath.Join(t.TempDir(), "repo")
	hooks := filepath.Join(repo, ".git", "hooks")
	fakeGit := fakeRuntimeGit(t, repo, hooks, "")

	root, localRoot := resolveRuntimeRoot(fakeGit, "/cwd/repo")
	if root != repo || localRoot != repo {
		t.Fatalf("git root = (%q, %q), want %q", root, localRoot, repo)
	}

	if got := resolveRuntimeHooksDir(fakeGit, repo); got != hooks {
		t.Fatalf("git hooks dir = %q, want %q", got, hooks)
	}
}

//nolint:paralleltest // Serializes process-global runtime environment state.
func TestRuntimePathResolutionPrefersSuperprojectForSubmodule(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	parent := filepath.Join(t.TempDir(), "parent")
	repo := filepath.Join(parent, "coding-ethos")
	hooks := filepath.Join(parent, ".git", "hooks")

	err := os.MkdirAll(repo, 0o700)
	if err != nil {
		t.Fatalf("create submodule fixture: %v", err)
	}

	fakeGit := fakeRuntimeGit(t, repo, hooks, parent)

	root, localRoot := resolveRuntimeRoot(fakeGit, repo)
	if root != parent {
		t.Fatalf("consumer root = %q, want parent %q", root, parent)
	}

	if localRoot != repo {
		t.Fatalf("local root = %q, want submodule %q", localRoot, repo)
	}
}

func TestRuntimePathsExportManagedEnvironment(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	restoreEnv := captureRuntimeEnvForTest(
		"INVOCATION_CWD",
		"CODE_ETHOS_PRECOMMIT_ROOT",
		"CODE_ETHOS_CONSUMER_ROOT",
		"CODE_ETHOS_LOCAL_ROOT",
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

	if got := os.Getenv("CODE_ETHOS_LOCAL_ROOT"); got != "/repo" {
		t.Fatalf("exported local root = %q", got)
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

func TestRuntimeFileBinaryAndRunToolHappyPath(t *testing.T) { //nolint:paralleltest
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

func fakeRuntimeGit(t *testing.T, repo, hooks, superproject string) string {
	t.Helper()

	superprojectCase := "  \"rev-parse --show-superproject-working-tree\") exit 1 ;;\n"
	if superproject != "" {
		superprojectCase = strings.Join([]string{
			"  \"rev-parse --show-superproject-working-tree\") printf '%s\\n' ",
			shellQuoteForRuntimeTest(superproject),
			"; exit 0 ;;\n",
		}, "")
	}

	path := filepath.Join(t.TempDir(), "git")
	body := "#!/usr/bin/env sh\n" +
		"case \"$*\" in\n" +
		"  \"rev-parse --show-toplevel\") printf '%s\\n' " +
		shellQuoteForRuntimeTest(repo) + "; exit 0 ;;\n" +
		superprojectCase +
		"  \"rev-parse --path-format=absolute --git-path hooks\") printf '%s\\n' " +
		shellQuoteForRuntimeTest(hooks) + "; exit 0 ;;\n" +
		"esac\n" +
		"exit 1\n"
	writeExecutableFixture(t, path, body)

	return path
}

func captureRuntimeStdout(t *testing.T, action func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer := runtimeStdoutPipe(t)

	os.Stdout = writer

	t.Cleanup(func() {
		os.Stdout = originalStdout
	})

	action()

	closeErr := writer.Close()
	if closeErr != nil {
		t.Fatalf("close stdout writer: %v", closeErr)
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read stdout pipe: %v", err)
	}

	return string(output)
}

func writeChangedGoFile(t *testing.T, repo string) {
	t.Helper()

	err := os.WriteFile(filepath.Join(repo, "changed.go"), []byte("package main\n"), 0o600)
	if err != nil {
		t.Fatalf("write changed file: %v", err)
	}
}

func runtimeStdoutPipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	return reader, writer
}

func fakeStatusGit(t *testing.T, status string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "git")
	body := "#!/usr/bin/env sh\n" +
		"case \"$*\" in\n" +
		"  \"status --porcelain -- \"*) printf '%s' " +
		shellQuoteForRuntimeTest(status) + "; exit 0 ;;\n" +
		"esac\n" +
		"printf 'unexpected git args: %s\\n' \"$*\" >&2\n" +
		"exit 2\n"
	writeExecutableFixture(t, path, body)

	return path
}

func fakeDiffGit(t *testing.T, output string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "git")
	body := "#!/usr/bin/env sh\n" +
		"case \"$*\" in\n" +
		"  \"diff --name-only \"*) printf '%s' " +
		shellQuoteForRuntimeTest(output) + "; exit 0 ;;\n" +
		"  *\" rev-parse --verify \"*) exit 0 ;;\n" +
		"esac\n" +
		"printf 'unexpected git args: %s\\n' \"$*\" >&2\n" +
		"exit 2\n"
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
			name: "policy formatter group",
			args: []string{"policy-tool-group", "formatters"},
			want: "run-lint:--bundle " + paths.PolicyBundle +
				" --managed-capture-tool ruff-format --ethos-root " + paths.EthosRoot +
				" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
				" -- format coding_ethos tests\n" +
				"run-lint:--bundle " + paths.PolicyBundle +
				" --managed-capture-tool golangci-lint-format --ethos-root " +
				paths.EthosRoot + " --consumer-root " + paths.Root +
				" --invocation-cwd " + paths.InvocationCWD + " --",
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

func TestPolicyLinterGroupLetsGolangciChooseNestedGoModule(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	code := runRuntime(paths, []string{"policy-tool-group", "linters"})
	if code != 0 {
		t.Fatalf("runRuntime exit = %d, want 0", code)
	}

	want := "run-lint:--bundle " + paths.PolicyBundle +
		" --managed-capture-tool ruff --ethos-root " + paths.EthosRoot +
		" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
		" -- check coding_ethos tests\n" +
		"run-lint:--bundle " + paths.PolicyBundle +
		" --managed-capture-tool golangci-lint --ethos-root " +
		paths.EthosRoot + " --consumer-root " + paths.Root +
		" --invocation-cwd " + paths.InvocationCWD + " --"

	got := strings.Join(calls, "\n")
	if got != want {
		t.Fatalf("policy-tool-group linters calls = %q, want %q", got, want)
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
		"agent-hook:--bundle " + paths.PolicyBundle + " --json",
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

func parentGoToolsSourceFixture(t *testing.T, ethosRoot string) string {
	t.Helper()

	sourceRoot := filepath.Join(ethosRoot, "go")
	oldTime := time.Now().Add(-3 * time.Hour)

	for _, tool := range parentGoToolCommands() {
		mainPath := filepath.Join(sourceRoot, "cmd", tool, "main.go")

		err := os.MkdirAll(filepath.Dir(mainPath), 0o755)
		if err != nil {
			t.Fatalf("create source dir: %v", err)
		}

		err = os.WriteFile(mainPath, []byte("package main\n"), 0o600)
		if err != nil {
			t.Fatalf("write source: %v", err)
		}

		err = os.Chtimes(mainPath, oldTime, oldTime)
		if err != nil {
			t.Fatalf("touch source: %v", err)
		}
	}

	writeParentGoSource(t, sourceRoot, "go.mod", oldTime)

	return sourceRoot
}

func writeParentGoSource(
	t *testing.T,
	sourceRoot string,
	relative string,
	modTime time.Time,
) {
	t.Helper()

	path := filepath.Join(sourceRoot, relative)

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("create source parent: %v", err)
	}

	err = os.WriteFile(path, []byte("package runtime\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	err = os.Chtimes(path, modTime, modTime)
	if err != nil {
		t.Fatalf("touch source file: %v", err)
	}
}

func touchParentGoTools(t *testing.T, paths runtimePaths, modTime time.Time) {
	t.Helper()

	for _, tool := range parentGoToolCommands() {
		toolPath := filepath.Join(paths.BinDir, tool)
		writeExecutableFixture(t, toolPath, "#!/usr/bin/env sh\nexit 0\n")

		err := os.Chtimes(toolPath, modTime, modTime)
		if err != nil {
			t.Fatalf("touch tool %s: %v", tool, err)
		}
	}
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

func (stub stubRuntimeOps) execAgentHook(args ...string) {
	*stub.calls = append(*stub.calls, "agent-hook:"+strings.Join(args, " "))
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
