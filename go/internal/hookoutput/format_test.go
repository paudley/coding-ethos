// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
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
		"advice[1]{principle_id,skill_id,message,next}:",
		"no-conditional-imports,conditional-imports,Conditional imports are banned; use protocols.,Load the conditional-imports skill for the remediation playbook.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, output)
		}
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
