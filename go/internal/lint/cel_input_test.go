// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/celexpr"
)

func TestFindingPopulatesCELFindingInput(t *testing.T) {
	t.Parallel()

	program, err := celexpr.Program(
		"test.finding_input",
		`
			finding.tool == "ruff" &&
			finding.code == "F401" &&
			finding.message == "unused import" &&
			finding.file == "src/app.py" &&
			finding.line == 14 &&
			finding.severity == "error" &&
			finding.policy_id == "python.direct_imports" &&
			finding.skill_id == "lint-remediation" &&
			finding.principle_ids.exists(id, id == "static-analysis-is-the-first-line-of-defense") &&
			paths.exists(path, path.file == finding.file)
		`,
	)
	if err != nil {
		t.Fatalf("compile CEL finding expression: %v", err)
	}

	output, _, err := program.Eval(celexpr.ActivationForFinding(
		celexpr.ActivationInput{},
		Finding{
			SourceTool: "ruff",
			Code:       "F401",
			Message:    "unused import",
			File:       "./src/app.py",
			Line:       14,
			Severity:   "error",
			PolicyID:   "python.direct_imports",
			SkillID:    "lint-remediation",
			EthosIDs:   []string{"static-analysis-is-the-first-line-of-defense"},
		},
	))
	if err != nil {
		t.Fatalf("evaluate CEL finding expression: %v", err)
	}
	if matched, ok := output.Value().(bool); !ok || !matched {
		t.Fatalf("finding expression output = %#v, want true", output.Value())
	}
}
