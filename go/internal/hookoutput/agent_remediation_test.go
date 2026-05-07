// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput_test

import (
	"encoding/json"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

func TestFormatLintResultJSONIncludesAgentRemediation(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:         "ruff",
			File:         "pkg/app.py",
			Line:         12,
			Severity:     "error",
			Code:         "PLC" + "0415",
			PolicyID:     "python.conditional_imports",
			SkillID:      "conditional-imports",
			Message:      "import outside top-level",
			PrincipleIDs: []string{"no-conditional-imports"},
		}},
	}

	output, err := FormatLintResult(result, FormatJSON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	var payload map[string]any

	inlineErr0 := json.Unmarshal([]byte(output), &payload)
	if inlineErr0 != nil {
		t.Fatalf("decode JSON: %v\n%s", inlineErr0, output)
	}

	assertJSONPath(
		t,
		payload,
		"agent_remediation.0.policy_id",
		"python.conditional_imports",
	)
	assertJSONPath(t, payload, "agent_remediation.0.skill_id", "conditional-imports")
	assertJSONPath(t, payload, "agent_remediation.0.mcp.tool", "policy_explain")
	assertJSONPath(
		t,
		payload,
		"agent_remediation.0.mcp.arguments.policy_id",
		"python.conditional_imports",
	)
	assertJSONPath(
		t,
		payload,
		"agent_remediation.0.next_steps.0",
		"Call MCP policy_explain with policy_id=python.conditional_imports before retrying.",
	)
}

func TestFormatLintResultTOONIncludesAgentRemediation(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     12,
			Severity: "error",
			Code:     "PLC" + "0415",
			PolicyID: "python.conditional_imports",
			SkillID:  "conditional-imports",
			Message:  "import outside top-level",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"agent_remediation[1]{policy_id,skill_id,file,line,next,mcp_tool}:",
		"python.conditional_imports,conditional-imports,pkg/app.py,12," +
			"Call MCP policy_explain with policy_id=python.conditional_imports " +
			"before retrying.,policy_explain",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultSARIFIncludesNormalizedEvidence(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     4,
			Column:   8,
			Severity: "error",
			Code:     "F401",
			PolicyID: "python.unused_imports",
			SkillID:  "lint-remediation",
			Message:  "unused import",
			Metadata: map[string]any{
				"language":    "python",
				"symbol_kind": "module",
				"symbol_name": "pkg.app",
			},
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any

	inlineErr1 := json.Unmarshal([]byte(output), &payload)
	if inlineErr1 != nil {
		t.Fatalf("decode SARIF: %v\n%s", inlineErr1, output)
	}

	assertSARIFNormalizedRemediation(t, payload)
	assertSARIFNormalizedFinding(t, payload)
}

func assertSARIFNormalizedRemediation(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.results.0.partialFingerprints.coding-ethos/finding/v1",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.agent_remediation.0.policy_id",
		"python.unused_imports",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.agent_remediation.0.skill_id",
		"lint-remediation",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.agent_remediation.0.mcp.tool",
		"policy_explain",
	)
}

func assertSARIFNormalizedFinding(t *testing.T, payload map[string]any) {
	t.Helper()

	assertJSONPathPrefix(t, payload, "runs.0.results.0.properties.finding.id")
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.finding.source_span.language",
		"python",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.finding.source_span.symbol_kind",
		"module",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.source_span.path",
		"pkg/app.py",
	)
	assertJSONPath(
		t,
		payload,
		"runs.0.results.0.properties.source_span.start_line",
		float64(4),
	)
	assertJSONPathPrefix(
		t,
		payload,
		"runs.0.results.0.properties.remediation_events.0.id",
	)
}
