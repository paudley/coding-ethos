// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package realgit_test

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

var errRealGitTestUnresolved = apperror.StaticError(
	"real git executable could not be resolved",
)

func TestResolutionHelpers(t *testing.T) {
	root := t.TempDir()
	selfDir, gitDir := createGitResolutionDirs(t, root)
	self := createExecutableGit(t, selfDir)
	git := createExecutableGit(t, gitDir)

	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+selfDir)

	candidates := realgit.Candidates(self)
	if len(candidates) == 0 || candidates[0] != git {
		t.Fatalf("real git candidates = %#v", candidates)
	}

	files := realgit.ExecutableFiles([]string{git, git, filepath.Join(root, "missing")})
	if len(files) != 1 || files[0] != git {
		t.Fatalf("executable files = %#v", files)
	}

	if !realgit.SamePath(filepath.Join(root, "bin", "..", "bin"), gitDir) {
		t.Fatal("SamePath should compare absolute paths")
	}

	if realgit.SameExecutable(self, git) {
		t.Fatal("different executables should not compare equal")
	}
}

func TestResolveRejectsShimEnvironmentCandidate(t *testing.T) {
	root := t.TempDir()
	selfDir, gitDir := createGitResolutionDirs(t, root)
	self := createExecutableGit(t, selfDir)
	git := createExecutableGit(t, gitDir)
	shim := createNamedExecutable(
		t,
		selfDir,
		"git-shim",
		"#!/usr/bin/env sh\nexec /repo/bin/coding-ethos-run policy-git \"$@\"\n",
	)

	t.Setenv(realgit.Env, shim)
	t.Setenv("PATH", gitDir+string(os.PathListSeparator)+selfDir)

	got, err := resolveWithSelfForTest(self)
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	if got != git {
		t.Fatalf("Resolve chose %q, want non-shim git %q", got, git)
	}
}

func resolveWithSelfForTest(self string) (string, error) {
	if envValue := os.Getenv(realgit.Env); envValue != "" &&
		realgit.UsableCandidate(self, envValue) {
		return envValue, nil
	}

	for _, candidate := range realgit.Candidates(self) {
		if realgit.UsableCandidate(self, candidate) {
			return candidate, nil
		}
	}

	return "", errRealGitTestUnresolved
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

	return createNamedExecutable(
		t,
		dir,
		"git",
		"#!/usr/bin/env sh\nprintf 'git version 2.0.0\\n'\n",
	)
}

func createNamedExecutable(t *testing.T, dir, name, payload string) string {
	t.Helper()

	err := os.MkdirAll(dir, 0o755)
	if err != nil {
		t.Fatalf("create executable dir: %v", err)
	}

	path := filepath.Join(dir, name)

	err = os.WriteFile(path, []byte(payload), 0o600)
	if err != nil {
		t.Fatalf("write git executable: %v", err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod git executable: %v", err)
	}

	return path
}
