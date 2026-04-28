// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdminStartBranchRunsApprovedBranchSequence(t *testing.T) {
	repo := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	fakeGit := fakeAdminGit(t, logPath, "")

	t.Setenv("GIT_DIR", "/tmp/poisoned-git-dir")
	t.Setenv("GIT_INDEX_FILE", "/tmp/poisoned-index")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.email")
	t.Setenv("GIT_CONFIG_VALUE_0", "poison@example.com")

	err := AdminStartBranch(fakeGit, repo, []string{"hooks_and_crooks_take_7"})
	if err != nil {
		t.Fatalf("admin start branch: %v", err)
	}

	log := readText(t, logPath)
	for _, expected := range []string{
		"check-ref-format --branch hooks_and_crooks_take_7|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
		"status --porcelain=v1 --untracked-files=all|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
		"checkout main|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
		"pull --ff-only|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
		"checkout -b hooks_and_crooks_take_7|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
	} {
		if !strings.Contains(log, expected) {
			t.Fatalf("missing %q in fake git log:\n%s", expected, log)
		}
	}
}

func TestAdminStartBranchRequiresCleanWorktree(t *testing.T) {
	repo := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	fakeGit := fakeAdminGit(t, logPath, " M file.txt\n")

	err := AdminStartBranch(fakeGit, repo, []string{"hooks_and_crooks_take_7"})
	if !errors.Is(err, errAdminBranchDirty) {
		t.Fatalf("AdminStartBranch dirty error = %v, want %v", err, errAdminBranchDirty)
	}

	log := readText(t, logPath)
	if strings.Contains(log, "checkout main") {
		t.Fatalf("dirty worktree still ran checkout:\n%s", log)
	}
}

func TestAdminStartBranchRejectsInvalidBranchName(t *testing.T) {
	err := AdminStartBranch("/usr/bin/git", t.TempDir(), []string{"../main"})
	if !errors.Is(err, errAdminBranchInvalid) {
		t.Fatalf("AdminStartBranch invalid branch error = %v", err)
	}
}

func fakeAdminGit(t *testing.T, logPath string, statusOutput string) string {
	t.Helper()

	t.Setenv("FAKE_ADMIN_GIT_LOG", logPath)
	t.Setenv("FAKE_ADMIN_GIT_STATUS", statusOutput)

	scriptPath := filepath.Join(t.TempDir(), "git")
	script := `#!/usr/bin/env bash
set -euo pipefail
printf '%s|GIT_DIR=%s|GIT_INDEX_FILE=%s|GIT_CONFIG_COUNT=%s\n' "$*" "${GIT_DIR:-}" "${GIT_INDEX_FILE:-}" "${GIT_CONFIG_COUNT:-}" >> "$FAKE_ADMIN_GIT_LOG"
if [[ "${1:-}" == "status" ]]; then
  printf '%s' "$FAKE_ADMIN_GIT_STATUS"
fi
`
	err := os.WriteFile(scriptPath, []byte(script), 0o700)
	if err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	return scriptPath
}

func readText(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return string(data)
}
