// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lint_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const conditionalImportsSkillID = "conditional-imports"

func TestEnrichResultWithSkillsDerivesSkillFromEthosOverlap(t *testing.T) {
	t.Parallel()

	result := Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Findings: []Finding{{
			CheckID:    "python.conditional_imports",
			SourceTool: "ruff",
			Code:       "PLC" + "0415",
			File:       "pkg/app.py",
			Status:     "fail",
			Severity:   "error",
			Message:    "import should be at module level",
			EthosIDs:   []string{"no-conditional-imports"},
			Blocking:   true,
		}},
	}

	enriched := EnrichResultWithSkills(result, map[string]policy.Skill{
		conditionalImportsSkillID: {
			ID:           conditionalImportsSkillID,
			Description:  "Fix conditional imports structurally.",
			ShortHint:    "Use module-scope imports or Protocol boundaries.",
			PrincipleIDs: []string{"no-conditional-imports", "protocol-first-design"},
		},
		"lint-remediation": {
			ID:           "lint-remediation",
			Description:  "Fix lint structurally.",
			ShortHint:    "Do not weaken lint policy.",
			PrincipleIDs: []string{"linting-as-code-quality-enforcement"},
		},
	})

	if enriched.Findings[0].SkillID != conditionalImportsSkillID {
		t.Fatalf("finding skill = %q", enriched.Findings[0].SkillID)
	}

	if len(enriched.SkillHints) != 1 ||
		enriched.SkillHints[0].SkillID != conditionalImportsSkillID ||
		enriched.SkillHints[0].PrincipleID != "no-conditional-imports" {
		t.Fatalf("skill hints = %#v", enriched.SkillHints)
	}
}

func TestSkillHintsForDiagnosticsDerivesSkillFromTriggerTerms(t *testing.T) {
	t.Parallel()

	hints := SkillHintsForDiagnostics(
		[]diagnostics.Diagnostic{{
			Tool:     "pylint",
			File:     "pkg/app.py",
			Line:     4,
			Severity: "error",
			Code:     "cyclic-import",
			Message:  "Cyclic import detected",
		}},
		map[string]policy.Skill{
			conditionalImportsSkillID: {
				ID:           conditionalImportsSkillID,
				Description:  "Fix import structure.",
				ShortHint:    "Break cycles with Protocol-oriented boundaries.",
				PrincipleIDs: []string{"protocol-first-design"},
				TriggerTerms: []string{"cyclic-import", "import cycle"},
			},
		},
	)

	if len(hints) != 1 ||
		hints[0].SkillID != conditionalImportsSkillID ||
		hints[0].PrincipleID != "protocol-first-design" {
		t.Fatalf("skill hints = %#v", hints)
	}
}

func TestEnrichResultWithSkillsDerivedDecisionSkillOverridesEvidenceKey(t *testing.T) {
	t.Parallel()

	result := Result{
		Scope:  "staged",
		Status: "blocked",
		Decisions: []policy.Decision{{
			Evidence: map[string]any{
				"skill_id": 42,
			},
			Decision:     "block",
			Message:      "Large source files must not keep growing.",
			PolicyID:     "filesystem.line_limits",
			Severity:     "block",
			PrincipleIDs: []string{"solid-is-law"},
		}},
	}

	enriched := EnrichResultWithSkills(result, map[string]policy.Skill{
		"agent-operating-discipline": {
			ID:           "agent-operating-discipline",
			Description:  "Keep edits scoped and verifiable.",
			ShortHint:    "State assumptions and verify the smallest sufficient change.",
			PrincipleIDs: []string{"solid-is-law"},
		},
	})

	got, ok := enriched.Decisions[0].Evidence["skill_id"].(string)
	if !ok || got != "agent-operating-discipline" {
		t.Fatalf("decision skill evidence = %#v", enriched.Decisions[0].Evidence)
	}
}
