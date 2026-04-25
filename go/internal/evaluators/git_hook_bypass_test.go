// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateGitHookBypassBlocksNoVerify(t *testing.T) {
	policyDef := policy.ExampleBundle().Policies["git.hook_bypass"]
	decisions, err := EvaluateGitHookBypass(policyDef, Context{
		Argv: []string{"git", "commit", "--no-verify", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("evaluate hook bypass: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: got %d", len(decisions))
	}
	if decisions[0].Decision != "block" {
		t.Fatalf("decision mismatch: got %q", decisions[0].Decision)
	}
}

func TestEvaluateGitHookBypassIgnoresNormalCommit(t *testing.T) {
	policyDef := policy.ExampleBundle().Policies["git.hook_bypass"]
	decisions, err := EvaluateGitHookBypass(policyDef, Context{
		Argv: []string{"git", "commit", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("evaluate hook bypass: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluateGitHookBypassIgnoresNonGitCommand(t *testing.T) {
	policyDef := policy.ExampleBundle().Policies["git.hook_bypass"]
	decisions, err := EvaluateGitHookBypass(policyDef, Context{
		Argv: []string{"pytest", "--no-verify"},
	})
	if err != nil {
		t.Fatalf("evaluate hook bypass: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}
