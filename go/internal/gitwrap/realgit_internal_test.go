// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRealGitResolutionHelpers(t *testing.T) {
	root := t.TempDir()
	selfDir, gitDir := createGitResolutionDirs(t, root)
	self := createExecutableGit(t, selfDir)
	git := createExecutableGit(t, gitDir)

	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+selfDir)

	candidates := realGitCandidates(self)
	if len(candidates) == 0 || candidates[0] != git {
		t.Fatalf("real git candidates = %#v", candidates)
	}

	files := executableFiles([]string{git, git, filepath.Join(root, "missing")})
	if len(files) != 1 || files[0] != git {
		t.Fatalf("executable files = %#v", files)
	}

	if !samePath(filepath.Join(root, "bin", "..", "bin"), gitDir) {
		t.Fatal("samePath should compare absolute paths")
	}

	if sameExecutable(self, git) {
		t.Fatal("different executables should not compare equal")
	}
}

func createGitResolutionDirs(t *testing.T, root string) (string, string) {
	t.Helper()

	selfDir := filepath.Join(root, "shim")
	gitDir := filepath.Join(root, "bin")

	for _, dir := range []string{selfDir, gitDir} {
		err := os.MkdirAll(dir, 0o755)
		if err != nil {
			t.Fatalf("create git resolution dir: %v", err)
		}
	}

	return selfDir, gitDir
}

func createExecutableGit(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, "git")

	err := os.WriteFile(path, []byte("#!/usr/bin/env sh\n"), 0o600)
	if err != nil {
		t.Fatalf("write git executable: %v", err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod git executable: %v", err)
	}

	return path
}

func TestResultJSONAndExitCodeError(t *testing.T) {
	t.Parallel()

	result := Result{
		Status:    "blocked",
		Operation: "commit",
		Argv:      []string{"git", "commit"},
	}
	if !result.Blocked() {
		t.Fatal("blocked result should report blocked")
	}

	var buffer bytes.Buffer

	err := EncodeResult(&buffer, result)
	if err != nil {
		t.Fatalf("encode result: %v", err)
	}

	if !strings.Contains(buffer.String(), `"status": "blocked"`) {
		t.Fatalf("encoded result = %s", buffer.String())
	}

	if got := (ExitCodeError{Code: 128}).Error(); got != "git exited with status 128" {
		t.Fatalf("exit code error = %q", got)
	}
}
