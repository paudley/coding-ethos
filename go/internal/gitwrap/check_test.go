// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	checkStatusAllowed = "allowed"
	checkStatusBlocked = "blocked"
)

func TestCheckBlocksHookBypass(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"commit", "--no-verify", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].PolicyID != "git.hook_bypass" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestCheckBlocksCommitAttribution(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"commit", "-m", "feat: test\n\nCo-authored-by: Claude"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].PolicyID != "git.commit_attribution" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestCheckAllowsNormalCommit(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"commit", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusAllowed {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func TestCheckAllowsUnknownOperation(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"status", "--short"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusAllowed {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if result.Operation != "status" {
		t.Fatalf("operation mismatch: got %q", result.Operation)
	}
}

func TestCheckBlocksHistoryRewriteCommands(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"branch force":         {"branch", "-f", "topic", "HEAD~1"},
		"checkout branch move": {"checkout", "-B", "topic", "main"},
		"commit amend":         {"commit", "--amend", "-m", "fix"},
		"force push":           {"push", "--force-with-lease", "origin", "topic"},
		"reset branch":         {"reset", "--soft", "HEAD~1"},
		"reset protected":      {"reset", "main"},
		"reset sha":            {"reset", "1234abc"},
	}

	for name, argv := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := Check(policy.ExampleBundle(), Options{Argv: argv})
			if err != nil {
				t.Fatalf("check git wrapper: %v", err)
			}

			if result.Status != checkStatusBlocked {
				t.Fatalf("status mismatch: got %q", result.Status)
			}

			if len(result.Decisions) == 0 ||
				result.Decisions[0].PolicyID != "git.history_rewrite_prevention" {
				t.Fatalf("policy mismatch: %#v", result.Decisions)
			}
		})
	}
}

func TestCheckAllowsRebaseAndNonMovingReset(t *testing.T) {
	t.Parallel()

	tests := map[string][]string{
		"rebase":                           {"rebase", "main"},
		"reset head":                       {"reset", "HEAD"},
		"reset pathspec":                   {"reset", "HEAD", "--", "file.txt"},
		"reset single pathspec":            {"reset", "file.txt"},
		"reset head pathspec no separator": {"reset", "HEAD", "file.txt"},
	}

	for name, argv := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := Check(policy.ExampleBundle(), Options{Argv: argv})
			if err != nil {
				t.Fatalf("check git wrapper: %v", err)
			}

			if result.Status != checkStatusAllowed {
				t.Fatalf(
					"status mismatch: got %q decisions %#v",
					result.Status,
					result.Decisions,
				)
			}
		})
	}
}

func TestCheckBlocksProtectedSubmoduleInit(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"submodule", "update", "--init", "coding-ethos"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].PolicyID != "git.protected_submodule_update" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestCheckAllowsProtectedSubmoduleRemoteUpgrade(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"submodule", "update", "--remote", "coding-ethos"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusAllowed {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func TestCheckBlocksDisabledGitSigningConfig(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "false")
	runGitwrapGit(t, repo, "config", "user.signingkey", "test-key")

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"status", "--short"},
		Cwd:  repo,
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) == 0 ||
		result.Decisions[0].PolicyID != "git.signed_commits_required" {
		t.Fatalf("policy mismatch: %#v", result.Decisions)
	}
}

func TestCheckBlocksSigningDisableCommands(t *testing.T) {
	t.Parallel()

	repo := initSignedGitwrapRepo(t)

	tests := map[string][]string{
		"commit no gpg sign":  {"commit", "--no-gpg-sign", "-m", "test"},
		"config commit false": {"config", "commit.gpgsign", "false"},
		"global commit false": {"-c", "commit.gpgsign=false", "status"},
	}

	for name, argv := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := Check(policy.ExampleBundle(), Options{
				Argv: argv,
				Cwd:  repo,
			})
			if err != nil {
				t.Fatalf("check git wrapper: %v", err)
			}

			if result.Status != checkStatusBlocked {
				t.Fatalf("status mismatch: got %q", result.Status)
			}
		})
	}
}

func TestCheckBlocksUnsignedOutgoingPush(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runGitwrapGit(t, "", "init", "--bare", remote)
	runGitwrapGit(t, repo, "branch", "-M", "main")
	runGitwrapGit(t, repo, "remote", "add", "origin", remote)
	runGitwrapGit(t, repo, "push", "-u", "origin", "main")

	err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("changed\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}
	runGitwrapGit(t, repo, "add", "file.txt")
	runGitwrapGit(t, repo, "commit", "-m", "unsigned change")
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "true")
	runGitwrapGit(t, repo, "config", "user.signingkey", "test-key")

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"push", "origin", "main"},
		Cwd:  repo,
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
	if len(result.Decisions) == 0 ||
		result.Decisions[0].PolicyID != "git.signed_commits_required" {
		t.Fatalf("policy mismatch: %#v", result.Decisions)
	}
}

func initSignedGitwrapRepo(t *testing.T) string {
	t.Helper()

	repo := initGitwrapRepo(t)
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "true")
	runGitwrapGit(t, repo, "config", "user.signingkey", "test-key")

	return repo
}

func TestCheckHonorsRepoConfigSigningOptOut(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)
	err := os.WriteFile(filepath.Join(repo, "repo_config.yaml"), []byte(`
git:
  signed_operations:
    enabled: false
`), 0o600)
	if err != nil {
		t.Fatalf("write repo config: %v", err)
	}
	runGitwrapGit(t, repo, "config", "commit.gpgsign", "false")

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"status", "--short"},
		Cwd:  repo,
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != checkStatusAllowed {
		t.Fatalf("status mismatch: got %q decisions %#v", result.Status, result.Decisions)
	}
}
