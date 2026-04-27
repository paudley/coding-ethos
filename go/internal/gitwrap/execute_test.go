// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const statusBlocked = "blocked"

func TestVerifyPostBlocksFalseSuccessfulCommit(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)

	options := Options{Argv: []string{"commit", "-m", "noop"}, Cwd: repo}

	err := PreparePost(policy.ExampleBundle(), options)
	if err != nil {
		t.Fatalf("prepare post: %v", err)
	}

	result, err := VerifyPost(policy.ExampleBundle(), options)
	if err != nil {
		t.Fatalf("verify post: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func initGitwrapRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitwrapGit(t, repo, "init")
	runGitwrapGit(t, repo, "config", "user.email", "test@example.com")
	runGitwrapGit(t, repo, "config", "user.name", "Test User")
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "false")

	err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	runGitwrapGit(t, repo, "add", "file.txt")
	runGitwrapGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runGitwrapGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = repo

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
