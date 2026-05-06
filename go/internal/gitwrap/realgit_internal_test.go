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
	selfDir := filepath.Join(root, "shim")
	gitDir := filepath.Join(root, "bin")

	err := os.MkdirAll(selfDir, 0o755)
	if err != nil {
		t.Fatalf("create self dir: %v", err)
	}

	err = os.MkdirAll(gitDir, 0o755)
	if err != nil {
		t.Fatalf("create git dir: %v", err)
	}

	self := filepath.Join(selfDir, "git")
	git := filepath.Join(gitDir, "git")

	err = os.WriteFile(self, []byte("#!/usr/bin/env sh\n"), 0o755)
	if err != nil {
		t.Fatalf("write self: %v", err)
	}

	err = os.WriteFile(git, []byte("#!/usr/bin/env sh\n"), 0o755)
	if err != nil {
		t.Fatalf("write git: %v", err)
	}

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
