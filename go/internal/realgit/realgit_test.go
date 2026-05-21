// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package realgit_test

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
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

func TestResolveIgnoresEnvironmentCandidate(t *testing.T) {
	root := t.TempDir()
	selfDir := filepath.Join(root, "runtime")
	gitDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(selfDir, 0o755); err != nil {
		t.Fatalf("create self dir: %v", err)
	}
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("create git dir: %v", err)
	}
	git := createExecutableGit(t, gitDir)
	envGit := createNamedExecutable(
		t,
		selfDir,
		"env-git",
		"#!/usr/bin/env sh\nprintf 'git version env\\n'\n",
	)

	t.Setenv(realgit.Env, envGit)
	t.Setenv("PATH", gitDir)

	got, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	if got != git {
		t.Fatalf("Resolve chose %q, want PATH git %q", got, git)
	}
}

func TestResolveRejectsLookalikeAgentShellRealGit(t *testing.T) {
	root := t.TempDir()
	gitDir := filepath.Join(root, "bin")
	lookalikeDir := filepath.Join(
		root,
		".coding-ethos",
		"cache",
		"agent-shell",
		"run-test",
	)
	git := createExecutableGit(t, gitDir)
	lookalike := createNamedExecutable(
		t,
		lookalikeDir,
		"real-git",
		"#!/usr/bin/env sh\nprintf 'not git\\n'\n",
	)

	t.Setenv(realgit.Env, lookalike)
	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("PATH", gitDir)

	got, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	if got != git {
		t.Fatalf("Resolve chose %q, want PATH git %q", got, git)
	}
}

func TestCleanGitLocalEnvRemovesHookScopedConfiguration(t *testing.T) {
	t.Parallel()

	got := realgit.CleanGitLocalEnv([]string{
		"PATH=/usr/bin",
		"GIT_DIR=/tmp/repo/.git",
		"GIT_CONFIG_KEY_0=core.sshCommand",
		"GIT_CONFIG_VALUE_0=ssh -i key",
		"GIT_WORK_TREE=/tmp/repo",
		"KEEP=value",
	})

	for _, blocked := range []string{
		"GIT_DIR=/tmp/repo/.git",
		"GIT_CONFIG_KEY_0=core.sshCommand",
		"GIT_CONFIG_VALUE_0=ssh -i key",
		"GIT_WORK_TREE=/tmp/repo",
	} {
		if slices.Contains(got, blocked) {
			t.Fatalf("CleanGitLocalEnv retained %q: %#v", blocked, got)
		}
	}

	for _, kept := range []string{"PATH=/usr/bin", "KEEP=value", "GIT_OPTIONAL_LOCKS=0"} {
		if !slices.Contains(got, kept) {
			t.Fatalf("CleanGitLocalEnv missing %q: %#v", kept, got)
		}
	}
}

func TestLooksLikeCodingEthosShimRejectsRuntimeDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	selfDir := filepath.Join(root, "runtime")
	toolDir := filepath.Join(root, "tools")
	self := createNamedExecutable(t, selfDir, "coding-ethos-run", "#!/bin/sh\nexit 0\n")
	shim := createNamedExecutable(t, toolDir, "git", "#!/bin/sh\nexit 0\n")
	createNamedExecutable(t, toolDir, "coding-ethos-run", "#!/bin/sh\nexit 0\n")

	if !realgit.LooksLikeCodingEthosShim(filepath.Join(selfDir, "git"), self) {
		t.Fatal("git beside current runtime should be treated as a shim")
	}
	if !realgit.LooksLikeCodingEthosShim(shim, self) {
		t.Fatal("git beside coding-ethos-run should be treated as a shim")
	}
	systemGit := createNamedExecutable(
		t,
		filepath.Join(root, "system"),
		"git",
		"#!/bin/sh\nexit 0\n",
	)
	if realgit.LooksLikeCodingEthosShim(systemGit, self) {
		t.Fatal(
			"git outside the runtime and tool directories should not be treated as a coding-ethos shim",
		)
	}
}

func TestExecutableUsesShimNameWhenRequested(t *testing.T) {
	t.Parallel()

	if got := realgit.Executable(context.Background(), true); got != "git" {
		t.Fatalf("Executable(wantsShim=true) = %q, want git", got)
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
