// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
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
