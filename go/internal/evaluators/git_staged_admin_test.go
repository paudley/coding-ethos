// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"strings"
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

	for _, want := range []string{
		"Human/admin handoff",
		"Agent action: stop trying to commit these files.",
		"git commit -m 'admin change'",
		"--admin-approved is only valid inside the coding-ethos repo admin wrapper.",
	} {
		if !strings.Contains(decisions[0].Suggestion, want) {
			t.Fatalf("suggestion missing %q: %q", want, decisions[0].Suggestion)
		}
	}
}

func TestEvaluateGitStagedAdminFilesOmitsWrapperWarningInsideCodingEthos(t *testing.T) {
	t.Parallel()

	repo := stagedAdminRepo(t)
	writeCodingEthosMarkers(t, repo)

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

	if strings.Contains(decisions[0].Suggestion, "--admin-approved is only valid") {
		t.Fatalf("suggestion should omit wrapper warning: %q", decisions[0].Suggestion)
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

func writeCodingEthosMarkers(t *testing.T, repo string) {
	t.Helper()

	for _, path := range []string{
		"coding_ethos.yml",
		"config.yaml",
		"go/cmd/coding-ethos-run",
	} {
		fullPath := filepath.Join(repo, path)

		err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
		if err != nil {
			t.Fatalf("create marker parent: %v", err)
		}

		err = os.WriteFile(fullPath, []byte("marker\n"), 0o600)
		if err != nil {
			t.Fatalf("write marker %s: %v", path, err)
		}
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
