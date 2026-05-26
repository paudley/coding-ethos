// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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

func TestCheckAllowsSigningConfigRemediationCommands(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)

	tests := map[string][]string{
		"commit signing true": {"config", "commit.gpgsign", "true"},
		"signing key":         {"config", "user.signingkey", "test-key"},
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

func TestCheckDefersPushOutsideWorkTreeToGit(t *testing.T) {
	runOutsideGitwrapWorkTree(t, func(cwd string) {
		result, err := Check(policy.ExampleBundle(), Options{
			Argv: []string{"push", "origin", "main"},
			Cwd:  cwd,
		})
		if err != nil {
			t.Fatalf("check git wrapper: %v", err)
		}

		if result.Status != checkStatusAllowed {
			t.Fatalf("status mismatch: got %q decisions %#v", result.Status, result.Decisions)
		}
	})
}

func runOutsideGitwrapWorkTree(t *testing.T, run func(string)) {
	t.Helper()

	root := gitwrapCheckoutRoot(t)
	previous, existed := os.LookupEnv("GIT_CEILING_DIRECTORIES")
	if err := os.Setenv("GIT_CEILING_DIRECTORIES", root); err != nil {
		t.Fatalf("set GIT_CEILING_DIRECTORIES: %v", err)
	}
	defer func() {
		if existed {
			if err := os.Setenv("GIT_CEILING_DIRECTORIES", previous); err != nil {
				t.Fatalf("restore GIT_CEILING_DIRECTORIES: %v", err)
			}

			return
		}

		if err := os.Unsetenv("GIT_CEILING_DIRECTORIES"); err != nil {
			t.Fatalf("unset GIT_CEILING_DIRECTORIES: %v", err)
		}
	}()

	run(t.TempDir())
}

func gitwrapCheckoutRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get cwd: %v", err)
	}

	for {
		if gitwrapFileExists(filepath.Join(dir, "coding_ethos.yml")) &&
			gitwrapFileExists(filepath.Join(dir, "config.yaml")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("coding-ethos checkout root not found from %s", dir)
		}

		dir = parent
	}
}

func gitwrapFileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func TestCheckBlocksUnsignedOutgoingPush(t *testing.T) {
	t.Parallel()

	repo := initGitwrapRepo(t)
	runGitwrapGit(t, repo, "branch", "-M", "main")
	runGitwrapGit(t, repo, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGitwrapGit(t, repo, "config", "branch.main.remote", "origin")
	runGitwrapGit(t, repo, "config", "branch.main.merge", "refs/heads/main")

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
