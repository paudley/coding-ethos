// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStagedFilesListsGitIndexEntries(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.invalid")
	runGit(t, repo, "config", "user.name", "Test User")

	path := filepath.Join(repo, "pkg", "app.py")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}
	if err := os.WriteFile(path, []byte("print('x')\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	runGit(t, repo, "add", "pkg/app.py")

	files, err := stagedFiles(repo)
	if err != nil {
		t.Fatalf("staged files: %v", err)
	}
	if len(files) != 1 || files[0] != "pkg/app.py" {
		t.Fatalf("staged files = %#v, want pkg/app.py", files)
	}
}

func TestFilesFromInputsCombinesFlagAndFileLists(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(
		path,
		[]byte("pkg/app.py\n\npkg/other.py\r\n"),
		0o600,
	); err != nil {
		t.Fatalf("write file list: %v", err)
	}

	files, err := filesFromInputs("README.md, docs/usage.md", path)
	if err != nil {
		t.Fatalf("files from inputs: %v", err)
	}

	want := []string{
		"README.md",
		"docs/usage.md",
		"pkg/app.py",
		"pkg/other.py",
	}
	if len(files) != len(want) {
		t.Fatalf("files = %#v, want %#v", files, want)
	}
	for index := range want {
		if files[index] != want[index] {
			t.Fatalf("files = %#v, want %#v", files, want)
		}
	}
}

func TestShouldReturnEmptyExplicitFileScope(t *testing.T) {
	t.Parallel()

	if !shouldReturnEmptyExplicitFileScope("files", nil, "", "files.txt") {
		t.Fatal("empty --files-from selection should return an empty files result")
	}
	if !shouldReturnEmptyExplicitFileScope("files", nil, "   ", "files.txt") {
		t.Fatal("--files-from makes an empty selection explicit")
	}
	if shouldReturnEmptyExplicitFileScope("files", []string{"pkg/app.py"}, "", "files.txt") {
		t.Fatal("non-empty files must run policy evaluation")
	}
	if shouldReturnEmptyExplicitFileScope("files", nil, "", "") {
		t.Fatal("implicit files scope must preserve policy explanation behavior")
	}
	if shouldReturnEmptyExplicitFileScope("staged", nil, "", "files.txt") {
		t.Fatal("staged scope resolves files from git index")
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	command := exec.Command("git", args...)
	command.Dir = dir
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
