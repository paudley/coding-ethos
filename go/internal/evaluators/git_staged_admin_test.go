// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const recordDecision = "record"

func TestBlockedAdminFilesFindsBasenamesAndDirs(t *testing.T) {
	t.Parallel()

	blocked := BlockedAdminFiles(
		[]string{
			"pyproject.toml",
			"src/app.py",
			"pre-commit/hooks/run.sh",
			"docs/notes.md",
		},
		nil,
	)
	if len(blocked) != 2 {
		t.Fatalf("blocked count mismatch: %#v", blocked)
	}

	if blocked[0] != "pyproject.toml" || blocked[1] != "pre-commit/hooks/run.sh" {
		t.Fatalf("blocked files mismatch: %#v", blocked)
	}
}

func TestBlockedAdminFilesUsesConfiguredPatterns(t *testing.T) {
	t.Parallel()

	blocked := BlockedAdminFiles(
		[]string{"custom.lock", "ops/config.yml", "pyproject.toml"},
		map[string]any{
			"basenames": []any{"custom.lock"},
			"dirs":      []any{"ops"},
		},
	)
	if len(blocked) != 2 {
		t.Fatalf("blocked count mismatch: %#v", blocked)
	}

	if blocked[0] != "custom.lock" || blocked[1] != "ops/config.yml" {
		t.Fatalf("blocked files mismatch: %#v", blocked)
	}
}

func TestEvaluateGitStagedAdminFilesBlocksWithoutAdminApproval(t *testing.T) {
	t.Parallel()

	repo := stagedAdminRepo(t)

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "admin change"},
			Cwd:  repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}
}

func TestEvaluateGitStagedAdminFilesRecordsWithAdminApproval(t *testing.T) {
	t.Parallel()

	repo := stagedAdminRepo(t)

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			AdminApproved: true,
			Argv:          []string{"git", "commit", "-m", "admin change"},
			Cwd:           repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].Decision != recordDecision ||
		decisions[0].Severity != recordDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}
}

func stagedAdminRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")

	hookPath := filepath.Join(repo, "bin")

	err := os.MkdirAll(hookPath, 0o755)
	if err != nil {
		t.Fatalf("create hook dir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(hookPath, "coding-ethos-run"),
		[]byte("#!/usr/bin/env bash\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write hook file: %v", err)
	}

	runGit(t, repo, "add", "bin/coding-ethos-run")

	return repo
}

func stagedAdminPolicy() policy.Policy {
	return policy.Policy{
		ID:              "git.staged_admin_files",
		DefaultSeverity: blockDecision,
		Message:         "Administrative staged files require explicit handling.",
	}
}
