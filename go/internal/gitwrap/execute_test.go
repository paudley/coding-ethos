// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestVerifyPostBlocksFalseSuccessfulCommit(t *testing.T) {
	repo := initGitwrapRepo(t)
	options := Options{Argv: []string{"commit", "-m", "noop"}, Cwd: repo}
	if err := PreparePost(policy.ExampleBundle(), options); err != nil {
		t.Fatalf("prepare post: %v", err)
	}
	result, err := VerifyPost(policy.ExampleBundle(), options)
	if err != nil {
		t.Fatalf("verify post: %v", err)
	}
	if result.Status != "blocked" {
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
	if err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitwrapGit(t, repo, "add", "file.txt")
	runGitwrapGit(t, repo, "commit", "-m", "initial")
	return repo
}

func runGitwrapGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
