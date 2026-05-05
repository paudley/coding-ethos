// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
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
	if err := encodeLintResultTo(&output, result); err != nil {
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
	if err := os.WriteFile(runner, []byte("#!/usr/bin/env sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write runner: %v", err)
	}
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
	if err := os.WriteFile(messagePath, []byte("fix(test): valid subject\n"), 0o600); err != nil {
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
			args: []string{"--bundle", bundlePath, "--runner", runner, "--cwd", repo, "commit-msg", messagePath},
			want: 0,
		},
		{
			name: "pre push allowed then legacy runner",
			args: []string{"--bundle", bundlePath, "--runner", runner, "--cwd", repo, "pre-push"},
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
	if err := os.WriteFile(
		runner,
		[]byte("#!/bin/sh\nexit 7\n"),
		0o700,
	); err != nil {
		t.Fatalf("write runner: %v", err)
	}

	if status := runLegacyRunner(runner, []string{"pre-commit"}); status != 7 {
		t.Fatalf("status = %d, want 7", status)
	}
}

func TestRunLegacyRunnerReportsLaunchFailure(t *testing.T) {
	t.Parallel()

	if status := runLegacyRunner(filepath.Join(t.TempDir(), "missing"), []string{"pre-commit"}); status != 1 {
		t.Fatalf("status = %d, want 1", status)
	}
}

func runTestGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	command := exec.CommandContext(context.Background(), "git", args...)
	command.Dir = repo
	command.Env = cleanTestGitEnv()

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
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
