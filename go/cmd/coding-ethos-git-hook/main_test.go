// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
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
