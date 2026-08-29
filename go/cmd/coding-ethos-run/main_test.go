// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
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

func TestRuntimePolicyRequiresKnownAction(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"remove", "--repo", "/repo"}} {
		err := runRuntimePolicy(runtimePaths{}, args)
		if err == nil ||
			!strings.Contains(err.Error(), "runtime-policy requires sync or check") {
			t.Fatalf("runRuntimePolicy(%#v) error = %v", args, err)
		}
	}
}

func TestDebugRunnerArgsStripInternalFlag(t *testing.T) {
	t.Parallel()

	args, debug := debugRunnerArgs([]string{
		"git-hook",
		debuglog.Flag,
		"pre-commit",
		debuglog.Flag,
	})

	want := []string{"git-hook", "pre-commit"}
	if !debug || !slices.Equal(args, want) {
		t.Fatalf("debugRunnerArgs() = %#v, %v; want %#v, true", args, debug, want)
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

func TestPolicyToolLintArgsDoNotExposeSandboxMode(t *testing.T) {
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
		"--",
		"check",
		"pkg",
	} {
		if !slices.Contains(args, want) {
			t.Fatalf("policyToolLintArgs() missing %q: %#v", want, args)
		}
	}

	if slices.Contains(args, "--sandbox-mode") {
		t.Fatalf("sandbox mode should not be selected by runner args: %#v", args)
	}
}

func TestCodeIntelArgsInsertRootAfterSubcommand(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs(
		"/repo",
		"/repo",
		[]string{"stats", "--db", "/tmp/code-intel.duckdb"},
	)

	want := []string{"stats", "--root", "/repo", "--db", "/tmp/code-intel.duckdb"}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestCodeIntelArgsKeepExplicitRoot(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs("/repo", "/repo", []string{"stats", "--root", "/other"})

	want := []string{"stats", "--root", "/other"}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestCodeIntelArgsBindPrivateStateDatabase(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs(
		"/repo",
		"/private/state",
		[]string{"index-code", "--state-root", "/private/state", "."},
	)

	want := []string{
		"index-code",
		"--root",
		"/repo",
		"--db",
		"/private/state/.coding-ethos/code-intel.duckdb",
		".",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestCodeIntelArgsBindTokenEconomyStateRootAfterNestedCommand(t *testing.T) {
	t.Parallel()

	args := codeIntelArgs(
		"/repo",
		"/private/state",
		[]string{"token-economy", "report", "--historical", "--output-prefix", "/tmp/report"},
	)

	want := []string{
		"token-economy",
		"report",
		"--root",
		"/repo",
		"--state-root",
		"/private/state",
		"--historical",
		"--output-prefix",
		"/tmp/report",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("codeIntelArgs() = %#v, want %#v", args, want)
	}
}

func TestCodeIntelArgsDoNotRewriteExplicitBenchmarkContract(t *testing.T) {
	t.Parallel()

	input := []string{
		"token-economy",
		"benchmark",
		"run",
		"--manifest",
		"/private/benchmark.yaml",
		"--state-root",
		"/private/evidence",
		"--approved-max-runs",
		"3",
	}
	args := codeIntelArgs("/repo", "/ambient/state", input)
	if !slices.Equal(args, input) {
		t.Fatalf("codeIntelArgs() = %#v, want explicit contract %#v", args, input)
	}
}

func TestCodeIntelArgsDoNotAddReportFlagsToLedger(t *testing.T) {
	t.Parallel()

	input := []string{
		"token-economy",
		"ledger",
		"--provider",
		"codex",
		"--path",
		"/private/rollout.jsonl",
	}
	args := codeIntelArgs("/repo", "/ambient/state", input)
	if !slices.Equal(args, input) {
		t.Fatalf("codeIntelArgs() = %#v, want ledger contract %#v", args, input)
	}
}

func TestCodeIntelArgsKeepSourceV2CommandsFreeOfDuckDBFlags(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"sync", "status"} {
		got := codeIntelArgs("/repo", "/private/state", []string{command})
		want := []string{command, "--root", "/repo"}
		if !slices.Equal(got, want) {
			t.Fatalf("codeIntelArgs(%q) = %#v, want %#v", command, got, want)
		}
	}
}

func TestAgentHooksArgsInjectCapabilityEthosRootWithoutHookCommand(t *testing.T) {
	t.Parallel()

	paths := runtimePaths{
		EthosRoot: "/opt/coding-ethos",
		RunBinary: "/opt/coding-ethos/bin/coding-ethos-run",
	}

	got := agentHooksArgs(paths, []string{"capabilities"})
	want := []string{
		"capabilities",
		"--ethos-root",
		"/opt/coding-ethos",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("agentHooksArgs() = %#v, want %#v", got, want)
	}
}

func TestFlagValueReadsPrivateOverlayRepoRoot(t *testing.T) {
	t.Parallel()

	args := []string{
		"sync",
		"--root",
		"/private/overlay",
		"--repo-root=/repo",
	}
	if got := rootFlagValue(args, "/fallback"); got != "/private/overlay" {
		t.Fatalf("rootFlagValue() = %q", got)
	}
	if got := flagValue(args, "--repo-root", "/fallback"); got != "/repo" {
		t.Fatalf("repo-root flagValue() = %q", got)
	}
}

func TestWithCommandRootsSeparatesRepositoryAndState(t *testing.T) {
	t.Parallel()

	paths := runtimePaths{Root: "/original", StateRoot: "/original"}
	got := paths.withCommandRoots([]string{
		"mcp",
		"--repo-root",
		"/repo",
		"--state-root=/private/state",
	})

	if got.Root != "/repo" || got.StateRoot != "/private/state" {
		t.Fatalf("withCommandRoots() = %#v", got)
	}
}

func TestWithCommandRootsUsesCommandSpecificRepositoryFlags(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{
			name: "agent hooks",
			args: []string{
				"agent-hooks",
				"sync",
				"--repo-root",
				"/repo",
				"--state-root",
				"/private/state",
			},
		},
		{
			name: "code intel",
			args: []string{
				"code-intel",
				"index",
				"--root",
				"/repo",
				"--state-root",
				"/private/state",
			},
		},
		{
			name: "runtime policy",
			args: []string{
				"runtime-policy",
				"sync",
				"--repo",
				"/repo",
				"--state-root",
				"/private/state",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			paths := runtimePaths{Root: "/original", StateRoot: "/original"}
			got := paths.withCommandRoots(testCase.args)
			if got.Root != "/repo" || got.StateRoot != "/private/state" {
				t.Fatalf("withCommandRoots() = %#v", got)
			}
		})
	}
}

func TestCapabilitiesDiscoveryDoesNotWriteRuntimeLog(t *testing.T) {
	t.Parallel()

	if shouldLogRuntimeCommand([]string{"agent-hooks", "capabilities"}) {
		t.Fatal("capabilities discovery should be read-only")
	}
	if !shouldLogRuntimeCommand([]string{"agent-hooks", "doctor"}) {
		t.Fatal("doctor should retain runtime logging")
	}
}

func TestAgentHooksStateDefaultsToPrivateSettingsRoot(t *testing.T) {
	t.Parallel()

	paths := runtimePaths{Root: "/original", StateRoot: "/original"}
	got := paths.withCommandRoots([]string{
		"agent-hooks",
		"sync",
		"--root",
		"/private/settings",
		"--repo-root",
		"/repo",
	})
	if got.Root != "/repo" || got.StateRoot != "/private/settings" {
		t.Fatalf("withCommandRoots() = %#v", got)
	}
}

func TestRunAgentHooksCommandExportsRepositoryRootBeforeSettingsRoot(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	restoreEnv := captureRuntimeEnvForTest(
		"CODE_ETHOS_CONSUMER_ROOT",
		envStateRoot,
		"CODING_ETHOS_GIT_SHIM_DIR",
	)
	t.Cleanup(restoreEnv)

	paths := runtimeTestPaths(t)
	var calls []string
	paths.Executor = stubRuntimeOps{calls: &calls}

	err := run(paths, []string{
		"agent-hooks",
		"sync",
		"--root",
		"/private/settings",
		"--repo-root",
		"/repo",
	})
	if err != nil {
		t.Fatalf("run agent-hooks: %v", err)
	}

	if got := os.Getenv("CODE_ETHOS_CONSUMER_ROOT"); got != "/repo" {
		t.Fatalf("consumer root = %q, want /repo", got)
	}
	if got := os.Getenv(envStateRoot); got != "/private/settings" {
		t.Fatalf("%s = %q, want /private/settings", envStateRoot, got)
	}
}

func TestOutputArgsInsertRootAfterSubcommand(t *testing.T) {
	t.Parallel()

	args := outputArgs("/repo", []string{"report", "--format", "json"})

	want := []string{"report", "--root", "/repo", "--format", "json"}
	if !slices.Equal(args, want) {
		t.Fatalf("outputArgs() = %#v, want %#v", args, want)
	}
}

func TestOutputArgsKeepExplicitRoot(t *testing.T) {
	t.Parallel()

	args := outputArgs("/repo", []string{"report", "--root", "/other"})

	want := []string{"report", "--root", "/other"}
	if !slices.Equal(args, want) {
		t.Fatalf("outputArgs() = %#v, want %#v", args, want)
	}
}

func TestWebGuidanceArgsInsertRootAfterSubcommand(t *testing.T) {
	t.Parallel()

	args := webGuidanceArgs("/repo", []string{"search", "navigation drawer"})

	want := []string{"search", "--root", "/repo", "navigation drawer"}
	if !slices.Equal(args, want) {
		t.Fatalf("webGuidanceArgs() = %#v, want %#v", args, want)
	}
}

func TestWebGuidanceArgsKeepExplicitRoot(t *testing.T) {
	t.Parallel()

	args := webGuidanceArgs("/repo", []string{"list", "--root", "/other"})

	want := []string{"list", "--root", "/other"}
	if !slices.Equal(args, want) {
		t.Fatalf("webGuidanceArgs() = %#v, want %#v", args, want)
	}
}

func TestRuntimePathSetDerivesManagedPaths(t *testing.T) {
	t.Parallel()

	inputs := runtimePathInputs{
		RealGit:       "/usr/bin/git",
		InvocationCWD: "/repo/pkg",
		LocalRoot:     "/repo",
		GitDir:        "/repo/.git",
		GitCommonDir:  "/repo/.git",
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

	if paths.GitDir != "/repo/.git" {
		t.Fatalf("git dir = %q", paths.GitDir)
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
		"/parent/.git/coding-ethos-hooks/policy/policy-bundle.json",
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

func TestRuntimePolicyBundleUsesPrivateStateRoot(t *testing.T) {
	t.Parallel()

	got := parentPolicyBundleDir(
		runtimePaths{},
		parentWorkflowOptions{
			Repo:      "/repo",
			StateRoot: "/private/state",
		},
	)
	want := "/private/state/.coding-ethos/policy"
	if got != want {
		t.Fatalf("parentPolicyBundleDir() = %q, want %q", got, want)
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

func TestSyncParentPolicyBundleUsesParentRepoConfig(t *testing.T) {
	t.Parallel()

	sourceRoot := findRunnerRepoRoot(t)
	parentRepo := t.TempDir()
	runRunnerTestGit(t, parentRepo, "init", "--initial-branch", "main")

	repoConfig := filepath.Join(parentRepo, "repo_config.yaml")

	err := os.WriteFile(repoConfig, []byte(`
filesystem:
  protected_branch_write:
    branches:
      - main
`), 0o600)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	paths := runtimeTestPaths(t)
	paths.RealGit = "git"
	paths.EthosRoot = sourceRoot
	paths.BinDir = filepath.Join(t.TempDir(), "bin")

	err = os.MkdirAll(paths.BinDir, 0o755)
	if err != nil {
		t.Fatalf("create bin dir: %v", err)
	}

	writeExecutableFixture(
		t,
		filepath.Join(paths.BinDir, "coding-ethos-policy"),
		"#!/usr/bin/env sh\nexit 0\n",
	)

	options := parentWorkflowOptions{
		Repo:       parentRepo,
		RepoEthos:  filepath.Join(sourceRoot, "repo_ethos.yml"),
		RepoConfig: repoConfig,
	}

	err = syncParentPolicyBundle(paths, options)
	if err != nil {
		t.Fatalf("sync parent policy bundle: %v", err)
	}

	parentBundle := parentPolicyBundlePath(paths, options)
	assertProtectedBranchWorkPoliciesPresent(t, parentBundle)

	err = checkParentPolicyBundle(paths, options)
	if err != nil {
		t.Fatalf("check synced parent policy bundle: %v", err)
	}

	bundle, err := os.ReadFile(parentBundle)
	if err != nil {
		t.Fatalf("read policy bundle: %v", err)
	}

	tampered := strings.Replace(
		string(bundle),
		`"mode": "block"`,
		`"mode": "warn"`,
		1,
	)

	err = os.WriteFile(parentBundle, []byte(tampered), 0o600)
	if err != nil {
		t.Fatalf("write tampered policy bundle: %v", err)
	}

	err = checkParentPolicyBundle(paths, options)
	if !errors.Is(err, errParentArtifactDrift) {
		t.Fatalf("check tampered parent policy bundle error = %v", err)
	}
}

func TestParentRuntimeBundleRejectsRemovedProtectedBranchWorkConfig(t *testing.T) {
	t.Parallel()

	sourceRoot := findRunnerRepoRoot(t)
	parentRepo := t.TempDir()
	runRunnerTestGit(t, parentRepo, "init", "--initial-branch", "master")

	repoConfig := filepath.Join(parentRepo, "repo_config.yaml")
	err := os.WriteFile(repoConfig, []byte(`
repo:
  protected_branch_work:
    enabled: false
`), 0o600)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	paths := runtimePathSet(runtimePathInputs{
		RealGit:       "git",
		InvocationCWD: parentRepo,
		LocalRoot:     parentRepo,
		GitCommonDir:  filepath.Join(parentRepo, ".git"),
		Root:          parentRepo,
		HooksDir:      filepath.Join(parentRepo, ".git", "hooks"),
		BinDir:        filepath.Join(t.TempDir(), "bin"),
		RunBinary:     filepath.Join(t.TempDir(), "bin", "coding-ethos-run"),
		BundleRoot:    filepath.Join(sourceRoot, "pre-commit"),
		EthosRoot:     sourceRoot,
		ToolchainDir:  filepath.Join(sourceRoot, "build", "toolchain"),
	})

	err = syncParentPolicyBundle(paths, parentWorkflowOptions{
		Repo:       parentRepo,
		RepoEthos:  filepath.Join(sourceRoot, "repo_ethos.yml"),
		RepoConfig: repoConfig,
	})
	if err == nil || !strings.Contains(err.Error(), "repo.protected_branch_work.enabled") {
		t.Fatalf("sync parent policy bundle error = %v", err)
	}
}

func TestRebuildParentGoToolsBuildsRepoLocalBinaries(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	sourceRoot := parentBuildableGoToolsSourceFixture(t, paths.EthosRoot)
	paths.ToolsSource = sourceRoot

	err := rebuildParentGoTools(paths)
	if err != nil {
		t.Fatalf("rebuildParentGoTools: %v", err)
	}

	tools, err := parentGoToolCommands(paths)
	if err != nil {
		t.Fatalf("parentGoToolCommands: %v", err)
	}

	for _, tool := range tools {
		toolPath := filepath.Join(paths.BinDir, tool)

		info, err := os.Stat(toolPath)
		if err != nil {
			t.Fatalf("stat rebuilt tool %s: %v", tool, err)
		}

		if info.IsDir() || info.Mode()&0o111 == 0 {
			t.Fatalf("rebuilt tool %s not executable: %#v", tool, info.Mode())
		}
	}
}

func TestBuildParentGoToolKeepsExistingBinaryWhenBuildFails(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	sourceRoot := parentBuildableGoToolsSourceFixture(t, paths.EthosRoot)
	paths.ToolsSource = sourceRoot

	tool := "coding-ethos-run"
	toolPath := filepath.Join(paths.BinDir, tool)
	original := "#!/usr/bin/env sh\necho original\n"
	writeExecutableFixture(t, toolPath, original)

	err := os.WriteFile(
		filepath.Join(sourceRoot, "cmd", tool, "main.go"),
		[]byte("package main\n\nfunc main(\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write invalid main: %v", err)
	}

	err = buildParentGoTool(paths, tool)
	if err == nil {
		t.Fatalf("buildParentGoTool succeeded with invalid source")
	}

	current, err := os.ReadFile(toolPath)
	if err != nil {
		t.Fatalf("read existing tool: %v", err)
	}

	if string(current) != original {
		t.Fatalf("existing tool changed after failed build: %q", string(current))
	}
}

func TestRequireParentGoToolsSourceRejectsFile(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	paths.ToolsSource = filepath.Join(paths.Root, "go-source-file")

	err := os.WriteFile(paths.ToolsSource, []byte("not a dir\n"), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	err = requireParentGoToolsSource(paths)
	if !errors.Is(err, errParentPathIsNotDirectory) {
		t.Fatalf("requireParentGoToolsSource error = %v", err)
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
		[]string{"/repo/.coding-ethos/gemini/prompt-pack.json"},
	)
	if err == nil {
		t.Fatal("expected drift error")
	}

	message := err.Error()
	for _, want := range []string{
		"gemini_prompts out of sync in parent checkout",
		"coding-ethos/bin/coding-ethos-run parent-install --repo /repo",
		".coding-ethos/gemini/prompt-pack.json(working_tree)",
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
func TestRuntimePathResolutionKeepsSubmoduleCheckoutLocal(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	parent := filepath.Join(t.TempDir(), "parent")
	repo := filepath.Join(parent, "coding-ethos")
	hooks := filepath.Join(repo, ".git", "hooks")

	err := os.MkdirAll(repo, 0o700)
	if err != nil {
		t.Fatalf("create submodule fixture: %v", err)
	}

	fakeGit := fakeRuntimeGit(t, repo, hooks, parent)

	root, localRoot := resolveRuntimeRoot(fakeGit, repo)
	if root != repo {
		t.Fatalf("consumer root = %q, want checkout %q", root, repo)
	}

	if localRoot != repo {
		t.Fatalf("local root = %q, want submodule %q", localRoot, repo)
	}
}

func TestRuntimeGitCommonDirFallbackReadsLinkedWorktreeDotGitFile(t *testing.T) {
	parent := t.TempDir()
	commonDir := filepath.Join(parent, ".git")
	worktreeGitDir := filepath.Join(commonDir, "worktrees", "feature")
	worktreeRoot := filepath.Join(parent, "feature")

	err := os.MkdirAll(worktreeGitDir, 0o700)
	if err != nil {
		t.Fatalf("create worktree git dir: %v", err)
	}

	err = os.MkdirAll(worktreeRoot, 0o700)
	if err != nil {
		t.Fatalf("create worktree root: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(worktreeRoot, ".git"),
		[]byte("gitdir: "+worktreeGitDir+"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write worktree .git file: %v", err)
	}

	hooksDir := filepath.Join(worktreeRoot, ".git", "hooks")
	got := resolveRuntimeGitCommonDir("/missing/git", worktreeRoot, hooksDir)
	if got != commonDir {
		t.Fatalf("git common dir = %q, want %q", got, commonDir)
	}
}

func TestRuntimeGitDirFallbackReadsLinkedWorktreeDotGitFile(t *testing.T) {
	parent := t.TempDir()
	commonDir := filepath.Join(parent, ".git")
	worktreeGitDir := filepath.Join(
		commonDir,
		"worktrees",
		"feature-branch",
		"modules",
		"coding-ethos",
	)
	worktreeRoot := filepath.Join(parent, "feature", "coding-ethos")

	err := os.MkdirAll(worktreeGitDir, 0o700)
	if err != nil {
		t.Fatalf("create worktree git dir: %v", err)
	}

	err = os.MkdirAll(worktreeRoot, 0o700)
	if err != nil {
		t.Fatalf("create worktree root: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(worktreeRoot, ".git"),
		[]byte("gitdir: "+worktreeGitDir+"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write worktree .git file: %v", err)
	}

	hooksDir := filepath.Join(worktreeRoot, ".git", "hooks")
	got := resolveRuntimeGitDir("/missing/git", worktreeRoot, hooksDir)
	if got != worktreeGitDir {
		t.Fatalf("git dir = %q, want %q", got, worktreeGitDir)
	}
}

func TestRuntimeGitCommonDirFallbackReadsCommonDirFile(t *testing.T) {
	parent := t.TempDir()
	commonDir := filepath.Join(parent, ".git", "modules", "coding-ethos")
	worktreeGitDir := filepath.Join(
		parent,
		".git",
		"worktrees",
		"feature-branch",
		"modules",
		"coding-ethos",
	)
	worktreeRoot := filepath.Join(parent, "feature", "coding-ethos")

	err := os.MkdirAll(worktreeGitDir, 0o700)
	if err != nil {
		t.Fatalf("create worktree git dir: %v", err)
	}

	err = os.MkdirAll(worktreeRoot, 0o700)
	if err != nil {
		t.Fatalf("create worktree root: %v", err)
	}

	relativeCommonDir, err := filepath.Rel(worktreeGitDir, commonDir)
	if err != nil {
		t.Fatalf("rel commondir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(worktreeGitDir, "commondir"),
		[]byte(relativeCommonDir+"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write commondir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(worktreeRoot, ".git"),
		[]byte("gitdir: "+worktreeGitDir+"\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write worktree .git file: %v", err)
	}

	hooksDir := filepath.Join(worktreeRoot, ".git", "hooks")
	got := resolveRuntimeGitCommonDir("/missing/git", worktreeRoot, hooksDir)
	if got != commonDir {
		t.Fatalf("git common dir = %q, want %q", got, commonDir)
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
		"CODE_ETHOS_AGENT_API_PROXY",
		"CODE_ETHOS_AGENT_API_PROXY_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"http_proxy",
		"https_proxy",
		"PATH",
	)
	t.Cleanup(restoreEnv)

	paths := runtimePathSet(runtimePathInputs{
		RealGit:       "/usr/bin/git",
		InvocationCWD: "/repo/pkg",
		LocalRoot:     "/repo",
		GitDir:        "/repo/.git",
		GitCommonDir:  "/repo/.git",
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

func TestAgentAPIProxyRoutingEnvRequiresExplicitOptIn(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-agent-api-proxy-env")

	restoreEnv := captureRuntimeEnvForTest(
		"CODE_ETHOS_AGENT_API_PROXY",
		"CODE_ETHOS_AGENT_API_PROXY_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"http_proxy",
		"https_proxy",
	)
	t.Cleanup(restoreEnv)

	t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", "http://127.0.0.1:8080")
	if got := agentAPIProxyRoutingEnv(); len(got) != 0 {
		t.Fatalf("proxy env without opt-in = %#v", got)
	}

	t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "1")
	got := agentAPIProxyRoutingEnv()
	for _, key := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		if got[key] != "http://127.0.0.1:8080" {
			t.Fatalf("proxy env %s = %q in %#v", key, got[key], got)
		}
	}
}

func TestAgentAPIProxyRoutingEnvRejectsInvalidURL(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-agent-api-proxy-invalid-env")

	restoreEnv := captureRuntimeEnvForTest(
		"CODE_ETHOS_AGENT_API_PROXY",
		"CODE_ETHOS_AGENT_API_PROXY_URL",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"http_proxy",
		"https_proxy",
	)
	t.Cleanup(restoreEnv)

	for _, value := range []string{
		"127.0.0.1:8080",
		"https:///v1/messages",
		"ftp://127.0.0.1:8080",
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("CODE_ETHOS_AGENT_API_PROXY", "1")
			t.Setenv("CODE_ETHOS_AGENT_API_PROXY_URL", value)

			if got := agentAPIProxyRoutingEnv(); len(got) != 0 {
				t.Fatalf("proxy env for invalid URL %q = %#v", value, got)
			}
		})
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
		{
			name: "policy git exit code",
			run: func() {
				exitErr(gitwrap.ExitCodeError{Code: 2})
			},
			want: 2,
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

func TestAgentShellCommandRequiresExplicitSeparator(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{},
		{"git", "status"},
		{"--"},
		{"--", ""},
	} {
		if _, err := agentShellCommand(args); err == nil {
			t.Fatalf("agentShellCommand(%#v) unexpectedly succeeded", args)
		}
	}
}

func TestAgentShellCommandBuildsShellCommand(t *testing.T) {
	t.Parallel()

	request, err := agentShellCommand([]string{"--", "git", "status", "pkg/a b.py"})
	if err != nil {
		t.Fatalf("agent shell command: %v", err)
	}

	if request.Command != "git status 'pkg/a b.py'" || !request.Rewrite {
		t.Fatalf("request = %#v", request)
	}
}

func TestAgentShellCommandDefaultsRewrite(t *testing.T) {
	t.Parallel()

	request, err := agentShellCommand([]string{"--", "git status"})
	if err != nil {
		t.Fatalf("agent shell command: %v", err)
	}

	if request.Command != "git status" || !request.Rewrite {
		t.Fatalf("request = %#v", request)
	}
}

func TestAgentShellCommandParsesNoRewriteFlag(t *testing.T) {
	t.Parallel()

	request, err := agentShellCommand([]string{"--no-rewrite", "--", "git status"})
	if err != nil {
		t.Fatalf("agent shell command: %v", err)
	}

	if request.Command != "git status" || request.Rewrite {
		t.Fatalf("request = %#v", request)
	}
}

func TestAgentShellCommandParsesFlagsInAnyOrder(t *testing.T) {
	t.Parallel()

	request, err := agentShellCommand([]string{
		"--check",
		"--intent=inspect repo status",
		"--rewrite",
		"--",
		"git",
		"status",
	})
	if err != nil {
		t.Fatalf("agent shell command: %v", err)
	}

	if !request.Check || !request.Rewrite ||
		request.Intent != "inspect repo status" ||
		request.Command != "git status" {
		t.Fatalf("request = %#v", request)
	}
}

func TestAgentShellRewriteRoutesGitToPolicyGitWithoutNestedRunner(t *testing.T) {
	paths := runtimeTestPaths(t)
	writePolicyBundleForTest(t, hookPolicyBundlePath(paths))
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", paths.RunBinary)

	request, err := agentShellCommand([]string{"--", "git", "status"})
	if err != nil {
		t.Fatalf("agent shell command: %v", err)
	}

	rewritten, err := rewriteAgentShellCommand(paths, request)
	if err != nil {
		t.Fatalf("rewrite agent shell command: %v", err)
	}

	if strings.Contains(rewritten, "agent-shell") {
		t.Fatalf("rewritten command recurses through agent-shell: %q", rewritten)
	}
	if !strings.Contains(rewritten, "policy-git") ||
		!strings.Contains(rewritten, "status") {
		t.Fatalf("rewritten command does not route through policy-git: %q", rewritten)
	}
}

func TestAgentShellEdgeDecisionBlocksRecursiveRunner(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"cerun -- git status",
		"bin/coding-ethos-run agent-shell -- git status",
	} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()

			decision, blocked := agentShellEdgeDecision(command)
			if !blocked {
				t.Fatalf("recursive runner command %q was allowed", command)
			}
			if decision.PolicyID != "runner.recursive_invocation" {
				t.Fatalf("policy = %q", decision.PolicyID)
			}
		})
	}
}

func TestAgentShellSandboxPlanRoutesThroughNativeWrapper(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("agent-shell native sandbox is Linux-only")
	}

	home := t.TempDir()
	runtimeDir := t.TempDir()
	resolvedGPGHome := filepath.Join(t.TempDir(), "dot-gnupg")
	if err := os.MkdirAll(resolvedGPGHome, 0o700); err != nil {
		t.Fatalf("create resolved GPG home fixture: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(resolvedGPGHome, "trustdb.gpg"),
		[]byte{},
		0o600,
	); err != nil {
		t.Fatalf("create resolved GPG file fixture: %v", err)
	}
	resolvedPrivateKeys := filepath.Join(resolvedGPGHome, "private-keys-v1.d")
	if err := os.MkdirAll(resolvedPrivateKeys, 0o700); err != nil {
		t.Fatalf("create resolved private key fixture: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(resolvedPrivateKeys, "key.key"),
		[]byte{},
		0o600,
	); err != nil {
		t.Fatalf("create private key fixture: %v", err)
	}
	gpgHome := filepath.Join(home, ".gnupg")
	if err := os.MkdirAll(gpgHome, 0o700); err != nil {
		t.Fatalf("create GPG home fixture: %v", err)
	}
	privateKeys := filepath.Join(gpgHome, "private-keys-v1.d")
	if err := os.MkdirAll(privateKeys, 0o700); err != nil {
		t.Fatalf("create GPG private key directory fixture: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(resolvedGPGHome, "trustdb.gpg"),
		filepath.Join(gpgHome, "trustdb.gpg"),
	); err != nil {
		t.Fatalf("create GPG symlink fixture: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(resolvedPrivateKeys, "key.key"),
		filepath.Join(privateKeys, "key.key"),
	); err != nil {
		t.Fatalf("create private key symlink fixture: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("GNUPGHOME", "")
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)
	t.Setenv("DISPLAY", ":0")
	t.Setenv("GPG_TTY", "/stale/tty")
	t.Setenv("TMPDIR", "/tmp/stale")
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	t.Setenv("XAUTHORITY", filepath.Join(home, ".Xauthority"))
	t.Setenv(envAgentAPIProxyEnabled, "1")
	t.Setenv(envAgentAPIProxyURL, "http://127.0.0.1:18080")

	paths := runtimeTestPaths(t)
	paths.GitDir = filepath.Join(
		filepath.Dir(paths.GitCommonDir),
		"worktrees",
		"feature",
		"modules",
		"coding-ethos",
	)
	writeExecutableFixture(
		t,
		filepath.Join(paths.BinDir, "coding-ethos-sandbox"),
		"#!/usr/bin/env sh\nexit 0\n",
	)

	plan, env, cleanup, err := agentShellSandboxPlan(
		paths,
		"/usr/bin/env",
		[]string{"bash", "-lc", "git status"},
		"git status",
	)
	defer cleanup()
	if err != nil {
		t.Fatalf("agentShellSandboxPlan() error = %v", err)
	}

	if plan.Executable != filepath.Join(paths.BinDir, "coding-ethos-sandbox") {
		t.Fatalf("plan executable = %q", plan.Executable)
	}
	if !slices.ContainsFunc(env, func(item string) bool {
		return strings.HasPrefix(
			item,
			"PATH="+filepath.Join(paths.Root, ".coding-ethos", "cache", "agent-shell"),
		)
	}) {
		t.Fatalf("agent-shell env does not prioritize managed git wrapper: %#v", env)
	}
	if !slices.Contains(env, "CODING_ETHOS_AGENT_SHELL_SANDBOX=1") {
		t.Fatalf("agent-shell env does not mark sandbox execution: %#v", env)
	}
	for _, want := range agentShellProxyEnvNames() {
		if !slices.Contains(plan.Evidence.EnvBindings, want) {
			t.Fatalf("sandbox evidence missing env binding %q: %#v", want, plan.Evidence)
		}
	}
	if !slices.Contains(
		env,
		"TMPDIR="+filepath.Join(paths.Root, sandbox.SandboxTempWritePath),
	) {
		t.Fatalf("agent-shell env does not route temp files into sandbox: %#v", env)
	}
	for _, unwanted := range []string{"DISPLAY=", "WAYLAND_DISPLAY=", "XAUTHORITY="} {
		if slices.ContainsFunc(env, func(item string) bool {
			return strings.HasPrefix(item, unwanted)
		}) {
			t.Fatalf("agent-shell env leaked GUI pinentry variable %q: %#v", unwanted, env)
		}
	}
	for _, item := range env {
		if item == "GPG_TTY=/stale/tty" || item == "TMPDIR=/tmp/stale" {
			t.Fatalf("agent-shell env leaked stale process variable: %#v", env)
		}
	}
	if gpgTTY := agentShellGPGTTY(); gpgTTY != "" &&
		!slices.Contains(env, "GPG_TTY="+gpgTTY) {
		t.Fatalf("agent-shell env does not set current GPG_TTY %q: %#v", gpgTTY, env)
	}
	if !slices.ContainsFunc(env, func(item string) bool {
		return strings.HasPrefix(item, "CODING_ETHOS_REAL_GIT="+filepath.Join(
			paths.Root,
			".coding-ethos",
			"cache",
			"agent-shell",
		)) && strings.HasSuffix(item, string(filepath.Separator)+"real-git")
	}) {
		t.Fatalf("agent-shell env does not expose sandbox real git bind: %#v", env)
	}
	if !plan.Evidence.RequiresGit {
		t.Fatalf("agent-shell plan must preserve git-required evidence: %#v", plan.Evidence)
	}
	if !plan.Evidence.RequiresProcesses || plan.Evidence.ProcessIsolated {
		t.Fatalf("agent-shell plan must allow hook subprocess sandboxes: %#v", plan.Evidence)
	}
	for _, want := range []string{
		paths.GitDir,
		paths.GitCommonDir,
		filepath.Join(home, ".gnupg"),
		resolvedGPGHome,
		resolvedPrivateKeys,
		filepath.Join(runtimeDir, "gnupg"),
	} {
		if !containsFlagValue(plan.Args, "--write-path", want) {
			t.Fatalf("plan args missing git write path %q: %#v", want, plan.Args)
		}
	}
	for _, unwanted := range []string{"--git-wrapper", "--real-git-path", "--git-target"} {
		if slices.Contains(plan.Args, unwanted) {
			t.Fatalf(
				"agent-shell plan asked native sandbox for git bind %q: %#v",
				unwanted,
				plan.Args,
			)
		}
	}
	for _, want := range []string{"--", "/usr/bin/env", "bash", "-lc", "git status"} {
		if !slices.Contains(plan.Args, want) {
			t.Fatalf("plan args missing %q: %#v", want, plan.Args)
		}
	}
}

func TestAgentShellGitWritePathsDeduplicatesGitDirs(t *testing.T) {
	t.Parallel()

	gitDir := "/repo/.git"
	got := agentShellGitWritePaths(runtimePaths{
		GitDir:       gitDir,
		GitCommonDir: gitDir,
	})
	if !slices.Equal(got, []string{gitDir}) {
		t.Fatalf("agentShellGitWritePaths() = %#v, want %#v", got, []string{gitDir})
	}
}

func TestAppendExistingGPGHomeWritePathsSkipsMissingHome(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing-gnupg")
	got := appendExistingGPGHomeWritePaths([]string{"/repo/.git"}, missing)
	if !slices.Equal(got, []string{"/repo/.git"}) {
		t.Fatalf("write paths = %#v, want only existing paths", got)
	}
}

func TestAgentShellTerminalPathAllowsCurrentPTYOnly(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"/dev/pts/18", "/dev/tty"} {
		if !agentShellTerminalPath(path) {
			t.Fatalf("agentShellTerminalPath(%q) = false", path)
		}
	}
	for _, path := range []string{"/dev/pts", "/dev/null", "/tmp/tty"} {
		if agentShellTerminalPath(path) {
			t.Fatalf("agentShellTerminalPath(%q) = true", path)
		}
	}
}

func TestSharedLockDirectoryMetadataRequiresTheExactExternalCapabilityShape(
	t *testing.T,
) {
	shared := t.TempDir()
	if err := os.Chmod(shared, os.ModeSticky|0o777); err != nil {
		t.Fatalf("set shared lock directory mode: %v", err)
	}
	info, err := os.Lstat(shared)
	if err != nil {
		t.Fatalf("inspect shared lock metadata: %v", err)
	}
	if !validSharedLockDirectoryMetadata("/var/tmp/coding-ethos-shared-lock-test", info) {
		t.Fatal("the exact direct-child mode-1777 capability shape was rejected")
	}

	if err := os.Chmod(shared, 0o0777); err != nil {
		t.Fatalf("remove sticky bit: %v", err)
	}
	info, err = os.Lstat(shared)
	if err != nil {
		t.Fatalf("inspect non-sticky metadata: %v", err)
	}
	if validSharedLockDirectoryMetadata("/var/tmp/coding-ethos-shared-lock-test", info) {
		t.Fatal("a non-sticky external directory became a shared lock capability")
	}
	if validSharedLockDirectoryMetadata("/tmp/coding-ethos-shared-lock-test", info) {
		t.Fatal("a path outside /var/tmp became a shared lock capability")
	}
}

func containsFlagValue(args []string, flag, value string) bool {
	for index := 0; index < len(args)-1; index++ {
		if args[index] == flag && args[index+1] == value {
			return true
		}
	}

	return false
}

func TestAgentShellWorktreeWritePathsExcludeProtectedRuntimeDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	for _, dir := range []string{
		"go",
		"docs",
		".git",
		".coding-ethos",
	} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			t.Fatalf("create worktree dir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(
		filepath.Join(root, "Makefile"),
		[]byte("all:\n"),
		0o600,
	); err != nil {
		t.Fatalf("write root file: %v", err)
	}
	if err := os.Symlink(
		filepath.Join(root, "Makefile"),
		filepath.Join(root, "Makefile.link"),
	); err != nil {
		t.Fatalf("create root symlink: %v", err)
	}

	got, err := agentShellWorktreeWritePaths(root)
	if err != nil {
		t.Fatalf("agentShellWorktreeWritePaths() error = %v", err)
	}

	for _, want := range []string{
		filepath.Join(root, "go"),
		filepath.Join(root, "docs"),
		filepath.Join(root, "Makefile"),
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("worktree write paths missing %s: %#v", want, got)
		}
	}
	for _, unwanted := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, ".coding-ethos"),
		filepath.Join(root, "Makefile.link"),
	} {
		if slices.Contains(got, unwanted) {
			t.Fatalf("worktree write paths included protected %s: %#v", unwanted, got)
		}
	}
}

// TestAgentShellWorktreeRootIsWritableAndProtectedEntriesArePinned covers the
// reason a merge could not replace a top-level file.
//
// Creating, deleting and renaming are rights over the directory holding a
// file, not over the file, so a write set built only from the entries beneath
// the root left the root ungranted and no top-level file could be replaced --
// only edited in place. git does exactly that on every merge, and it surfaced
// as "unable to unlink old 'Makefile': Permission denied" on files that were
// plainly writable, as though those files were singled out. They were not:
// they were the top-level ones main had touched.
//
// Granting the root reaches everything beneath it, so the entries that were
// protected by omission must now be pinned by a mount instead. Both halves are
// required and neither is any use alone.
func TestAgentShellWorktreeRootIsWritableAndProtectedEntriesArePinned(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(root, ".git"),
		[]byte("gitdir: /tmp/git\n"),
		0o600,
	); err != nil {
		t.Fatalf("create linked-worktree git pointer: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, ".coding-ethos"), 0o700); err != nil {
		t.Fatalf("create worktree dir .coding-ethos: %v", err)
	}

	got, err := agentShellWorktreeWritePaths(root)
	if err != nil {
		t.Fatalf("agentShellWorktreeWritePaths() error = %v", err)
	}

	if !slices.Contains(got, root) {
		t.Fatalf("worktree root is not writable, so no top-level file can be "+
			"created, deleted or renamed: %#v", got)
	}

	pinned, err := agentShellReadOnlyPaths(
		root,
		"/trusted/bin/coding-ethos-run",
		"git status",
	)
	if err != nil {
		t.Fatalf("agentShellReadOnlyPaths() error = %v", err)
	}
	for _, want := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, ".coding-ethos"),
	} {
		if !slices.Contains(pinned, want) {
			t.Fatalf("%s is beneath the now-writable root and is not pinned "+
				"read-only: %#v", want, pinned)
		}
	}
}

func TestAgentShellPrimaryGitDirectoryRemainsAnExplicitWriteCapability(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitDir := filepath.Join(root, ".git")
	if err := os.Mkdir(gitDir, 0o700); err != nil {
		t.Fatalf("create primary git directory: %v", err)
	}

	pinned, err := agentShellReadOnlyPaths(
		root,
		"/trusted/bin/coding-ethos-run",
		"git status",
	)
	if err != nil {
		t.Fatalf("agentShellReadOnlyPaths() error = %v", err)
	}
	if slices.Contains(pinned, gitDir) {
		t.Fatalf(
			"primary git metadata was pinned despite its explicit write capability: %#v",
			pinned,
		)
	}
	if want := filepath.Join(root, ".coding-ethos"); !slices.Contains(pinned, want) {
		t.Fatalf("coding-ethos policy directory is not pinned: %#v", pinned)
	}
}

func TestAgentShellPolicyTreeWriteExceptionIsStaticAndGitScoped(t *testing.T) {
	t.Parallel()

	runBinary := "/trusted/bin/coding-ethos-run"
	allowed := []string{
		"git merge --no-edit main",
		"/usr/bin/git commit -S -m test",
		"git stash push --keep-index",
		runBinary + " policy-git merge --no-edit main",
	}
	for _, command := range allowed {
		if !agentShellGitMayReplacePolicyTree(runBinary, command) {
			t.Fatalf("static Git worktree command was not admitted: %q", command)
		}
	}

	denied := []string{
		"git status",
		"git -c core.hooksPath=/tmp merge main",
		"git merge main; touch .coding-ethos/policy",
		`git merge "$TARGET"`,
		"MODE=test git merge main",
		"git merge main > .coding-ethos/result",
		"/tmp/coding-ethos-run policy-git merge main",
	}
	for _, command := range denied {
		if agentShellGitMayReplacePolicyTree(runBinary, command) {
			t.Fatalf(
				"non-static or non-worktree command received policy-tree access: %q",
				command,
			)
		}
	}
}

func TestAgentShellMergeMayReplaceTrackedPolicyFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	gitPointer := filepath.Join(root, ".git")
	if err := os.WriteFile(
		gitPointer,
		[]byte("gitdir: /tmp/git\n"),
		0o600,
	); err != nil {
		t.Fatalf("create linked-worktree git pointer: %v", err)
	}
	policyTree := filepath.Join(root, ".coding-ethos")
	if err := os.Mkdir(policyTree, 0o700); err != nil {
		t.Fatalf("create policy directory: %v", err)
	}

	runBinary := "/trusted/bin/coding-ethos-run"
	pinned, err := agentShellReadOnlyPaths(
		root,
		runBinary,
		runBinary+" policy-git merge --no-edit main",
	)
	if err != nil {
		t.Fatalf("agentShellReadOnlyPaths() error = %v", err)
	}
	if slices.Contains(pinned, policyTree) {
		t.Fatalf(
			"merge cannot unlink tracked policy files while parent is pinned: %#v",
			pinned,
		)
	}
	if !slices.Contains(pinned, gitPointer) {
		t.Fatalf("linked-worktree git pointer lost read-only protection: %#v", pinned)
	}
}

func TestAgentShellSandboxPlanFailsClosedWithoutNativeHelper(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("agent-shell native sandbox is Linux-only")
	}

	paths := runtimeTestPaths(t)

	_, _, cleanup, err := agentShellSandboxPlan(
		paths,
		"/usr/bin/env",
		[]string{"bash", "-lc", "git status"},
		"git status",
	)
	defer cleanup()
	if err == nil {
		t.Fatal("agentShellSandboxPlan() succeeded without coding-ethos-sandbox")
	}
	if !strings.Contains(err.Error(), "coding-ethos-sandbox") {
		t.Fatalf("error does not identify missing helper: %v", err)
	}
}

func TestAgentShellSandboxProfileIsExplicitByPlatform(t *testing.T) {
	t.Parallel()

	cases := []struct {
		goos     string
		profile  string
		enforced bool
	}{
		{goos: "linux", profile: "agent-shell", enforced: true},
		{goos: "darwin", profile: "none", enforced: false},
		{goos: "windows", profile: "none", enforced: false},
	}

	for _, test := range cases {
		t.Run(test.goos, func(t *testing.T) {
			t.Parallel()

			if agentShellSandboxProfile(test.goos) != test.profile {
				t.Fatalf("profile(%q) = %q", test.goos, agentShellSandboxProfile(test.goos))
			}
			if agentShellSandboxEnforced(test.goos) != test.enforced {
				t.Fatalf("enforced(%q) = %t", test.goos, agentShellSandboxEnforced(test.goos))
			}
		})
	}
}

func TestWriteExecutableFileCreatesRunnablePrivateFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "tool")
	err := writeExecutableFile(path, []byte("#!/usr/bin/env sh\nexit 0\n"))
	if err != nil {
		t.Fatalf("writeExecutableFile() error = %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat executable file: %v", err)
	}
	if info.Mode().Perm() != agentShellAssetMode {
		t.Fatalf("mode = %#o, want %#o", info.Mode().Perm(), agentShellAssetMode)
	}

	if err := validateExecutablePath(path); err != nil {
		t.Fatalf("validateExecutablePath() error = %v", err)
	}
}

func TestAgentShellCommandParsesCheckAndIntent(t *testing.T) {
	t.Parallel()

	request, err := agentShellCommand([]string{
		"--rewrite",
		"--check",
		"--intent",
		"fix formatter failures",
		"--",
		"git",
		"status",
	})
	if err != nil {
		t.Fatalf("agentShellCommand() error = %v", err)
	}

	if !request.Rewrite || !request.Check ||
		request.Intent != "fix formatter failures" ||
		request.Command != "git status" {
		t.Fatalf("request = %#v", request)
	}
}

func TestAgentShellEdgeScannerBlocksSecretsAndLocalPII(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		input  string
		policy string
	}{
		{
			name:   "secret",
			input:  "curl -H 'Authorization: token abcdefghijklmnopqrstuvwxyz1234'",
			policy: "runner.argv_secret",
		},
		{
			name:   "local path",
			input:  "cat /" + strings.Join([]string{"home", "example", ".ssh", "config"}, "/"),
			policy: "runner.argv_local_pii",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decision, blocked := agentShellEdgeDecision(test.input)
			if !blocked || decision.PolicyID != test.policy {
				t.Fatalf("edge decision = %#v, %v", decision, blocked)
			}
		})
	}
}

func TestAgentShellBlockResultFormatsSARIFRemediation(t *testing.T) {
	t.Parallel()

	decision, blocked := agentShellEdgeDecision(
		"curl -H 'Authorization: token abcdefghijklmnopqrstuvwxyz1234'",
	)
	if !blocked {
		t.Fatal("expected edge scanner block")
	}

	output, err := hookoutput.FormatLintResult(
		agentShellBlockLintResult(agentShellBlockedResult(decision)),
		hookoutput.FormatSARIF,
	)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}
	if !strings.Contains(output, `"agent_remediation"`) ||
		!strings.Contains(output, `"runner.argv_secret"`) {
		t.Fatalf("SARIF output missing remediation:\n%s", output)
	}
}

func TestAgentShellCheckRecordsEventLogWithoutDuckDB(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-agent-shell-check")

	paths := runtimeTestPaths(t)
	var calls []string
	paths.Executor = stubRuntimeOps{calls: &calls}
	writePolicyBundleForTest(t, hookPolicyBundlePath(paths))

	output := captureRuntimeStdout(t, func() {
		err := run(paths, []string{
			"agent-shell",
			"--check",
			"--intent",
			"inspect repo status",
			"--",
			"true",
		})
		if err != nil {
			t.Fatalf("run agent-shell --check: %v", err)
		}
	})

	if !strings.Contains(output, `"status": "allowed"`) ||
		!strings.Contains(output, `"strategic_intent": "inspect repo status"`) {
		t.Fatalf("check output = %s", output)
	}

	records, err := codeintel.NewEventLog(
		codeintel.DefaultEventLogDir(paths.Root),
	).ReadAll()
	if err != nil {
		t.Fatalf("read code-intel event log: %v", err)
	}
	if len(records) != 1 ||
		records[0].Kind != "proxy_event" ||
		records[0].Tool != "cerun" ||
		records[0].Provider != "coding-ethos" {
		t.Fatalf("event records = %#v", records)
	}

	if _, err := os.Stat(codeintel.DefaultDBPath(paths.Root)); !os.IsNotExist(err) {
		t.Fatalf("agent-shell check must not create DuckDB store: %v", err)
	}

	if len(calls) > 0 {
		t.Fatalf("agent-shell --check performed runtime side effects: %#v", calls)
	}
}

func TestPolicyGitIgnoresSpoofedAgentShellSandboxEnv(t *testing.T) {
	paths := runtimeTestPaths(t)
	var calls []string
	paths.Executor = stubRuntimeOps{calls: &calls}
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv(realgit.Env, filepath.Join(
		paths.Root,
		".coding-ethos",
		"cache",
		"agent-shell",
		"spoof",
		"real-git",
	))

	err := run(paths, []string{"policy-git", "status"})
	if err != nil {
		t.Fatalf("run policy-git: %v", err)
	}

	got := strings.Join(calls, "\n")
	if !strings.Contains(got, "direct-run:coding-ethos-toolchain install-git-shim") {
		t.Fatalf("spoofed sandbox env skipped shim install: %#v", calls)
	}
	if !strings.Contains(
		got,
		"exec:coding-ethos-git --bundle "+hookPolicyBundlePath(paths)+
			" --real-git "+paths.RealGit+" status",
	) {
		t.Fatalf("policy-git did not execute managed git: %#v", calls)
	}
}

func TestPolicyGitIgnoresArbitraryEnvRealGitExecutable(t *testing.T) {
	paths := runtimeTestPaths(t)
	var calls []string
	paths.Executor = stubRuntimeOps{calls: &calls}
	writePolicyBundleForTest(t, hookPolicyBundlePath(paths))

	attackerGit := filepath.Join(t.TempDir(), "attacker-git")
	writeExecutableFixture(t, attackerGit, "#!/usr/bin/env sh\nexit 0\n")
	t.Setenv(realgit.Env, attackerGit)

	err := run(paths, []string{"policy-git", "status"})
	if err != nil {
		t.Fatalf("run policy-git: %v", err)
	}

	got := strings.Join(calls, "\n")
	if strings.Contains(got, "--real-git "+attackerGit) {
		t.Fatalf("policy-git executed arbitrary env real git: %#v", calls)
	}
	if !strings.Contains(got, "direct-run:coding-ethos-toolchain install-git-shim") {
		t.Fatalf("arbitrary env real git skipped shim install: %#v", calls)
	}
	if !strings.Contains(
		got,
		"exec:coding-ethos-git --bundle "+hookPolicyBundlePath(paths)+
			" --real-git "+paths.RealGit+" status",
	) {
		t.Fatalf("policy-git did not execute managed git: %#v", calls)
	}
}

func TestAgentShellNativeGitBindRequiresReadOnlyMountInfo(t *testing.T) {
	t.Parallel()

	path := "/repo/.coding-ethos/cache/agent-shell/run-1/real-git"

	if !readOnlyMountInfoForPath(
		"42 1 0:1 / "+path+" ro,relatime - ext4 /dev/sda rw\n",
		path,
	) {
		t.Fatal("read-only bind mount was not recognized")
	}
	if readOnlyMountInfoForPath(
		"42 1 0:1 / "+path+" rw,relatime - ext4 /dev/sda rw\n",
		path,
	) {
		t.Fatal("read-write mount was accepted as sandbox bind")
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
	body := "#!/bin/sh\n" +
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
	body := "#!/bin/sh\n" +
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
	body := "#!/bin/sh\n" +
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

func TestResolveRuntimeGitIgnoresEnvExecutable(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, "env")
	pathDir := filepath.Join(root, "path")
	if err := os.MkdirAll(envDir, 0o700); err != nil {
		t.Fatalf("create env git dir: %v", err)
	}
	if err := os.MkdirAll(pathDir, 0o700); err != nil {
		t.Fatalf("create PATH git dir: %v", err)
	}

	envGit := filepath.Join(envDir, "git")
	pathGit := filepath.Join(pathDir, "git")
	writeExecutableFixture(t, envGit, "#!/usr/bin/env sh\nprintf 'git version env\\n'\n")
	writeExecutableFixture(t, pathGit, "#!/usr/bin/env sh\nprintf 'git version path\\n'\n")
	t.Setenv(realgit.Env, envGit)
	t.Setenv("PATH", pathDir)

	got, err := resolveRuntimeGit()
	if err != nil {
		t.Fatalf("resolveRuntimeGit() error = %v", err)
	}
	if got != pathGit {
		t.Fatalf("resolveRuntimeGit() = %q, want PATH git %q", got, pathGit)
	}
	if restored := os.Getenv(realgit.Env); restored != envGit {
		t.Fatalf("resolveRuntimeGit() did not restore %s: %q", realgit.Env, restored)
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
	writeRuntimePythonTargetConfig(
		t,
		paths,
		[]string{"pkg"},
		[]string{"tests"},
	)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	cases := []struct {
		name string
		want string
		args []string
	}{
		{
			name: "agent shell",
			args: []string{"agent-shell", "--no-rewrite", "--", "git", "status"},
			want: "direct-run:coding-ethos-toolchain install-git-shim " +
				"--dest-dir " + paths.BinDir +
				" --real-git " + paths.RealGit + " --runner " + paths.RunBinary + "\n" +
				"run-lint:--install-shims --tools-bin-dir " + paths.BinDir +
				" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot + "\n" +
				"agent-shell:git status",
		},
		{
			name: "policy lint",
			args: []string{"policy-lint", "--scope", "staged"},
			want: "exec-lint:--bundle " + paths.PolicyBundle + " --scope staged",
		},
		{
			name: "lint alias",
			args: []string{"lint", "--staged"},
			want: "exec-lint:--bundle " + paths.PolicyBundle + " --code-intel --staged",
		},
		{
			name: "lint alias explicit files",
			args: []string{"lint", "--files", "pkg/app.py,go/internal/app.go"},
			want: "run-lint:--code-intel --bundle " + paths.PolicyBundle +
				" --managed-capture-tool ruff-format --ethos-root " + paths.EthosRoot +
				" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
				" -- format pkg/app.py\n" +
				"run-lint:--code-intel --bundle " + paths.PolicyBundle +
				" --managed-capture-tool golangci-lint-format --ethos-root " +
				paths.EthosRoot + " --consumer-root " + paths.Root +
				" --invocation-cwd " + paths.InvocationCWD + " -- go/internal/app.go\n" +
				"run-lint:--code-intel --bundle " + paths.PolicyBundle +
				" --managed-capture-tool ruff-autofix --ethos-root " + paths.EthosRoot +
				" --consumer-root " + paths.Root + " --invocation-cwd " +
				paths.InvocationCWD +
				" -- check --fix --quiet --ignore-noqa --output-format json pkg/app.py\n" +
				"run-lint:--code-intel --bundle " + paths.PolicyBundle +
				" --managed-capture-tool golangci-lint-autofix --ethos-root " +
				paths.EthosRoot + " --consumer-root " + paths.Root +
				" --invocation-cwd " + paths.InvocationCWD + " -- go/internal/app.go\n" +
				"exec-lint:--bundle " + paths.PolicyBundle +
				" --code-intel --files pkg/app.py,go/internal/app.go",
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
			name: "output report",
			args: []string{"output", "report", "--format", "json"},
			want: "direct-exec:coding-ethos-output report --root " + paths.Root +
				" --format json",
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
				" -- format pkg tests\n" +
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
	writeRuntimePythonTargetConfig(
		t,
		paths,
		[]string{"pkg"},
		[]string{"tests"},
	)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	code := runRuntime(paths, []string{"policy-tool-group", "linters"})
	if code != 0 {
		t.Fatalf("runRuntime exit = %d, want 0", code)
	}

	want := "run-lint:--bundle " + paths.PolicyBundle +
		" --managed-capture-tool ruff --ethos-root " + paths.EthosRoot +
		" --consumer-root " + paths.Root + " --invocation-cwd " + paths.InvocationCWD +
		" -- check pkg tests\n" +
		"run-lint:--bundle " + paths.PolicyBundle +
		" --managed-capture-tool golangci-lint --ethos-root " +
		paths.EthosRoot + " --consumer-root " + paths.Root +
		" --invocation-cwd " + paths.InvocationCWD + " --"

	got := strings.Join(calls, "\n")
	if got != want {
		t.Fatalf("policy-tool-group linters calls = %q, want %q", got, want)
	}
}

func TestManagedLintUsesParentPythonTargetsForUnscopedFormatters(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	writeRuntimePythonTargetConfig(
		t,
		paths,
		[]string{"src/gmeow", "scripts", "missing"},
		[]string{"tests"},
	)

	var calls []string

	paths.Executor = stubRuntimeOps{calls: &calls}

	err := run(paths, []string{"lint"})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	got := strings.Join(calls, "\n")
	for _, want := range []string{
		" -- format src/gmeow scripts tests",
		" -- check --fix --quiet --ignore-noqa --output-format json src/gmeow scripts tests",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("managed lint calls missing %q:\n%s", want, got)
		}
	}

	// Only inspect the lint target list after the "--" separator. The
	// surrounding argv carries sandbox temp paths that legitimately contain
	// the parent repo directory name (for example "coding_ethos") when the
	// tests run under hook-managed sandboxes.
	for _, call := range calls {
		_, targets, found := strings.Cut(call, " -- ")
		if !found {
			continue
		}

		for _, unwanted := range []string{"coding_ethos", "missing"} {
			if strings.Contains(targets, unwanted) {
				t.Fatalf("managed lint call targets contain %q:\n%s", unwanted, call)
			}
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
	writeRuntimePythonTargetConfig(
		t,
		paths,
		[]string{"pkg"},
		[]string{"tests"},
	)

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

func TestManagedLintScopeFromArgsReadsExplicitFiles(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	filesList := filepath.Join(paths.Root, "lint-files.txt")

	err := os.WriteFile(
		filesList,
		[]byte("pkg/from-file.py\ninternal/from-file.go\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write file list: %v", err)
	}

	scope, err := managedLintScopeFromArgs(paths, []string{
		"--files",
		"pkg/app.py,go/internal/app.go",
		"--files-from",
		filesList,
	})
	if err != nil {
		t.Fatalf("managedLintScopeFromArgs: %v", err)
	}

	want := []string{
		"pkg/app.py",
		"go/internal/app.go",
		"pkg/from-file.py",
		"internal/from-file.go",
	}
	if !scope.Scoped || !slices.Equal(scope.Files, want) {
		t.Fatalf("scope = %#v, want scoped files %#v", scope, want)
	}
}

func TestManagedLintScopeFromArgsReadsStagedGitFiles(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)
	writeExecutableFixture(t, paths.RealGit, `#!/usr/bin/env sh
case " $* " in
  *" diff --cached --name-only --diff-filter=ACMR -- "*) printf 'pkg/app.py\npkg/app.py\ngo/app.go\n' ;;
  *) exit 3 ;;
esac
`)

	scope, err := managedLintScopeFromArgs(paths, []string{"--staged"})
	if err != nil {
		t.Fatalf("managedLintScopeFromArgs staged: %v", err)
	}

	want := []string{"pkg/app.py", "go/app.go"}
	if !scope.Scoped || !slices.Equal(scope.Files, want) {
		t.Fatalf("scope = %#v, want staged files %#v", scope, want)
	}
}

func TestScopedPolicyToolGroupFiltersFormatterTargets(t *testing.T) {
	t.Parallel()

	group := []policyToolGroupEntry{
		{Tool: "ruff-format", Args: []string{"format", "pkg", "tests"}},
		{Tool: "golangci-lint-format"},
	}

	scoped := scopedPolicyToolGroup(group, managedLintScope{
		Files:  []string{"pkg/app.py", "go/app.go", "README.md"},
		Scoped: true,
	})
	if len(scoped) != 2 {
		t.Fatalf("scoped group = %#v, want two formatter entries", scoped)
	}

	wantRuff := []string{"format", "pkg/app.py"}
	if scoped[0].Tool != "ruff-format" || !slices.Equal(scoped[0].Args, wantRuff) {
		t.Fatalf("ruff scoped entry = %#v, want args %#v", scoped[0], wantRuff)
	}

	wantGo := []string{"go/app.go"}
	if scoped[1].Tool != "golangci-lint-format" ||
		!slices.Equal(scoped[1].Args, wantGo) {
		t.Fatalf("go scoped entry = %#v, want args %#v", scoped[1], wantGo)
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
		"direct-run:coding-ethos-policy validate-metadata --metadata " +
			hookPolicyMetadataPath(paths),
		"run-lint:--install-shims --tools-bin-dir " + paths.BinDir +
			" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot,
		"direct-exec:coding-ethos-git-hook --bundle " + hookPolicyBundlePath(paths) +
			" --runner " + paths.GitHookRunner + " --cwd " + paths.Root + " validate",
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

func TestRunGitHookIgnoresObsoletePrepareCommitMsgHook(t *testing.T) {
	t.Parallel()

	paths := runtimeTestPaths(t)

	var calls []string
	paths.Executor = stubRuntimeOps{calls: &calls}

	err := runGitHook(paths, []string{"prepare-commit-msg", "COMMIT_EDITMSG"})
	if err != nil {
		t.Fatalf("runGitHook prepare-commit-msg: %v", err)
	}

	if len(calls) != 0 {
		t.Fatalf("prepare-commit-msg made runtime calls: %#v", calls)
	}
}

//nolint:paralleltest // Serializes process-global wrapper authorization env.
func TestRunGitHookDoesNotSelfAuthorizeNativeGitHooks(t *testing.T) {
	testlock.ProcessState(t, "coding-ethos-run-env")

	restoreEnv := captureRuntimeEnvForTest(
		gitwrap.WrapperAuthorizedEnv,
		gitwrap.WrapperPIDEnv,
	)
	t.Cleanup(restoreEnv)

	t.Setenv(gitwrap.WrapperAuthorizedEnv, "")
	t.Setenv(gitwrap.WrapperPIDEnv, "")

	paths := runtimeTestPaths(t)

	var calls []string
	paths.Executor = stubRuntimeOps{calls: &calls}

	err := runGitHook(paths, []string{"pre-commit"})
	if err != nil {
		t.Fatalf("runGitHook pre-commit: %v", err)
	}

	if got := os.Getenv(gitwrap.WrapperAuthorizedEnv); got != "" {
		t.Fatalf("%s = %q, want unset", gitwrap.WrapperAuthorizedEnv, got)
	}

	if got := os.Getenv(gitwrap.WrapperPIDEnv); got != "" {
		t.Fatalf("%s = %q, want unset", gitwrap.WrapperPIDEnv, got)
	}

	for _, want := range []string{
		"run-lint:--install-shims --tools-bin-dir " + paths.BinDir +
			" --runner " + paths.RunBinary + " --ethos-root " + paths.EthosRoot,
		"direct-exec:coding-ethos-git-hook --bundle " + hookPolicyBundlePath(paths) +
			" --runner " + paths.GitHookRunner + " --cwd " + paths.Root + " pre-commit",
	} {
		if !slices.Contains(calls, want) {
			t.Fatalf("git hook calls missing %q: %#v", want, calls)
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
		"agent-hook:--bundle " + hookPolicyBundlePath(paths) + " --json",
		"direct-exec:coding-ethos-mcp --bundle " + hookPolicyBundlePath(paths),
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
	assertInvalidRunCommand(t, "runner unknown command", func() error {
		return run(paths, []string{"explode"})
	}, "unknown coding-ethos-run command")
}

func TestRunHelpListsManagedLintEntrypoints(t *testing.T) {
	t.Parallel()

	output := captureRuntimeStdout(t, func() {
		err := run(runtimeTestPaths(t), []string{"--help"})
		if err != nil {
			t.Fatalf("run help: %v", err)
		}
	})

	for _, want := range []string{
		"coding-ethos-run",
		"lint --staged",
		"lint --changed",
		"lint --full",
		"bin/lint --staged",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("help missing %q:\n%s", want, output)
		}
	}
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

	realGitPath := filepath.Join(root, "system", "git")
	err = os.MkdirAll(filepath.Dir(realGitPath), 0o755)
	if err != nil {
		t.Fatalf("create real git fixture dir: %v", err)
	}
	writeExecutableFixture(t, realGitPath, "#!/usr/bin/env sh\nexit 0\n")

	paths := runtimePaths{
		RealGit:        realGitPath,
		InvocationCWD:  filepath.Join(root, "pkg"),
		Root:           root,
		GitDir:         filepath.Join(root, ".git"),
		GitCommonDir:   filepath.Join(root, ".git"),
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
		hookPolicyBundlePath(paths),
		hookPolicyMetadataPath(paths),
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

func writeRuntimePythonTargetConfig(
	t *testing.T,
	paths runtimePaths,
	sourcePaths []string,
	testPaths []string,
) {
	t.Helper()

	for _, target := range append(append([]string{}, sourcePaths...), testPaths...) {
		if target == "missing" {
			continue
		}

		path := filepath.Join(paths.Root, filepath.FromSlash(target))
		if filepath.Ext(target) == ".py" {
			err := os.MkdirAll(filepath.Dir(path), 0o755)
			if err != nil {
				t.Fatalf("create target parent %s: %v", path, err)
			}

			err = os.WriteFile(path, []byte("print('ok')\n"), 0o600)
			if err != nil {
				t.Fatalf("write target %s: %v", path, err)
			}

			continue
		}

		err := os.MkdirAll(path, 0o755)
		if err != nil {
			t.Fatalf("create target dir %s: %v", path, err)
		}
	}

	writeRuntimeConfigFile(
		t,
		filepath.Join(paths.EthosRoot, "config.yaml"),
		sourcePaths,
		testPaths,
	)
	writeRuntimeConfigFile(
		t,
		filepath.Join(paths.Root, "repo_config.yaml"),
		sourcePaths,
		testPaths,
	)
}

func writeRuntimeConfigFile(
	t *testing.T,
	path string,
	sourcePaths []string,
	testPaths []string,
) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("create config parent %s: %v", path, err)
	}

	content := "python:\n  source_paths:\n" +
		yamlStringList(sourcePaths) +
		"  test_paths:\n" +
		yamlStringList(testPaths)

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write config %s: %v", path, err)
	}
}

func yamlStringList(values []string) string {
	var builder strings.Builder

	for _, value := range values {
		builder.WriteString("    - ")
		builder.WriteString(value)
		builder.WriteByte('\n')
	}

	return builder.String()
}

func parentGoToolsSourceFixture(t *testing.T, ethosRoot string) string {
	t.Helper()

	sourceRoot := filepath.Join(ethosRoot, "go")
	oldTime := time.Now().Add(-3 * time.Hour)

	for _, tool := range parentGoToolFixtureCommands() {
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

func parentBuildableGoToolsSourceFixture(t *testing.T, ethosRoot string) string {
	t.Helper()

	sourceRoot := filepath.Join(ethosRoot, "go")

	err := os.MkdirAll(sourceRoot, 0o755)
	if err != nil {
		t.Fatalf("create Go source root: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(sourceRoot, "go.mod"),
		[]byte("module example.test/coding-ethos\n\ngo 1.26\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	for _, tool := range parentGoToolFixtureCommands() {
		mainPath := filepath.Join(sourceRoot, "cmd", tool, "main.go")

		err = os.MkdirAll(filepath.Dir(mainPath), 0o755)
		if err != nil {
			t.Fatalf("create source dir: %v", err)
		}

		err = os.WriteFile(
			mainPath,
			[]byte("package main\n\nfunc main() {}\n"),
			0o600,
		)
		if err != nil {
			t.Fatalf("write source: %v", err)
		}
	}

	return sourceRoot
}

func assertProtectedBranchWorkPoliciesPresent(t *testing.T, bundlePath string) {
	t.Helper()

	bundle := readPolicyBundleForTest(t, bundlePath)

	for _, policyID := range []string{
		"git.checkout_protected_branch",
		"filesystem.protected_branch_write",
	} {
		if _, found := bundle.Policies[policyID]; !found {
			t.Fatalf("%s should remain enabled in %s", policyID, bundlePath)
		}
	}
}

func readPolicyBundleForTest(t *testing.T, bundlePath string) policy.Bundle {
	t.Helper()

	file, err := os.Open(bundlePath)
	if err != nil {
		t.Fatalf("open policy bundle %s: %v", bundlePath, err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		t.Fatalf("decode policy bundle %s: %v", bundlePath, err)
	}

	return bundle
}

func writePolicyBundleForTest(t *testing.T, bundlePath string) {
	t.Helper()

	file, err := os.OpenFile(bundlePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create policy bundle %s: %v", bundlePath, err)
	}
	defer file.Close()

	err = policy.EncodeBundle(file, policy.ExampleBundle())
	if err != nil {
		t.Fatalf("encode policy bundle %s: %v", bundlePath, err)
	}
}

func runRunnerTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = dir

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func findRunnerRepoRoot(t *testing.T) string {
	t.Helper()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		_, err = os.Stat(filepath.Join(workingDir, "coding_ethos.yml"))
		if err == nil {
			return workingDir
		}

		parent := filepath.Dir(workingDir)
		if parent == workingDir {
			t.Fatalf("could not find repo root from %s", workingDir)
		}

		workingDir = parent
	}
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

	for _, tool := range parentGoToolFixtureCommands() {
		toolPath := filepath.Join(paths.BinDir, tool)
		writeExecutableFixture(t, toolPath, "#!/usr/bin/env sh\nexit 0\n")

		err := os.Chtimes(toolPath, modTime, modTime)
		if err != nil {
			t.Fatalf("touch tool %s: %v", tool, err)
		}
	}
}

func parentGoToolFixtureCommands() []string {
	return []string{
		"coding-ethos-lint",
		"coding-ethos-policy",
		"coding-ethos-run",
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

func (stub stubRuntimeOps) execAgentShell(_ runtimePaths, command string) {
	*stub.calls = append(*stub.calls, "agent-shell:"+command)
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
