// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package gitwrap_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const gitEnvLogSuffix = "GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT="

func TestAdminStartBranchRunsApprovedBranchSequence(t *testing.T) {
	repo := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "git.log")
	fakeGit := fakeAdminGit(t, logPath, "")

	t.Setenv("GIT_DIR", "/tmp/poisoned-git-dir")
	t.Setenv("GIT_INDEX_FILE", "/tmp/poisoned-index")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.email")
	t.Setenv("GIT_CONFIG_VALUE_0", "poison@example.com")

	err := AdminStartBranch(fakeGit, repo, []string{"fix/admin-start-branch-take-7"})
	if err != nil {
		t.Fatalf("admin start branch: %v", err)
	}

	log := readText(t, logPath)
	for _, expected := range []string{
		"check-ref-format --branch fix/admin-start-branch-take-7|" + gitEnvLogSuffix,
		"status --porcelain=v1 --untracked-files=all|" + gitEnvLogSuffix,
		"checkout main|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
		"pull --ff-only|GIT_DIR=|GIT_INDEX_FILE=|GIT_CONFIG_COUNT=",
		"checkout -b fix/admin-start-branch-take-7|" + gitEnvLogSuffix,
	} {
		if !strings.Contains(log, expected) {
			t.Fatalf("missing %q in fake git log:\n%s", expected, log)
		}
	}
}

func TestAdminStartBranchRequiresCleanWorktree(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	realGit := resolveRealGit(t)

	runGit(t, realGit, repo, "init")
	err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("dirty\n"), 0o600)
	if err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	err = AdminStartBranch(realGit, repo, []string{"fix/admin-start-branch-take-7"})
	if err == nil {
		t.Fatalf("AdminStartBranch dirty error = nil")
	}
	if !strings.Contains(err.Error(), "worktree must be clean") {
		t.Fatalf("AdminStartBranch dirty error = %v", err)
	}
}

func TestAdminStartBranchRejectsInvalidBranchName(t *testing.T) {
	t.Parallel()

	err := AdminStartBranch("/usr/bin/git", t.TempDir(), []string{"../main"})
	if !strings.Contains(err.Error(), "invalid admin branch name") {
		t.Fatalf("AdminStartBranch invalid branch error = %v", err)
	}
}

func fakeAdminGit(t *testing.T, logPath, statusOutput string) string {
	t.Helper()

	scriptPath := filepath.Join(t.TempDir(), "git")
	script := `#!/usr/bin/env bash
set -euo pipefail
log_path=` + strconv.Quote(logPath) + `
status_output=` + strconv.Quote(statusOutput) + `
printf '%s|GIT_DIR=%s|GIT_INDEX_FILE=%s|GIT_CONFIG_COUNT=%s\n' \
  "$*" "${GIT_DIR:-}" "${GIT_INDEX_FILE:-}" "${GIT_CONFIG_COUNT:-}" >> "$log_path"
if [[ "${1:-}" == "status" ]]; then
  printf '%s' "$status_output"
fi
exit 0
`

	err := os.WriteFile(scriptPath, []byte(script), 0o600)
	if err != nil {
		t.Fatalf("write fake git: %v", err)
	}

	err = os.Chmod(scriptPath, 0o700)
	if err != nil {
		t.Fatalf("chmod fake git: %v", err)
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

func resolveRealGit(t *testing.T) string {
	t.Helper()

	realGit, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve real git: %v", err)
	}

	return realGit
}

func runGit(t *testing.T, realGit, cwd string, args ...string) {
	t.Helper()

	cmd := exec.Command(realGit, args...)
	cmd.Dir = cwd
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}
