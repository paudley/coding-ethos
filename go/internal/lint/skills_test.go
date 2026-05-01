// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

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
		"conditional-imports": {
			ID:           "conditional-imports",
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

	if enriched.Findings[0].SkillID != "conditional-imports" {
		t.Fatalf("finding skill = %q", enriched.Findings[0].SkillID)
	}
	if len(enriched.SkillHints) != 1 ||
		enriched.SkillHints[0].SkillID != "conditional-imports" ||
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
			"conditional-imports": {
				ID:           "conditional-imports",
				Description:  "Fix import structure.",
				ShortHint:    "Break cycles with Protocol-oriented boundaries.",
				PrincipleIDs: []string{"protocol-first-design"},
				TriggerTerms: []string{"cyclic-import", "import cycle"},
			},
		},
	)

	if len(hints) != 1 ||
		hints[0].SkillID != "conditional-imports" ||
		hints[0].PrincipleID != "protocol-first-design" {
		t.Fatalf("skill hints = %#v", hints)
	}
}
