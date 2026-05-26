// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestFindingsFromDecisionsUseDecisionEvidenceForEmbeddedDiagnostics(
	t *testing.T,
) {
	t.Parallel()

	findings := findingsFromDecisions([]policy.Decision{{
		Decision:   "block",
		PolicyID:   "git.staged_admin_files",
		Severity:   "block",
		Message:    "Administrative staged files require explicit handling.",
		Suggestion: "Confirm the policy change is intentional.",
		Evidence: map[string]any{
			"files":    []any{"coding_ethos.yml"},
			"skill_id": "safe-git-workflow",
		},
		Diagnostics: []diagnostics.Diagnostic{{
			Message: "staged admin file detected",
		}},
	}}, nil)

	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}

	finding := findings[0]
	if finding.File != "coding_ethos.yml" ||
		finding.PolicyID != "git.staged_admin_files" ||
		finding.SkillID != "safe-git-workflow" ||
		finding.Message != "staged admin file detected" {
		t.Fatalf("finding = %#v", finding)
	}
}
