// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func TestGitCommandUsesConfiguredRealGit(t *testing.T) {
	realGit := writeExecutableGit(
		t,
		t.TempDir(),
		"git",
		"#!/usr/bin/env sh\nprintf 'git version 2.0.0\\n'\n",
	)
	t.Setenv(RealGitEnv, realGit)

	cmd := GitCommand("", "status")
	if cmd.Path != realGit {
		t.Fatalf("gitCommand path = %q, want configured real git", cmd.Path)
	}
}

func TestGitCommandRejectsConfiguredCodingEthosShim(t *testing.T) {
	shim := writeExecutableGit(
		t,
		t.TempDir(),
		"git",
		"#!/usr/bin/env sh\nexec /repo/bin/coding-ethos-run policy-git \"$@\"\n",
	)
	t.Setenv(RealGitEnv, shim)

	cmd := GitCommand("", "status")
	if cmd.Path == shim {
		t.Fatal("gitCommand must not route internal git operations through coding-ethos-run")
	}
}

func writeExecutableGit(t *testing.T, dir, name, payload string) string {
	t.Helper()

	path := filepath.Join(dir, name)

	err := os.WriteFile(path, []byte(payload), 0o600)
	if err != nil {
		t.Fatalf("write executable git fixture: %v", err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("chmod executable git fixture: %v", err)
	}

	return path
}

func TestCleanGitLocalEnvRemovesHookScopedGitVariables(t *testing.T) {
	t.Parallel()

	source := []string{
		"GIT_DIR=/tmp/wrong-git-dir",
		"GIT_INDEX_FILE=/tmp/wrong-index",
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=user.email",
		"GIT_CONFIG_VALUE_0=test@example.com",
		"PATH=/usr/bin",
	}

	got := realgit.CleanGitLocalEnv(source)

	if len(got) != 2 || got[0] != "PATH=/usr/bin" {
		t.Fatalf("CleanGitLocalEnv() = %#v, want PATH and git lock guard", got)
	}

	if !slices.Contains(got, "GIT_OPTIONAL_LOCKS=0") {
		t.Fatalf("CleanGitLocalEnv() missing optional lock guard: %#v", got)
	}
}
