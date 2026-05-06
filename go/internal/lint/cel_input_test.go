// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"bytes"
	"strings"
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
			findings.exists(item, item.file == finding.file && item.code == finding.code) &&
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

func TestEncodeResultAndOutputStatusHelpers(t *testing.T) {
	t.Parallel()

	result := Result{
		TraceID: "trace-1",
		Scope:   "tool:ruff",
		Status:  "blocked",
		Findings: []Finding{{
			SourceTool: "ruff",
			Code:       "F401",
			File:       "pkg/app.py",
			Line:       3,
			Severity:   "error",
			Message:    "unused import",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	if tool := ResultTool(result); tool != "ruff" {
		t.Fatalf("ResultTool() = %q, want ruff", tool)
	}

	if status := ResultStatus(result); status != "FAIL" {
		t.Fatalf("ResultStatus() = %q, want FAIL", status)
	}

	var buffer bytes.Buffer

	err := EncodeResult(&buffer, result)
	if err != nil {
		t.Fatalf("EncodeResult() error = %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, `"scope": "tool:ruff"`) ||
		!strings.Contains(output, `"trace_id"`) {
		t.Fatalf("encoded result did not use indented JSON result schema: %s", output)
	}

	pass := Result{Scope: "staged", Status: "passed"}
	if tool := ResultTool(pass); tool != "policy-lint" {
		t.Fatalf("default ResultTool() = %q, want policy-lint", tool)
	}

	if status := ResultStatus(pass); status != "PASS" {
		t.Fatalf("passing ResultStatus() = %q, want PASS", status)
	}
}
