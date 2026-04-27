// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestHookFilesForPreCommitDiscoversStagedAndAllFiles(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "tracked.py", "print('tracked')\n")
	mustWriteTestFile(t, "staged.py", "print('staged')\n")
	runGitTestCommand(t, "add", "tracked.py")
	runGitTestCommand(t, "commit", "-m", "test: seed")
	runGitTestCommand(t, "add", "staged.py")

	staged, err := hookFilesForPreCommit(false)
	if err != nil {
		t.Fatalf("hookFilesForPreCommit(false) returned error: %v", err)
	}
	if !reflect.DeepEqual(staged, []string{"staged.py"}) {
		t.Fatalf("staged files = %#v, want staged.py", staged)
	}

	allFiles, err := hookFilesForPreCommit(true)
	if err != nil {
		t.Fatalf("hookFilesForPreCommit(true) returned error: %v", err)
	}
	if !reflect.DeepEqual(allFiles, []string{"staged.py", "tracked.py"}) {
		t.Fatalf("all files = %#v, want staged.py and tracked.py", allFiles)
	}
}

func TestPushedFilesParsesPrePushRefs(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "base.py", "print('base')\n")
	runGitTestCommand(t, "add", "base.py")
	runGitTestCommand(t, "commit", "-m", "test: base")
	remoteSHA := gitTestOutput(t, "rev-parse", "HEAD")

	mustWriteTestFile(t, "feature.py", "print('feature')\n")
	runGitTestCommand(t, "add", "feature.py")
	runGitTestCommand(t, "commit", "-m", "test: feature")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	input := strings.NewReader("refs/heads/main " + localSHA + " refs/heads/main " + remoteSHA + "\n")
	files, err := pushedFiles(input)
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}
	if !reflect.DeepEqual(files, []string{"feature.py"}) {
		t.Fatalf("pushed files = %#v, want feature.py", files)
	}
}

func TestDockerAndWorkflowFileSelection(t *testing.T) {
	files := []string{
		"Dockerfile",
		"docker/Dockerfile.worker",
		".github/workflows/ci.yml",
		".github/workflows/ci.yaml",
		".github/not-workflows/ci.yml",
		"notes.txt",
	}
	if got := dockerFiles(files); !reflect.DeepEqual(got, []string{"Dockerfile", "docker/Dockerfile.worker"}) {
		t.Fatalf("dockerFiles() = %#v", got)
	}
	if got := workflowFiles(files); !reflect.DeepEqual(got, []string{".github/workflows/ci.yml", ".github/workflows/ci.yaml"}) {
		t.Fatalf("workflowFiles() = %#v", got)
	}
}

func setupGitHookTestRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGitTestCommandInDir(t, root, "init", "-q")
	runGitTestCommandInDir(t, root, "config", "user.email", "test@example.com")
	runGitTestCommandInDir(t, root, "config", "user.name", "Test User")

	return root
}

func chdirForTest(t *testing.T, root string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	if err := os.Chdir(root); err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", root, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory failed: %v", err)
		}
	})
}

func runGitTestCommand(t *testing.T, args ...string) {
	t.Helper()
	runGitTestCommandInDir(t, repoRoot(), args...)
}

func runGitTestCommandInDir(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitTestOutput(t *testing.T, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot()
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(output))
}

func TestRepoPathResolvesRelativePath(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	if got := repoPath("ruff.toml"); got != filepath.Join(root, "ruff.toml") {
		t.Fatalf("repoPath() = %q, want repo-root path", got)
	}
}
