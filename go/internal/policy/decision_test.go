// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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

	if decision.Severity != policyDef.DefaultSeverity {
		t.Fatalf("severity mismatch: got %q", decision.Severity)
	}

	if decision.Message != policyDef.Message {
		t.Fatalf("message mismatch: got %q", decision.Message)
	}

	if decision.Suggestion != policyDef.Suggestion {
		t.Fatalf("suggestion mismatch: got %q", decision.Suggestion)
	}

	if len(decision.PrincipleIDs) != len(policyDef.PrincipleIDs) {
		t.Fatalf("principle ids mismatch: %#v", decision.PrincipleIDs)
	}
}

func TestDecisionEvidenceFilesPrefersCanonicalFiles(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"files":        []string{" pyproject.toml ", ""},
			"staged_files": []string{"bin/coding-ethos-run"},
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "pyproject.toml" {
		t.Fatalf("files mismatch: %#v", files)
	}
}

func TestDecisionEvidenceFilesFallsBackToStagedFiles(t *testing.T) {
	t.Parallel()

	decision := Decision{
		Evidence: map[string]any{
			"staged_files": []any{"bin/coding-ethos-run", ""},
		},
	}

	files := decision.EvidenceFiles()
	if len(files) != 1 || files[0] != "bin/coding-ethos-run" {
		t.Fatalf("files mismatch: %#v", files)
	}
}
