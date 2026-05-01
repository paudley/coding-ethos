// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"encoding/json"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestFormatLintResultTOONUsesDiagnostics(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "pii",
			File:     ".codex/config.toml",
			Line:     8,
			Severity: "block",
			PolicyID: "repo.pii_scrubber",
			Message:  "local machine detail detected",
			Advice:   "Replace local paths with generic placeholders.",
			Detail:   "matched /" + "home/example/project",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"format: toon",
		"tool: policy-lint",
		"scope: staged",
		"findings[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
		"pii,.codex/config.toml,8,0,block,,repo.pii_scrubber,,local machine detail detected,Replace local paths with generic placeholders.,matched /" + "home/example/project",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, `"decisions"`) || strings.Contains(output, "{\n") {
		t.Fatalf("TOON output looks like raw JSON:\n%s", output)
	}
}

func TestFormatLintResultSARIFIncludesRuleMetadata(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:         "ruff",
			File:         "pkg/app.py",
			Line:         4,
			Column:       8,
			Severity:     "error",
			Code:         "F401",
			PolicyID:     "python.unused_imports",
			SkillID:      "lint-remediation",
			Message:      "unused import",
			Advice:       "Remove unused imports instead of suppressing Ruff.",
			Detail:       "imported but unused",
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
			Tags:         []string{"linting", "quality"},
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode SARIF: %v\n%s", err, output)
	}

	assertJSONPath(t, payload, "$schema", sarifSchema)
	assertJSONPath(t, payload, "version", sarifVersion)
	assertJSONPath(t, payload, "runs.0.tool.driver.name", "coding-ethos")
	assertJSONPath(t, payload, "runs.0.tool.driver.rules.0.id", "python.unused_imports")
	assertJSONPath(t, payload, "runs.0.tool.driver.rules.0.properties.policy_id", "python.unused_imports")
	assertJSONPath(t, payload, "runs.0.tool.driver.rules.0.properties.skill_id", "lint-remediation")
	assertJSONPath(t, payload, "runs.0.results.0.ruleId", "python.unused_imports")
	assertJSONPath(t, payload, "runs.0.results.0.level", "error")
	assertJSONPath(t, payload, "runs.0.results.0.message.text", "unused import")
	assertJSONPath(t, payload, "runs.0.results.0.locations.0.physicalLocation.artifactLocation.uri", "pkg/app.py")
	assertJSONPath(t, payload, "runs.0.results.0.locations.0.physicalLocation.region.startLine", float64(4))
	assertJSONPath(t, payload, "runs.0.results.0.locations.0.physicalLocation.region.startColumn", float64(8))
	assertJSONPath(t, payload, "runs.0.results.0.properties.policy_id", "python.unused_imports")
	assertJSONPath(t, payload, "runs.0.results.0.properties.skill_id", "lint-remediation")
	assertJSONPath(t, payload, "runs.0.results.0.properties.coding_ethos", true)
}

func TestFormatLintResultSARIFIncludesRepoLocationForPolicyFindings(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Decisions: []policy.Decision{{
			Decision: "block",
			Severity: "block",
			PolicyID: "repo.pii_scrubber",
			Message:  "Local-machine PII must not be committed.",
		}},
	}

	output, err := FormatLintResult(result, FormatSARIF)
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(output), &payload); err != nil {
		t.Fatalf("decode SARIF: %v\n%s", err, output)
	}

	assertJSONPath(t, payload, "runs.0.results.0.ruleId", "repo.pii_scrubber")
	assertJSONPath(t, payload, "runs.0.results.0.locations.0.physicalLocation.artifactLocation.uri", sarifRepoURI)
}

func TestFormatLintResultTOONDedupesDiagnostics(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:mypy",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{
			{
				Tool:     "mypy",
				File:     "pkg/app.py",
				Line:     12,
				Column:   4,
				Code:     "union-attr",
				PolicyID: "python.optional_required_types",
				Message:  "Item None has no attribute run",
				Advice:   "Make the required contract explicit.",
			},
			{
				Tool:     "pyright",
				File:     "pkg/app.py",
				Line:     12,
				Column:   4,
				Code:     "reportOptionalMemberAccess",
				PolicyID: "python.optional_required_types",
				Message:  "Object of type None has no run",
			},
		},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"findings[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
		"also reported by pyright:reportOptionalMemberAccess",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func assertJSONPath(t *testing.T, payload map[string]any, path string, want any) {
	t.Helper()

	var current any = payload
	for _, segment := range strings.Split(path, ".") {
		switch value := current.(type) {
		case map[string]any:
			var ok bool
			current, ok = value[segment]
			if !ok {
				t.Fatalf("JSON path %q missing segment %q in %#v", path, segment, value)
			}
		case []any:
			index := int(segment[0] - '0')
			if len(segment) != 1 || index < 0 || index >= len(value) {
				t.Fatalf("JSON path %q invalid index %q in %#v", path, segment, value)
			}
			current = value[index]
		default:
			t.Fatalf("JSON path %q cannot descend into %#v", path, value)
		}
	}

	if current != want {
		t.Fatalf("JSON path %q = %#v, want %#v", path, current, want)
	}
}

func TestFormatLintResultTOONIncludesSkillAdvice(t *testing.T) {
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
		SkillHints: []lint.SkillHint{{
			PrincipleID: "no-conditional-imports",
			SkillID:     "conditional-imports",
			Message:     "Conditional imports are banned; use protocols.",
			Next:        "Load the conditional-imports skill for the remediation playbook.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"advice[1]{skill_id,message}:",
		"conditional-imports,Conditional imports are banned; use protocols.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONKeepsLongSkillAdviceLineStable(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			File:     "pkg/app.py",
			Line:     12,
			Severity: "error",
			Code:     "S608",
			PolicyID: "python.sql_safety",
			SkillID:  "lint-remediation",
			Message:  "Possible SQL injection vector through string-based query construction",
		}},
		SkillHints: []lint.SkillHint{{
			PrincipleID: "static-analysis-is-the-first-line-of-defense",
			SkillID:     "lint-remediation",
			Message: "Linters are enforced code reviewers. Fix findings structurally, " +
				"keep managed config intact, and suppress only when technically necessary " +
				"and fully documented.",
			Next: "Load the lint-remediation skill for the remediation playbook.",
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"advice[1]{skill_id,message}:",
		"lint-remediation,Linters are enforced code reviewers.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "  lint-remediation,") && len(line) > 96 {
			t.Fatalf("TOON skill advice line too long: %d\n%s", len(line), line)
		}
	}
	if strings.Contains(output, "Load the lint-remediation skill") {
		t.Fatalf("TOON output should not inline verbose next-step text:\n%s", output)
	}
}

func TestFormatLintResultTOONPrefersBlockingDecisions(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  lint.ScopeStaged,
		Status: "blocked",
		Decisions: []policy.Decision{
			{
				Decision:   "record",
				Severity:   "record",
				PolicyID:   "shell.forbidden_strings",
				Message:    "Commands must not inspect protected internals.",
				Suggestion: "Use documented commands.",
			},
			{
				Decision:   "block",
				Severity:   "block",
				PolicyID:   "git.staged_admin_files",
				Message:    "Administrative staged files require explicit handling.",
				Suggestion: "Confirm the policy change is intentional.",
			},
		},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"findings[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
		"policy,,0,0,block,,git.staged_admin_files,,Administrative staged files require explicit handling.,Confirm the policy change is intentional.,",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
	if strings.Contains(output, "shell.forbidden_strings") {
		t.Fatalf("TOON output included non-blocking record finding:\n%s", output)
	}
}

func TestFormatLintResultTOONUsesCapturedFindings(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:mypy",
		Status: "blocked",
		Findings: []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  "configuration_error",
				"exit_code": 2,
				"output":    "mypy: error: cannot read file 'lbox/parsing/analyzer_base.py'",
			},
			CheckID:    "tool.mypy",
			Message:    "mypy configuration or usage failed with status 2",
			Severity:   "error",
			SourceTool: "mypy",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, want := range []string{
		"findings[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
		"mypy,,0,0,error,,tool.mypy,,mypy configuration or usage failed with status 2,,category=configuration_error; exit_code=2; output=mypy: error: cannot read file 'lbox/parsing/analyzer_base.py'",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONBlockedOutputQuality(t *testing.T) {
	t.Parallel()

	result := lint.Result{
		Scope:  "tool:pyright",
		Status: "blocked",
		Findings: []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  "tool_config_error",
				"exit_code": 2,
				"output":    "pyright: config failure in <repo>/pyrightconfig.json",
			},
			CheckID:    "tool.pyright",
			Message:    "pyright configuration or usage failed with status 2",
			Severity:   "error",
			SourceTool: "pyright",
			Status:     "fail",
			Blocking:   true,
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	for _, forbidden := range []string{
		"findings[0]",
		"/home/",
		"duration_ms",
		"groups[",
		"commands[",
	} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("TOON output contains forbidden %q:\n%s", forbidden, output)
		}
	}
	for _, want := range []string{
		"findings[1]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
		"tool.pyright",
		"pyright configuration or usage failed with status 2",
		"output=pyright: config failure in <repo>/pyrightconfig.json",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
	}
}

func TestFormatLintResultTOONTruncatesPathologicalFindingCells(t *testing.T) {
	t.Parallel()

	longMessage := strings.Repeat("schema-value,", 80)
	result := lint.Result{
		Scope:  "tool:tombi",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "tombi",
			File:     "pyproject.toml",
			Line:     104,
			Column:   5,
			Severity: "error",
			Message:  longMessage,
		}},
	}

	output, err := FormatLintResult(result, FormatTOON)
	if err != nil {
		t.Fatalf("format lint result: %v", err)
	}

	if !strings.Contains(output, "...[truncated]") {
		t.Fatalf("TOON output did not mark truncation:\n%s", output)
	}
	if strings.Contains(output, longMessage) {
		t.Fatalf("TOON output included full pathological message:\n%s", output)
	}
	if len(output) > 800 {
		t.Fatalf("TOON output too large after truncation: %d bytes\n%s", len(output), output)
	}
}

func TestFormatLintResultCELBackedGoldenSurfaces(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		result lint.Result
		want   string
	}{
		{
			name: "command policy",
			result: lint.Result{
				Scope:      "files",
				Status:     "blocked",
				SkillHints: celSkillHints(),
				Decisions: []policy.Decision{celDecision(
					"custom.no_subprocess_git",
					"",
					"Git subprocesses are forbidden.",
				)},
			},
			want: "custom.no_subprocess_git",
		},
		{
			name: "file policy",
			result: lint.Result{
				Scope:      "files",
				Status:     "blocked",
				SkillHints: celSkillHints(),
				Decisions: []policy.Decision{celDecision(
					"custom.generated_python",
					"generated/model.py",
					"Generated Python must not be edited directly.",
				)},
			},
			want: "generated/model.py",
		},
		{
			name: "diagnostic policy",
			result: lint.Result{
				Scope:      "tool:ruff",
				Status:     "blocked",
				SkillHints: celSkillHints(),
				Diagnostics: []diagnostics.Diagnostic{{
					Tool:     "policy",
					File:     "pkg/app.py",
					Line:     10,
					Severity: "block",
					PolicyID: "custom.ruff_policy",
					SkillID:  "lint-remediation",
					Message:  "Ruff diagnostic maps to ETHOS policy.",
					Advice:   "Fix the diagnostic structurally.",
				}},
			},
			want: "custom.ruff_policy",
		},
		{
			name: "lint finding policy",
			result: lint.Result{
				Scope:      "tool:mypy",
				Status:     "blocked",
				SkillHints: celSkillHints(),
				Findings: []lint.Finding{{
					CheckID:    "custom.mypy_policy",
					PolicyID:   "custom.mypy_policy",
					SkillID:    "lint-remediation",
					Message:    "Mypy finding maps to ETHOS policy.",
					Severity:   "error",
					SourceTool: "mypy",
					Status:     "fail",
					Blocking:   true,
				}},
			},
			want: "custom.mypy_policy",
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			for _, format := range []string{FormatTOON, FormatJSON, FormatHuman} {
				output, err := FormatLintResult(testCase.result, format)
				if err != nil {
					t.Fatalf("format %s lint result: %v", format, err)
				}
				for _, want := range []string{
					testCase.want,
					"lint-remediation",
				} {
					if !strings.Contains(output, want) {
						t.Fatalf("%s output missing %q:\n%s", format, want, output)
					}
				}
				if format != FormatJSON &&
					!strings.Contains(output, "Fix the reported diagnostics before continuing.") {
					t.Fatalf("%s output missing guidance:\n%s", format, output)
				}
			}
		})
	}
}

func TestSelectedFormatAutoDetectsAgent(t *testing.T) {
	getenv := func(name string) string {
		switch name {
		case FormatEnv:
			return FormatAuto
		case "CODEX_THREAD_ID":
			return "thread"
		default:
			return ""
		}
	}

	if got := SelectedFormatWithEnv(getenv); got != FormatTOON {
		t.Fatalf("SelectedFormatWithEnv() = %q, want toon", got)
	}
}

func TestTOONCellEscapesCommasAndNewlines(t *testing.T) {
	t.Parallel()

	got := TOONCell("a,b\nc")
	if got != `a\,b\nc` {
		t.Fatalf("TOONCell() = %q", got)
	}
}

func celDecision(policyID string, file string, message string) policy.Decision {
	decision := policy.Decision{
		Decision:   "block",
		Severity:   "block",
		PolicyID:   policyID,
		Message:    message,
		Suggestion: "Load the lint remediation skill.",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "policy",
			File:     file,
			Severity: "block",
			PolicyID: policyID,
			SkillID:  "lint-remediation",
			Message:  message,
			Advice:   "Load the lint remediation skill.",
		}},
	}

	return decision
}

func celSkillHints() []lint.SkillHint {
	return []lint.SkillHint{{
		PrincipleID: "linting-as-code-quality-enforcement",
		SkillID:     "lint-remediation",
		Message:     "Resolve CEL-backed lint findings structurally.",
		Next:        "Load the lint-remediation skill for the remediation playbook.",
	}}
}
