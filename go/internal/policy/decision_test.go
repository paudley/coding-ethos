// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import "testing"

func TestNewDecisionCopiesPolicyContext(t *testing.T) {
	policyDef := ExampleBundle().Policies["git.hook_bypass"]
	decision := NewDecision("block", policyDef)

	if decision.Decision != "block" {
		t.Fatalf("decision mismatch: got %q", decision.Decision)
	}
	if decision.PolicyID != "git.hook_bypass" {
		t.Fatalf("policy id mismatch: got %q", decision.PolicyID)
	}
	if decision.Severity != "block" {
		t.Fatalf("severity mismatch: got %q", decision.Severity)
	}
	if len(decision.PrincipleIDs) != 1 || decision.PrincipleIDs[0] != "one-path-for-critical-operations" {
		t.Fatalf("principle ids mismatch: got %#v", decision.PrincipleIDs)
	}
}
