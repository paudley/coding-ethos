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
