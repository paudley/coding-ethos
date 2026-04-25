// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateGitCommitHeadAdvancedBlocksUnchangedHead(t *testing.T) {
	repo := initCommitHeadRepo(t)
	policyDef := policy.ExampleBundle().Policies["git.commit_head_advanced"]
	context := Context{
		Scope:   "PreToolUse",
		Argv:    []string{"git", "commit", "-m", "test"},
		Command: "git commit -m test",
		Cwd:     repo,
	}
	if _, err := EvaluateGitCommitHeadAdvanced(policyDef, context); err != nil {
		t.Fatalf("record head: %v", err)
	}
	context.Scope = "PostToolUse"
	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify head: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
	if decisions[0].Decision != "block" {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}
}

func TestEvaluateGitCommitHeadAdvancedRecordsAdvancedHead(t *testing.T) {
	repo := initCommitHeadRepo(t)
	policyDef := policy.ExampleBundle().Policies["git.commit_head_advanced"]
	context := Context{
		Scope:   "PreToolUse",
		Argv:    []string{"git", "commit", "-m", "test"},
		Command: "git commit -m test",
		Cwd:     repo,
	}
	if _, err := EvaluateGitCommitHeadAdvanced(policyDef, context); err != nil {
		t.Fatalf("record head: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatalf("write change: %v", err)
	}
	runGit(t, repo, "add", "file.txt")
	runGit(t, repo, "commit", "-m", "second")
	context.Scope = "PostToolUse"
	decisions, err := EvaluateGitCommitHeadAdvanced(policyDef, context)
	if err != nil {
		t.Fatalf("verify head: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
	if decisions[0].Decision != "record" {
		t.Fatalf("decision mismatch: %#v", decisions[0])
	}
}

func initCommitHeadRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGit(t, repo, "add", "file.txt")
	runGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
