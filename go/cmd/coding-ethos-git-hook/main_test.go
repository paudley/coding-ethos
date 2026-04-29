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
		Scope:  lint.ScopeCommit,
		Status: "blocked",
		Files:  []string{"/tmp/COMMIT_EDITMSG"},
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
		"findings[1]{tool,file,line,column,severity,code,policy_id,message,advice,detail}:",
		"pii,.codex/config.toml,8,0,block,,repo.pii_scrubber,local machine detail detected,Replace local paths with generic placeholders.,matched /" + "home/example/project",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("encoded output missing %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, `"decisions"`) || strings.Contains(rendered, "{\n") {
		t.Fatalf("encoded output regressed to raw JSON:\n%s", rendered)
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
