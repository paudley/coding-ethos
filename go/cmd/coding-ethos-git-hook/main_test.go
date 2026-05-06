// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestGitHookHelperProcess(t *testing.T) {
	if os.Getenv("CODING_ETHOS_GIT_HOOK_HELPER") != "1" {
		return
	}

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

func TestRealGitCommitSucceedsThroughInstalledHooks(t *testing.T) {
	t.Parallel()

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
		t.Fatalf("git commit failed:\n%s\n%v", output, err)
	}

	head := strings.TrimSpace(runTestGitOutputOK(t, repo.root, "rev-parse", "HEAD"))
	if head == "" {
		t.Fatal("HEAD did not advance")
	}

	if _, err := os.Stat(
		filepath.Join(repo.root, ".coding-ethos", "lint-runs"),
	); err != nil {
		t.Fatalf("missing lint trace directory: %v", err)
	}
}

func TestRealGitCommitFailureKeepsOriginalPolicyFinding(t *testing.T) {
	t.Parallel()

	repo := newGitHookE2ERepo(t)
	writeTestGitHookFile(t, repo.root, "README.md", "# Test\n")
	runTestGit(t, repo.root, "add", "README.md")

	output, err := runTestGitOutput(t, repo.root, "commit", "-m", "bad")
	if err == nil {
		t.Fatalf("git commit unexpectedly succeeded:\n%s", output)
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

	if strings.Contains(output, "git.commit_head_advanced") {
		t.Fatalf("commit bookkeeping masked original failure:\n%s", output)
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

	original := verifyAdminApproved
	verifyAdminApproved = func(cwd string) error {
		if cwd != "/repo" {
			t.Fatalf("cwd = %q, want /repo", cwd)
		}

		return nil
	}

	t.Cleanup(func() {
		verifyAdminApproved = original
	})

	if !adminApproved("/repo") {
		t.Fatal("adminApproved should accept approved process ancestry")
	}
}

func TestAdminApprovedAcceptsEnvironmentMarker(t *testing.T) {
	t.Setenv(adminApprovedEnv, "1")

	original := verifyAdminApproved
	verifyAdminApproved = func(string) error {
		t.Fatal("environment marker should avoid process ancestry lookup")

		return nil
	}

	t.Cleanup(func() {
		verifyAdminApproved = original
	})

	if !adminApproved("/repo") {
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
		"format: toon",
		"tool: policy-lint",
		"trace_id: ",
		"findings[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
		"pii,.codex/config.toml,8,0,block,,repo.pii_scrubber,,local machine detail detected,Replace local paths with generic placeholders.,matched /" + "home/example/project",
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

	if err := policy.EncodeBundle(file, policy.ExampleBundle()); err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close bundle: %v", err)
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
	repo := t.TempDir()
	runner := filepath.Join(repo, "runner")
	writeExecutableTestScript(t, runner, "#!/usr/bin/env sh\nexit 0\n")

	bundlePath := filepath.Join(repo, "policy-bundle.json")

	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	if err := policy.EncodeBundle(file, policy.ExampleBundle()); err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close bundle: %v", err)
	}

	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")
	if err := os.WriteFile(
		messagePath,
		[]byte("fix(test): valid subject\n"),
		0o600,
	); err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	tests := []struct {
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
			args: []string{"--bundle", bundlePath},
			want: 1,
		},
		{
			name: "missing hook",
			args: []string{"--bundle", bundlePath, "--runner", runner},
			want: 1,
		},
		{
			name: "commit message allowed",
			args: []string{
				"--bundle",
				bundlePath,
				"--runner",
				runner,
				"--cwd",
				repo,
				"commit-msg",
				messagePath,
			},
			want: 0,
		},
		{
			name: "pre push allowed then legacy runner",
			args: []string{
				"--bundle",
				bundlePath,
				"--runner",
				runner,
				"--cwd",
				repo,
				"pre-push",
			},
			want: 0,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := runWithArgs(test.args); got != test.want {
				t.Fatalf("runWithArgs() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestRunLegacyRunnerReturnsExitStatus(t *testing.T) {
	t.Parallel()

	runner := filepath.Join(t.TempDir(), "runner")
	writeExecutableTestScript(t, runner, "#!/bin/sh\nexit 7\n")

	if status := runLegacyRunner(runner, []string{"pre-commit"}); status != 7 {
		t.Fatalf("status = %d, want 7", status)
	}
}

func TestRunLegacyRunnerReportsLaunchFailure(t *testing.T) {
	t.Parallel()

	if status := runLegacyRunner(
		filepath.Join(t.TempDir(), "missing"),
		[]string{"pre-commit"},
	); status != 1 {
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
		".code-ethos/cache/\n.coding-ethos/\nbin/\nbuild/\n",
	)
	runTestGit(t, repo, "add", ".gitignore")
	runTestGit(t, repo, "commit", "-m", "chore(repo): initialize fixture")

	bundlePath := filepath.Join(repo, "policy-bundle.json")
	bundle := compileRepoPolicyBundle(t)

	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("create bundle: %v", err)
	}

	if err := policy.EncodeBundle(file, bundle); err != nil {
		t.Fatalf("encode bundle: %v", err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close bundle: %v", err)
	}

	writeGitHookHelper(t, repo, "pre-commit", bundlePath)
	writeGitHookHelper(t, repo, "commit-msg", bundlePath)

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

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(wd, "coding_ethos.yml")); err == nil {
			return wd
		}

		parent := filepath.Dir(wd)
		if parent == wd {
			t.Fatal("could not locate repository root")
		}

		wd = parent
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
			"exec %q -test.run '^TestGitHookHelperProcess$' -- "+
			"--bundle %q --runner /bin/true --cwd %q %s \"$@\"\n",
		executable,
		bundlePath,
		repo,
		name,
	)
	if err := os.WriteFile(hookPath, []byte(script), 0o700); err != nil {
		t.Fatalf("write %s hook: %v", name, err)
	}
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

	if _, err := file.WriteString(content); err != nil {
		_ = file.Close()

		t.Fatalf("write executable script %s: %v", path, err)
	}

	if err := file.Sync(); err != nil {
		_ = file.Close()

		t.Fatalf("sync executable script %s: %v", path, err)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close executable script %s: %v", path, err)
	}

	if err := os.Chmod(path, 0o700); err != nil {
		t.Fatalf("chmod executable script %s: %v", path, err)
	}
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

	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = repo
	command.Env = cleanTestGitEnv()

	output, err := command.CombinedOutput()

	return string(output), err
}

func cleanTestGitEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
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
