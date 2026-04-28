// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:paralleltest,lll // Uses process-global fixtures.
package main

import (
	"context"
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

func TestPushedFilesHandlesNewBranchAndDeleteRefs(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)
	mustWriteTestFile(t, "first.py", "print('first')\n")
	runGitTestCommand(t, "add", "first.py")
	runGitTestCommand(t, "commit", "-m", "test: first")
	mustWriteTestFile(t, "second.py", "print('second')\n")
	runGitTestCommand(t, "add", "second.py")
	runGitTestCommand(t, "commit", "-m", "test: second")
	localSHA := gitTestOutput(t, "rev-parse", "HEAD")

	input := strings.NewReader(
		"refs/heads/new " + localSHA + " refs/heads/new " + allZeroSHA + "\n" +
			"refs/heads/gone " + allZeroSHA + " refs/heads/gone " + localSHA + "\n",
	)

	files, err := pushedFiles(input)
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}

	want := []string{"first.py", "second.py"}
	if !reflect.DeepEqual(files, want) {
		t.Fatalf("pushed files = %#v, want %#v", files, want)
	}
}

func TestPushedFilesDeduplicatesMultipleRefs(t *testing.T) {
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

	line := "refs/heads/main " + localSHA + " refs/heads/main " + remoteSHA + "\n"

	files, err := pushedFiles(strings.NewReader(line + line))
	if err != nil {
		t.Fatalf("pushedFiles() returned error: %v", err)
	}

	if !reflect.DeepEqual(files, []string{"feature.py"}) {
		t.Fatalf("pushed files = %#v, want deduplicated feature.py", files)
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

func TestRunHookGroupsInSubprocessesReplaysOnlyFailedOutputInGroupOrder(t *testing.T) {
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "hook-runner")
	mustWriteTestFile(
		t,
		scriptPath,
		`#!/bin/sh
case "$2" in
  pass)
    echo pass-output
    exit 0
    ;;
  fail-a)
    sleep 0.1
    echo first-failure
    exit 1
    ;;
  fail-b)
    echo second-failure
    exit 1
    ;;
esac
echo "unexpected group: $2"
exit 2
`,
	)

	err := os.Chmod(scriptPath, 0o700)
	if err != nil {
		t.Fatalf("os.Chmod(%q) failed: %v", scriptPath, err)
	}

	stdout := captureStdout(t, func() {
		groups := []hookGroup{
			{Name: "pass"},
			{Name: "fail-a"},
			{Name: "fail-b"},
		}
		executablePath := func() (string, error) {
			return scriptPath, nil
		}

		if got := runHookGroupsInSubprocessesWithExecutable(
			groups,
			nil,
			executablePath,
		); got != 1 {
			t.Fatalf("runHookGroupsInSubprocesses() = %d, want 1", got)
		}
	})

	if strings.Contains(stdout, "pass-output") {
		t.Fatalf("successful group output was replayed: %q", stdout)
	}

	firstIndex := strings.Index(stdout, "first-failure")

	secondIndex := strings.Index(stdout, "second-failure")
	if firstIndex < 0 || secondIndex < 0 || firstIndex > secondIndex {
		t.Fatalf("failed group output was not replayed in group order: %q", stdout)
	}
}

func TestHookGroupResultFileRoundTrip(t *testing.T) {
	t.Parallel()

	resultPath := filepath.Join(t.TempDir(), "result.json")
	want := hookGroupResult{
		Name:       "syntax",
		Status:     statusFail,
		ExitCode:   1,
		DurationMS: 12,
		Commands: []hookCommandResult{
			{Name: "yamllint", Status: statusPass, ExitCode: 0, DurationMS: 4},
			{Name: "yamllint", Status: statusFail, ExitCode: 1, DurationMS: 8},
		},
	}

	writeHookGroupResultFile(resultPath, want)

	got, ok := readHookGroupResultFile(resultPath)
	if !ok {
		t.Fatal("readHookGroupResultFile() did not read result")
	}

	if got.Name != want.Name ||
		got.Status != want.Status ||
		got.ExitCode != want.ExitCode ||
		got.DurationMS != want.DurationMS ||
		!reflect.DeepEqual(got.Commands, want.Commands) {
		t.Fatalf("readHookGroupResultFile() = %#v, want %#v", got, want)
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

	t.Chdir(root)
}

func runGitTestCommand(t *testing.T, args ...string) {
	t.Helper()
	runGitTestCommandInDir(t, repoRoot(), args...)
}

func runGitTestCommandInDir(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, string(output))
	}
}

func gitTestOutput(t *testing.T, args ...string) string {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = repoRoot()
	cmd.Env = cleanGitTestEnv()

	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s failed: %v", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(output))
}

func cleanGitTestEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		name, _, found := strings.Cut(item, "=")
		if found && gitLocalEnvName(name) {
			continue
		}

		env = append(env, item)
	}

	return env
}

func gitLocalEnvName(name string) bool {
	switch name {
	case "GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_COMMON_DIR",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_PARAMETERS",
		"GIT_DIR",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_OBJECT_DIRECTORY",
		"GIT_PREFIX",
		"GIT_QUARANTINE_PATH",
		"GIT_WORK_TREE":
		return true
	default:
		return strings.HasPrefix(name, "GIT_CONFIG_KEY_") ||
			strings.HasPrefix(name, "GIT_CONFIG_VALUE_")
	}
}

func TestRepoPathResolvesRelativePath(t *testing.T) {
	root := setupGitHookTestRepo(t)
	chdirForTest(t, root)

	if got := repoPath("ruff.toml"); got != filepath.Join(root, "ruff.toml") {
		t.Fatalf("repoPath() = %q, want repo-root path", got)
	}
}
