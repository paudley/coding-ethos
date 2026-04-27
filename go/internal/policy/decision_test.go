// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/policy"
)

const blockDecision = "block"

func TestNewDecisionCopiesPolicyContext(t *testing.T) {
	t.Parallel()

	policyDef := ExampleBundle().Policies["git.hook_bypass"]
	decision := NewDecision(blockDecision, policyDef)

	if decision.Decision != blockDecision {
		t.Fatalf("decision mismatch: got %q", decision.Decision)
	}

	if decision.PolicyID != "git.hook_bypass" {
		t.Fatalf("policy id mismatch: got %q", decision.PolicyID)
	}

	if decision.Severity != blockDecision {
		t.Fatalf("severity mismatch: got %q", decision.Severity)
	}

	if len(decision.PrincipleIDs) != 1 ||
		decision.PrincipleIDs[0] != "one-path-for-critical-operations" {
		t.Fatalf("principle ids mismatch: got %#v", decision.PrincipleIDs)
	}
}
