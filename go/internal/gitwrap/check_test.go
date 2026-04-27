// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap_test

import (
	. "blackcat.ca/coding-ethos/go/internal/gitwrap"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestCheckBlocksHookBypass(t *testing.T) {
	t.Parallel()

	result, err := Check(policy.ExampleBundle(), Options{
		Argv: []string{"commit", "--no-verify", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("check git wrapper: %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].PolicyID != "git.hook_bypass" {
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

	if result.Status != "allowed" {
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

	if result.Status != "allowed" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if result.Operation != "status" {
		t.Fatalf("operation mismatch: got %q", result.Operation)
	}
}
