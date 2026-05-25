// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package githookcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/testlock"
)

func TestMain(m *testing.M) {
	if os.Getenv("CODING_ETHOS_GIT_HOOK_HELPER") != "1" {
		os.Exit(m.Run())
	}

	if os.Getenv("CODING_ETHOS_GIT_HOOK_HELPER_ACTIVE") == "1" {
		fmt.Fprintln(os.Stderr, "FATAL: recursive git hook helper invocation blocked")
		os.Exit(96)
	}

	_ = os.Setenv("CODING_ETHOS_GIT_HOOK_HELPER_ACTIVE", "1")

	for index, arg := range os.Args {
		if arg == "--" {
			os.Exit(runWithArgs(os.Args[index+1:]))
		}
	}

	os.Exit(1)
}

func TestHookFilesReturnsStagedPreCommitFiles(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")
	runTestGit(t, repo, "config", "commit.gpgsign", "false")

	err := os.WriteFile(filepath.Join(repo, "tracked.txt"), []byte("x\n"), 0o600)
	if err != nil {
		t.Fatalf("write tracked file: %v", err)
	}

	runTestGit(t, repo, "add", "tracked.txt")

	files, err := hookFiles(repo, "pre-commit")
	if err != nil {
		t.Fatalf("hook files: %v", err)
	}

	if !slices.Contains(files, "tracked.txt") {
		t.Fatalf("missing staged file: %#v", files)
	}
}

func TestDirectRealGitCommitAllowsHumanGitThroughInstalledHooks(t *testing.T) {
	clearAgentGitHookEnv(t)

	repo := newGitHookE2ERepo(t)
	writeTestGitHookFile(t, repo.root, "README.md", "# Test\n")
	runTestGit(t, repo.root, "add", "README.md")

	output, err := runTestGitOutput(
		t,
		repo.root,
		"commit",
		"-m",
		"fix(repo): add readme",
	)
	if err != nil {
		t.Fatalf("direct human git commit failed:\n%s", output)
	}

	if strings.Contains(output, "direct git execution reached coding-ethos hooks") {
		t.Fatalf("human git output included agent direct-git block:\n%s", output)
	}
}

func TestDirectRealGitCommitAllowsPersistentAgentConfigEnv(t *testing.T) {
	clearAgentGitHookEnv(t)
	t.Setenv("CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1")

	repo := newGitHookE2ERepo(t)
	writeTestGitHookFile(t, repo.root, "README.md", "# Test\n")
	runTestGit(t, repo.root, "add", "README.md")

	output, err := runTestGitOutputWithEnv(
		t,
		repo.root,
		[]string{
			"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
		},
		"commit",
		"-m",
		"fix(repo): add readme",
	)
	if err != nil {
		t.Fatalf("direct human git commit failed with ambient tool env:\n%s", output)
	}

	if strings.Contains(output, "direct git execution reached coding-ethos hooks") {
		t.Fatalf("persistent agent config env triggered direct-git block:\n%s", output)
	}
}

func TestDirectAgentGitCommitBlocksThroughInstalledHooks(t *testing.T) {
	clearAgentGitHookEnv(t)
	t.Setenv("CODEX_THREAD_ID", "thread")

	repo := newGitHookE2ERepo(t)
	writeTestGitHookFile(t, repo.root, "README.md", "# Test\n")
	runTestGit(t, repo.root, "add", "README.md")

	output, err := runTestGitOutputWithEnv(
		t,
		repo.root,
		[]string{"CODEX_THREAD_ID=thread"},
		"commit",
		"-m",
		"fix(repo): add readme",
	)
	if err == nil {
		t.Fatalf("direct agent git commit unexpectedly succeeded:\n%s", output)
	}

	if !strings.Contains(output, "direct git execution reached coding-ethos hooks") {
		t.Fatalf("direct agent git output missing hostile-path block:\n%s", output)
	}

	for _, want := range []string{
		"provider: codex",
		"policy_id: git.wrapper_required",
		"Codex must retry the original git command through cerun",
		"/bin/cerun -- 'git commit <same arguments>'",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("direct Codex git output missing %q:\n%s", want, output)
		}
	}
}

func clearAgentGitHookEnv(t *testing.T) {
	t.Helper()

	for _, name := range agentGitHookEnvMarkers() {
		t.Setenv(name, "")
	}
}

func TestWrapperAuthorizedCommitMsgKeepsOriginalPolicyFinding(t *testing.T) {
	verifier := authorizeGitHookForTest(t)

	fixture := setupGitHookValidationFixture(t)
	fixture.bundlePath = writeCompiledPolicyBundleFile(t, fixture.repo)
	err := os.WriteFile(fixture.messagePath, []byte("bad\n"), 0o600)
	if err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	var got int
	output := captureGitStderr(t, func() {
		got = runWithArgsAndVerifier([]string{
			"--bundle",
			fixture.bundlePath,
			"--runner",
			fixture.runner,
			"--cwd",
			fixture.repo,
			"commit-msg",
			fixture.messagePath,
		}, verifier)
	})

	if got != blockedExitCode {
		t.Fatalf("runWithArgs() = %d, want %d\n%s", got, blockedExitCode, output)
	}

	for _, want := range []string{
		"trace_id:",
		"git.commitlint",
		"Commit messages must follow the configured conventional format.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("commit output missing %q:\n%s", want, output)
		}
	}
}

func TestWrapperAuthorizedPrepareCommitMsgBlocksHistoryRewrite(t *testing.T) {
	verifier := authorizeGitHookForTest(t)
	fixture := setupGitHookValidationFixture(t)

	var got int
	output := captureGitStderr(t, func() {
		got = runWithArgsAndVerifier([]string{
			"--bundle",
			fixture.bundlePath,
			"--runner",
			fixture.runner,
			"--cwd",
			fixture.repo,
			"prepare-commit-msg",
			fixture.messagePath,
			"commit",
			"abc123",
		}, verifier)
	})

	if got != blockedExitCode {
		t.Fatalf("runWithArgs() = %d, want %d\n%s", got, blockedExitCode, output)
	}

	for _, want := range []string{
		"git.history_rewrite_prevention",
		"History rewrite commit flow reached prepare-commit-msg.",
		"source=\"commit\"",
		"commit=\"abc123\"",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("prepare-commit-msg output missing %q:\n%s", want, output)
		}
	}
}

func TestRunWithArgsBlocksHookWithoutWrapperAuthorization(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "thread")
	fixture := setupGitHookValidationFixture(t)

	output := captureGitStderr(t, func() {
		got := runWithArgs([]string{
			"--bundle",
			fixture.bundlePath,
			"--runner",
			fixture.runner,
			"--cwd",
			fixture.repo,
			"commit-msg",
			fixture.messagePath,
		})
		if got != blockedExitCode {
			t.Fatalf("runWithArgs() = %d, want %d", got, blockedExitCode)
		}
	})

	if !strings.Contains(output, "direct git execution reached coding-ethos hooks") {
		t.Fatalf("direct hook output missing hostile-path block:\n%s", output)
	}
}

func TestRunWithArgsRejectsSpoofedWrapperAuthorization(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "thread")
	t.Setenv(gitwrap.WrapperAuthorizedEnv, "1")
	t.Setenv(gitwrap.WrapperPIDEnv, "12345")
	verifier := gitHookAuthorizationVerifier{
		CurrentPID: func() int { return 67890 },
		ProcessAncestryContains: func(pid, ancestorPID int) (bool, error) {
			return pid == 67890 && ancestorPID == 12345, nil
		},
		ProcessCommandLine: func(pid int) ([]string, error) {
			if pid != 12345 {
				t.Fatalf("processCommandLine pid = %d, want 12345", pid)
			}

			return []string{"/usr/bin/git", "commit"}, nil
		},
	}

	fixture := setupGitHookValidationFixture(t)
	output := captureGitStderr(t, func() {
		got := runWithArgsAndVerifier([]string{
			"--bundle",
			fixture.bundlePath,
			"--runner",
			fixture.runner,
			"--cwd",
			fixture.repo,
			"commit-msg",
			fixture.messagePath,
		}, verifier)
		if got != blockedExitCode {
			t.Fatalf("runWithArgs() = %d, want %d", got, blockedExitCode)
		}
	})

	if !strings.Contains(output, "direct git execution reached coding-ethos hooks") {
		t.Fatalf("spoofed hook output missing hostile-path block:\n%s", output)
	}
}

func TestTrustedGitWrapperProcessAcceptsOnlyManagedEntrypoints(t *testing.T) {
	t.Parallel()

	for _, commandLine := range [][]string{
		{"/repo/bin/coding-ethos-git", "commit"},
		{"/repo/bin/coding-ethos-run", "policy-git", "commit"},
	} {
		if !trustedGitWrapperProcess(commandLine) {
			t.Fatalf("trustedGitWrapperProcess(%#v) = false", commandLine)
		}
	}

	for _, commandLine := range [][]string{
		nil,
		{"/usr/bin/git", "commit"},
		{"/repo/bin/coding-ethos-run", "pre-commit"},
		{"/repo/bin/coding-ethos-run", "git-hook", "pre-push"},
	} {
		if trustedGitWrapperProcess(commandLine) {
			t.Fatalf("trustedGitWrapperProcess(%#v) = true", commandLine)
		}
	}
}

func TestHookFilesSkipsNonPreCommitHooks(t *testing.T) {
	t.Parallel()

	files, err := hookFiles(t.TempDir(), "pre-push")
	if err != nil {
		t.Fatalf("hook files: %v", err)
	}

	if len(files) != 0 {
		t.Fatalf("pre-push should not resolve staged files: %#v", files)
	}
}

func TestAdminApprovedFallsBackToProcessAncestry(t *testing.T) {
	t.Setenv(adminApprovedEnv, "")

	verifier := func(cwd string) error {
		if cwd != "/repo" {
			t.Fatalf("cwd = %q, want /repo", cwd)
		}

		return nil
	}

	if !adminApprovedWithVerifier("/repo", verifier) {
		t.Fatal("adminApproved should accept approved process ancestry")
	}
}

func TestAdminApprovedAcceptsEnvironmentMarker(t *testing.T) {
	t.Setenv(adminApprovedEnv, "1")

	verifier := func(string) error {
		t.Fatal("environment marker should avoid process ancestry lookup")

		return nil
	}

	if !adminApprovedWithVerifier("/repo", verifier) {
		t.Fatal("adminApproved should accept explicit environment marker")
	}
}

func TestBlockedOnlyResultDropsResolvedPolicyRecords(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		TraceID: "trace-commit.json",
		Scope:   lint.ScopeCommit,
		Status:  "blocked",
		Files:   []string{"/tmp/COMMIT_EDITMSG"},
		Decisions: []policy.Decision{
			{
				PolicyID: "git.commitlint",
				Decision: "block",
				Severity: "block",
				Message:  "bad commit",
			},
			{
				PolicyID: "git.commit_attribution",
				Decision: "record",
				Severity: "record",
				Message:  "clean attribution",
			},
		},
	}

	filtered := blockedOnlyResult(result)
	if len(filtered.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", filtered.Decisions)
	}

	if filtered.TraceID != "trace-commit.json" {
		t.Fatalf("filtered result lost trace ID: %#v", filtered)
	}

	if filtered.Decisions[0].PolicyID != "git.commitlint" {
		t.Fatalf("unexpected decision: %#v", filtered.Decisions[0])
	}

	if len(filtered.Files) != 0 {
		t.Fatalf("filtered result leaked input files: %#v", filtered.Files)
	}
}

func TestEncodeLintResultToUsesTOONForAgentEnvironment(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "auto")
	t.Setenv("CODEX_THREAD_ID", "thread")

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Decisions: []policy.Decision{{
			PolicyID: "repo.pii_scrubber",
			Decision: "block",
			Severity: "block",
			Message:  "Local-machine PII must not be committed.",
			Diagnostics: []diagnostics.Diagnostic{{
				Tool:     "pii",
				File:     ".codex/config.toml",
				Line:     8,
				Severity: "block",
				PolicyID: "repo.pii_scrubber",
				Message:  "local machine detail detected",
				Advice:   "Replace local paths with generic placeholders.",
				Detail:   "matched /" + "home/example/project",
			}},
		}},
	}

	var output bytes.Buffer

	err := encodeLintResultTo(&output, result)
	if err != nil {
		t.Fatalf("encode lint result: %v", err)
	}

	rendered := output.String()
	for _, want := range []string{
		"tool: policy-lint",
		"trace_id: ",
		"findings[1]{tool,file,line,column,severity,code," +
			"policy_id,skill_id,message,advice,detail}:",
		"pii,.codex/config.toml,8,0,block,,repo.pii_scrubber,," +
			"local machine detail detected,Replace local paths with generic placeholders.," +
			"matched /" + "home/example/project",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("encoded output missing %q:\n%s", want, rendered)
		}
	}

	if strings.Contains(rendered, `"decisions"`) || strings.Contains(rendered, "{\n") {
		t.Fatalf("encoded output regressed to raw JSON:\n%s", rendered)
	}
}

func TestGitHookReadBundleRoundTripsExample(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "policy-bundle.json")

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	inlineErr0 := policy.EncodeBundle(file, policy.ExampleBundle())
	if inlineErr0 != nil {
		t.Fatalf("encode bundle: %v", inlineErr0)
	}

	inlineErr1 := file.Close()
	if inlineErr1 != nil {
		t.Fatalf("close bundle: %v", inlineErr1)
	}

	bundle, err := readBundle(path)
	if err != nil {
		t.Fatalf("read bundle: %v", err)
	}

	if bundle.BundleID != policy.ExampleBundle().BundleID {
		t.Fatalf("bundle id = %q", bundle.BundleID)
	}
}

func TestRunWithArgsHandlesValidationAndAllowedHooks(t *testing.T) {
	verifier := authorizeGitHookForTest(t)

	fixture := setupGitHookValidationFixture(t)

	for _, test := range gitHookValidationCases(fixture) {
		if got := runWithArgsAndVerifier(test.args, verifier); got != test.want {
			t.Fatalf("%s: runWithArgs() = %d, want %d", test.name, got, test.want)
		}
	}
}

func authorizeGitHookForTest(t *testing.T) gitHookAuthorizationVerifier {
	t.Helper()

	t.Setenv(gitwrap.WrapperAuthorizedEnv, "1")
	t.Setenv(gitwrap.WrapperPIDEnv, "12345")
	return gitHookAuthorizationVerifier{
		CurrentPID: func() int { return 67890 },
		ProcessAncestryContains: func(pid, ancestorPID int) (bool, error) {
			return pid == 67890 && ancestorPID == 12345, nil
		},
		ProcessCommandLine: func(pid int) ([]string, error) {
			if pid != 12345 {
				t.Fatalf("processCommandLine pid = %d, want 12345", pid)
			}

			return []string{"/repo/bin/coding-ethos-run", "policy-git", "commit"}, nil
		},
	}
}

type gitHookValidationFixture struct {
	bundlePath  string
	messagePath string
	repo        string
	runner      string
}

func setupGitHookValidationFixture(t *testing.T) gitHookValidationFixture {
	t.Helper()

	repo := t.TempDir()
	runner := filepath.Join(repo, "runner")
	writeExecutableTestScript(t, runner, "#!/usr/bin/env sh\nexit 0\n")

	bundlePath := writeExampleBundleFile(t, repo)
	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")

	inlineErr4 := os.WriteFile(
		messagePath,
		[]byte("fix(test): valid subject\n"),
		0o600,
	)
	if inlineErr4 != nil {
		t.Fatalf("write commit message: %v", inlineErr4)
	}

	return gitHookValidationFixture{
		bundlePath:  bundlePath,
		messagePath: messagePath,
		repo:        repo,
		runner:      runner,
	}
}

func gitHookValidationCases(
	fixture gitHookValidationFixture,
) []struct {
	name string
	args []string
	want int
} {
	return []struct {
		name string
		args []string
		want int
	}{
		{
			name: "missing bundle",
			args: nil,
			want: 1,
		},
		{
			name: "missing runner",
			args: []string{"--bundle", fixture.bundlePath},
			want: 1,
		},
		{
			name: "missing hook",
			args: []string{
				"--bundle",
				fixture.bundlePath,
				"--runner",
				fixture.runner,
			},
			want: 1,
		},
		{
			name: "commit message allowed",
			args: []string{
				"--bundle",
				fixture.bundlePath,
				"--runner",
				fixture.runner,
				"--cwd",
				fixture.repo,
				"commit-msg",
				fixture.messagePath,
			},
			want: 0,
		},
		{
			name: "commit message allowed without cwd",
			args: []string{
				"--bundle",
				fixture.bundlePath,
				"--runner",
				fixture.runner,
				"commit-msg",
				fixture.messagePath,
			},
			want: 0,
		},
		{
			name: "prepare commit message allowed for normal source",
			args: []string{
				"--bundle",
				fixture.bundlePath,
				"--runner",
				fixture.runner,
				"--cwd",
				fixture.repo,
				"prepare-commit-msg",
				fixture.messagePath,
				"message",
			},
			want: 0,
		},
	}
}

func writeExampleBundleFile(t *testing.T, repo string) string {
	t.Helper()

	bundlePath := filepath.Join(repo, "policy-bundle.json")

	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	inlineErr := policy.EncodeBundle(file, policy.ExampleBundle())
	if inlineErr != nil {
		t.Fatalf("encode bundle: %v", inlineErr)
	}

	inlineErr = file.Close()
	if inlineErr != nil {
		t.Fatalf("close bundle: %v", inlineErr)
	}

	return bundlePath
}

func writeCompiledPolicyBundleFile(t *testing.T, repo string) string {
	t.Helper()

	bundlePath := filepath.Join(repo, "policy-bundle.json")
	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	bundle := compileRepoPolicyBundle(t)

	err = policy.EncodeBundle(file, bundle)
	if err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("close bundle: %v", err)
	}

	return bundlePath
}

func TestRunHookGroupRunnerReturnsExitStatus(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "githookcli-runHookGroupRunner-env")

	if status := runHookGroupRunner(t.TempDir(), []string{"pre-commit"}); status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
}

func TestRunHookGroupRunnerReportsLaunchFailure(t *testing.T) {
	t.Parallel()
	testlock.ProcessState(t, "githookcli-runHookGroupRunner-env")

	if status := runHookGroupRunner(t.TempDir(), []string{"pre-commit"}); status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
}

type gitHookE2ERepo struct {
	root string
}

func newGitHookE2ERepo(t *testing.T) gitHookE2ERepo {
	t.Helper()

	repo := t.TempDir()
	runTestGit(t, repo, "init")
	runTestGit(t, repo, "config", "user.email", "test@example.com")
	runTestGit(t, repo, "config", "user.name", "Test User")
	runTestGit(t, repo, "config", "commit.gpgsign", "false")
	writeTestGitHookFile(
		t,
		repo,
		".gitignore",
		".code-ethos/cache/\n.coding-ethos/cache/\n.coding-ethos/code-intel.db\n"+
			".coding-ethos/code-intel.db-shm\n.coding-ethos/code-intel.db-wal\n"+
			".coding-ethos/code-intel.duckdb\n.coding-ethos/code-intel.duckdb.wal\n"+
			".coding-ethos/events/\n"+
			".coding-ethos/hook-runs/\n.coding-ethos/lint-runs/\n"+
			".coding-ethos/prune-runs/\n.coding-ethos/state/\nbin/\nbuild/\n",
	)
	writeTestManifest(t, repo)
	writeTestRepoConfig(t, repo)
	runTestGit(t, repo, "add", ".gitignore")
	runTestGit(t, repo, "add", "manifest.yaml")
	runTestGit(t, repo, "add", "repo_config.yaml")
	runTestGit(t, repo, "commit", "-m", "chore(repo): initialize fixture")

	bundlePath := filepath.Join(repo, "policy-bundle.json")
	bundle := compileRepoPolicyBundle(t)

	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	inlineErr5 := policy.EncodeBundle(file, bundle)
	if inlineErr5 != nil {
		t.Fatalf("encode bundle: %v", inlineErr5)
	}

	inlineErr6 := file.Close()
	if inlineErr6 != nil {
		t.Fatalf("close bundle: %v", inlineErr6)
	}

	writeGitHookHelper(t, repo, "pre-commit", bundlePath)
	writeGitHookHelper(t, repo, "commit-msg", bundlePath)
	writeGitHookHelper(t, repo, "prepare-commit-msg", bundlePath)

	return gitHookE2ERepo{root: repo}
}

func compileRepoPolicyBundle(t *testing.T) policy.Bundle {
	t.Helper()

	root := findRepoRoot(t)

	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary: filepath.Join(root, "coding_ethos.yml"),
		Config:  filepath.Join(root, "config.yaml"),
	})
	if err != nil {
		t.Fatalf("compile policy bundle: %v", err)
	}

	return bundle
}

func findRepoRoot(t *testing.T) string {
	t.Helper()

	workingDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		_, inlineErrAutoA := os.Stat(filepath.Join(workingDir, "coding_ethos.yml"))
		if inlineErrAutoA == nil {
			return workingDir
		}

		parent := filepath.Dir(workingDir)
		if parent == workingDir {
			t.Fatal("could not locate repository root")
		}

		workingDir = parent
	}
}

func writeGitHookHelper(t *testing.T, repo, name, bundlePath string) {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test executable: %v", err)
	}

	hookPath := filepath.Join(repo, ".git", "hooks", name)

	script := fmt.Sprintf(
		"#!/bin/sh\n"+
			"CODING_ETHOS_GIT_HOOK_HELPER=1 CODE_ETHOS_HOOK_OUTPUT_FORMAT=toon "+
			"CODE_ETHOS_PRECOMMIT_ROOT=%q "+
			"CODE_ETHOS_PRECOMMIT_CONFIG=%q "+
			"exec %q -test.run '^$' -- "+
			"--bundle %q --runner /bin/true --cwd %q %s \"$@\"\n",
		filepath.Join(findRepoRoot(t), "pre-commit"),
		filepath.Join(repo, "repo_config.yaml"),
		executable,
		bundlePath,
		repo,
		name,
	)

	inlineErr7 := os.WriteFile(hookPath, []byte(script), 0o600)
	if inlineErr7 != nil {
		t.Fatalf("write %s hook: %v", name, inlineErr7)
	}

	inlineErr8 := os.Chmod(hookPath, 0o700)
	if inlineErr8 != nil {
		t.Fatalf("chmod %s hook: %v", name, inlineErr8)
	}
}

func writeTestRepoConfig(t *testing.T, repo string) string {
	t.Helper()

	path := filepath.Join(repo, "repo_config.yaml")
	writeTestGitHookFile(t, repo, "repo_config.yaml", fmt.Sprintf(
		"hooks:\n"+
			"  enabled_groups:\n"+
			"    - format\n"+
			"python:\n"+
			"  manifest_validation:\n"+
			"    enabled: true\n"+
			"    candidate_paths:\n"+
			"      - %q\n",
		filepath.Join(repo, "manifest.yaml"),
	))

	return path
}

func writeTestManifest(t *testing.T, repo string) {
	t.Helper()

	writeTestGitHookFile(t, repo, "manifest.yaml", strings.TrimSpace(`
version: "1"
symlinks:
  - source: README.md
    target: docs/README.md
`)+"\n")
}

func writeTestGitHookFile(t *testing.T, repo, path, content string) {
	t.Helper()

	fullPath := filepath.Join(repo, path)

	err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
	if err != nil {
		t.Fatalf("create parent directory: %v", err)
	}

	err = os.WriteFile(fullPath, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeExecutableTestScript(t *testing.T, path, content string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("create executable script %s: %v", path, err)
	}

	_, inlineErrB := file.WriteString(content)
	if inlineErrB != nil {
		_ = file.Close()

		t.Fatalf("write executable script %s: %v", path, inlineErrB)
	}

	inlineErr8 := file.Sync()
	if inlineErr8 != nil {
		_ = file.Close()

		t.Fatalf("sync executable script %s: %v", path, inlineErr8)
	}

	inlineErr9 := file.Close()
	if inlineErr9 != nil {
		t.Fatalf("close executable script %s: %v", path, inlineErr9)
	}

	inlineErr10 := os.Chmod(path, 0o700)
	if inlineErr10 != nil {
		t.Fatalf("chmod executable script %s: %v", path, inlineErr10)
	}
}

func captureGitStderr(t *testing.T, run func()) string {
	t.Helper()
	testlock.ProcessState(t, "githookcli-stderr")

	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}

	os.Stderr = writer
	defer func() {
		os.Stderr = original
	}()

	run()

	return readGitPipe(t, reader, writer)
}

func readGitPipe(t *testing.T, reader, writer *os.File) string {
	t.Helper()

	err := writer.Close()
	if err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}

	var buffer bytes.Buffer
	_, err = io.Copy(&buffer, reader)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}

	err = reader.Close()
	if err != nil {
		t.Fatalf("close pipe reader: %v", err)
	}

	return buffer.String()
}

func runTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	output, err := runTestGitOutput(t, repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}

func runTestGitOutputOK(t *testing.T, repo string, args ...string) string {
	t.Helper()

	output, err := runTestGitOutput(t, repo, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}

	return output
}

func runTestGitOutput(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()

	return runTestGitOutputWithEnv(t, repo, nil, args...)
}

func runTestGitOutputWithEnv(
	t *testing.T,
	repo string,
	extraEnv []string,
	args ...string,
) (string, error) {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	command := exec.CommandContext(context.Background(), gitPath, args...)
	command.Dir = repo
	command.Env = append(cleanTestGitEnv(), extraEnv...)

	output, err := command.CombinedOutput()

	return string(output), err
}

func cleanTestGitEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		name, _, found := strings.Cut(item, "=")
		if found && (name == gitwrap.WrapperAuthorizedEnv || name == gitwrap.WrapperPIDEnv) {
			continue
		}
		if found && slices.Contains(agentGitHookEnvMarkers(), name) {
			continue
		}

		switch {
		case item == "GIT_DIR" || item == "GIT_WORK_TREE":
			continue
		case len(item) > len("GIT_DIR=") && item[:len("GIT_DIR=")] == "GIT_DIR=":
			continue
		case len(item) > len("GIT_WORK_TREE=") &&
			item[:len("GIT_WORK_TREE=")] == "GIT_WORK_TREE=":
			continue
		default:
			env = append(env, item)
		}
	}

	return env
}
