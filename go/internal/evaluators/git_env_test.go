// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"slices"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
)

func TestGitCommandUsesConfiguredRealGit(t *testing.T) {
	t.Setenv(RealGitEnv, "/opt/system-git")

	cmd := GitCommand("", "status")
	if cmd.Path != "/opt/system-git" {
		t.Fatalf("gitCommand path = %q, want configured real git", cmd.Path)
	}
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

	got := CleanGitLocalEnv(source)

	if len(got) != 2 || got[0] != "PATH=/usr/bin" {
		t.Fatalf("CleanGitLocalEnv() = %#v, want PATH and git lock guard", got)
	}

	if !slices.Contains(got, "GIT_OPTIONAL_LOCKS=0") {
		t.Fatalf("CleanGitLocalEnv() missing optional lock guard: %#v", got)
	}
}
