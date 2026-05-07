// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

const executableFixtureMode os.FileMode = 0o700

func TestRunCapturedToolLogsRuffTrace(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "ruff-fixture")
	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --output-format=json "*) ;;
  *) echo "missing json output flag" >&2; exit 2 ;;
esac
cat <<'JSON'
[
  {
    "filename": "pkg/app.py",
    "code": "F401",
    "message": "unused import",
    "location": {"row": 4, "column": 8}
  }
]
JSON
exit 1
`)

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	output := captureStdout(t, func() {
		exitCode := runCapturedTool(
			"ruff",
			tool,
			repo,
			"",
			[]string{"check", "pkg/app.py"},
			ruffTracePolicyContext(),
			"",
		)
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
	})
	for _, want := range ruffTraceExpectedOutput() {
		if !strings.Contains(output, want) {
			t.Fatalf("normalized output missing %q:\n%s", want, output)
		}
	}

	matches, err := filepath.Glob(
		filepath.Join(repo, ".coding-ethos", "lint-runs", "*.json"),
	)
	if err != nil {
		t.Fatalf("glob traces: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("trace files = %#v", matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}

	for _, want := range ruffTraceExpectedTrace() {
		if !strings.Contains(string(content), want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func ruffTracePolicyContext() capturePolicyData {
	return capturePolicyData{
		EvidenceMaps: []diagnostics.EvidenceMap{{
			Source:       "ruff",
			Codes:        []string{"F401"},
			PolicyID:     "python.unused_imports",
			SkillID:      "lint-remediation",
			PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
			Advice: diagnostics.EvidenceAdvice{
				Summary: "Remove unused imports instead of suppressing Ruff.",
			},
		}},
		Skills: map[string]policy.Skill{
			"lint-remediation": ruffTraceSkill(),
		},
	}
}

func ruffTraceSkill() policy.Skill {
	return policy.Skill{
		ID:           "lint-remediation",
		Description:  "Fix lint structurally.",
		ShortHint:    "Fix lint structurally; do not weaken policy or add suppressions.",
		PrincipleIDs: []string{"linting-as-code-quality-enforcement"},
	}
}

func ruffTraceExpectedOutput() []string {
	return []string{
		"format: toon",
		"tool: ruff",
		"status: FAIL",
		strings.Join([]string{
			"ruff", "pkg/app.py", "4", "8", "error", "F401",
			"python.unused_imports", "lint-remediation", "unused import",
			"Remove unused imports instead of suppressing Ruff.",
		}, ","),
		"advice[1]{skill_id,message}:",
		"lint-remediation,Fix lint structurally; do not weaken policy or add suppressions.",
	}
}

func ruffTraceExpectedTrace() []string {
	return []string{
		`"scope": "tool:ruff"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
		`"policy_id": "python.unused_imports"`,
		`"skill_id": "lint-remediation"`,
		`"skill_hints": [`,
		`"message": "unused import"`,
		`"advice": "Remove unused imports instead of suppressing Ruff."`,
	}
}

func TestRunCapturedToolRendersSARIF(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "ruff-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --output-format=json "*) ;;
  *) echo "missing json output flag" >&2; exit 2 ;;
esac
cat <<'JSON'
[
  {
    "filename": "pkg/app.py",
    "code": "F401",
    "message": "unused import",
    "location": {"row": 4, "column": 8}
  }
]
JSON
exit 1
`)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"ruff",
		tool,
		repo,
		[]string{"check", "pkg/app.py"},
		capturePolicyData{
			EvidenceMaps: []diagnostics.EvidenceMap{{
				Source:       "ruff",
				Codes:        []string{"F401"},
				PolicyID:     "python.unused_imports",
				SkillID:      "lint-remediation",
				PrincipleIDs: []string{"static-analysis-is-the-first-line-of-defense"},
			}},
		},
		hookoutput.FormatSARIF,
		&output,
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	for _, want := range []string{
		`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
		`"ruleId": "python.unused_imports"`,
		`"uri": "pkg/app.py"`,
		`"policy_id": "python.unused_imports"`,
		`"skill_id": "lint-remediation"`,
		`"ethos_ids": [`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("SARIF output missing %q:\n%s", want, output.String())
		}
	}
}

func TestRunCapturedRuffWarningOutputCanBePromotedByCELIntoSARIFError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "ruff-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --output-format=json "*) ;;
  *) echo "missing json output flag" >&2; exit 2 ;;
esac
echo 'warning: top-level linter warning that does not affect exit status' >&2
exit 0
`)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"ruff",
		tool,
		repo,
		[]string{"check", "pkg/app.py"},
		ruffWarningPolicyContext(),
		hookoutput.FormatSARIF,
		&output,
	)
	if exitCode != blockedExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, blockedExitCode)
	}

	for _, want := range []string{
		`"ruleId": "tool.ruff.warning_output_denied"`,
		`"level": "error"`,
		`"uri": "pkg/app.py"`,
		`"matched_diagnostic_policy_id": "tool.output_visible"`,
		`"matched_diagnostic_severity": "warning"`,
		`"cel_expression": "findings.exists`,
		`"executionSuccessful": false`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("SARIF output missing %q:\n%s", want, output.String())
		}
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"policy_id": "tool.output_visible"`,
		`"policy_id": "tool.ruff.warning_output_denied"`,
		`"output_excerpt": "warning: top-level linter warning`,
		`"status": "blocked"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func ruffWarningPolicyContext() capturePolicyData {
	return capturePolicyData{
		Policies: []policy.Policy{{
			ID:              "tool.ruff.warning_output_denied",
			Category:        "expression",
			DefaultSeverity: "block",
			Message:         "Ruff warning output is blocking under policy.",
			Suggestion:      "Read and resolve ruff warning output before committing.",
			AppliesTo:       policy.AppliesTo{Tools: []string{"ruff"}},
			DefenseLayers:   policy.CodeDefenseLayers(),
			SupportedModes:  []string{"block", "record", "advise"},
			PrincipleIDs:    []string{"radical-visibility"},
			Evaluators:      []policy.Evaluator{ruffWarningEvaluator()},
		}},
	}
}

func ruffWarningEvaluator() policy.Evaluator {
	return policy.Evaluator{
		Kind: "cel",
		Name: "cel.expression",
		Options: map[string]any{
			"when": `findings.exists(item,
				item.tool == "ruff" &&
				item.policy_id == "tool.output_visible" &&
				item.severity == "warning"
			)`,
		},
	}
}

func TestRunCapturedRuffWarningOutputDisplaysWhenToolPasses(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "ruff-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --output-format=json "*) ;;
  *) echo "missing json output flag" >&2; exit 2 ;;
esac
echo 'warning: ruff emitted a non-fatal warning' >&2
exit 0
`)

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
	output := captureStdout(t, func() {
		exitCode := runCapturedTool(
			"ruff",
			tool,
			repo,
			"",
			[]string{"check", "pkg/app.py"},
			capturePolicyData{},
			"",
		)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", exitCode)
		}
	})

	for _, want := range []string{
		"format: toon",
		"tool: ruff",
		"status: PASS",
		strings.Join([]string{
			"ruff",
			"pkg/app.py",
			"0",
			"0",
			"warning",
			"TOOL_OUTPUT",
			"tool.output_visible",
			"",
			"ruff emitted output while passing",
		}, ","),
		"warning: ruff emitted a non-fatal warning",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}
}

func TestRunCapturedToolRecordsSandboxDenialInTraceAndSARIF(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	var output bytes.Buffer

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:               "ruff",
		ToolPath:           filepath.Join(repo, "ruff"),
		Cwd:                repo,
		TraceRoot:          repo,
		Args:               []string{"check", "pkg/app.py"},
		SandboxMode:        sandbox.ModeRequired,
		SandboxBackendPath: filepath.Join(repo, "missing-bwrap"),
		Output:             &output,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"."},
			WritePaths:     []string{".coding-ethos/cache"},
		},
	}, hookoutput.FormatSARIF)
	if exitCode != blockedExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, blockedExitCode)
	}

	for _, want := range []string{
		`"sandbox": {`,
		`"profile": "lint-offline"`,
		`"denied": true`,
		`"reason": "bubblewrap executable not found"`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("SARIF output missing %q:\n%s", want, output.String())
		}
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"policy_id": "runtime.sandbox_denial"`,
		`"skill_id": "managed-toolchain"`,
		`"security-by-design"`,
		`"one-path-for-critical-operations"`,
		`"advice": "Use the managed tool path with declared capabilities`,
		`"sandbox": {`,
		`"denied": true`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestPrepareSandboxCgroupSkipsWhenSandboxDisabled(t *testing.T) {
	t.Parallel()

	cgroup, evidence, err := prepareSandboxCgroup(sandbox.Evidence{
		Mode:            sandbox.ModeOff,
		CgroupRequested: true,
		MemoryMB:        2048,
		CPUQuotaPercent: 100,
	})
	if err != nil {
		t.Fatalf("prepare cgroup returned error: %v", err)
	}

	if cgroup != nil {
		t.Fatal("prepare cgroup returned cgroup while sandbox disabled")
	}

	if evidence.CgroupEnabled {
		t.Fatalf("cgroup evidence enabled with sandbox disabled: %#v", evidence)
	}
}

func TestRunCapturedToolReportsStartFailureDetail(t *testing.T) {
	repo := t.TempDir()
	missingTool := filepath.Join(repo, "missing-tool")

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
	output := captureStdout(t, func() {
		exitCode := runCapturedTool(
			"actionlint",
			missingTool,
			repo,
			"",
			[]string{".github/workflows/ci.yml"},
			capturePolicyData{},
			"",
		)
		if exitCode != 127 {
			t.Fatalf("exit code = %d, want 127", exitCode)
		}
	})

	for _, want := range []string{
		"tool: actionlint",
		"actionlint exited with status 127 without parseable diagnostics",
		"fork/exec",
		"missing-tool",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"scope": "tool:actionlint"`,
		`"exit_code": 127`,
		`"output_excerpt": "fork/exec`,
		`"message": "actionlint exited with status 127 without parseable diagnostics"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestRunCapturedToolLogsShellcheckTrace(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "shellcheck-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --format=json "*) ;;
  *) echo "missing shellcheck json flag" >&2; exit 2 ;;
esac
cat <<'JSON'
{
  "comments": [
    {
      "file": "script.sh",
      "line": 3,
      "column": 7,
      "level": "warning",
      "code": 2086,
      "message": "Double quote"
    }
  ]
}
JSON
exit 1
`)

	exitCode := runCapturedTool(
		"shellcheck",
		tool,
		repo,
		"",
		[]string{"script.sh"},
		capturePolicyData{},
		"",
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"scope": "tool:shellcheck"`,
		`"source_tool": "shellcheck"`,
		`"code": "SC2086"`,
		`"message": "Double quote"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestRunCapturedToolDerivesSkillFromTriggerTerms(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "pylint-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --output-format=json "*) ;;
  *) echo "missing pylint json flag" >&2; exit 2 ;;
esac
cat <<'JSON'
[
  {
    "path": "pkg/app.py",
    "type": "error",
    "symbol": "cyclic-import",
    "message": "Cyclic import",
    "line": 9,
    "column": 0
  }
]
JSON
exit 1
`)

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
	output := captureStdout(t, func() {
		exitCode := runCapturedTool(
			"pylint",
			tool,
			repo,
			"",
			[]string{"pkg"},
			capturePolicyData{
				Skills: map[string]policy.Skill{
					"conditional-imports": {
						ID:           "conditional-imports",
						Description:  "Fix import cycles structurally.",
						ShortHint:    "Break cycles with Protocol-oriented boundaries.",
						PrincipleIDs: []string{"protocol-first-design"},
						TriggerTerms: []string{"cyclic-import"},
					},
				},
			},
			"",
		)
		if exitCode != 1 {
			t.Fatalf("exit code = %d, want 1", exitCode)
		}
	})

	for _, want := range []string{
		"pylint,pkg/app.py,9,1,error,cyclic-import,,conditional-imports,Cyclic import",
		"advice[1]{skill_id,message}:",
		"conditional-imports,Break cycles with Protocol-oriented boundaries.",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q:\n%s", want, output)
		}
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"skill_id": "conditional-imports"`,
		`"skill_hints": [`,
		`"skill_id": "conditional-imports"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestRunCapturedToolSeparatesExecutionCWDFromTraceRoot(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	traceRoot := t.TempDir()

	execRoot := t.TempDir()

	err := os.Mkdir(filepath.Join(execRoot, "pkg"), 0o700)
	if err != nil {
		t.Fatalf("create package dir: %v", err)
	}

	tool := filepath.Join(traceRoot, "mypy-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
test -f pkg/app.py || { echo "wrong cwd: $PWD" >&2; exit 2; }
printf '%s' \
  '{"file":"pkg/app.py",' \
  '"line":3,' \
  '"column":4,' \
  '"severity":"error",' \
  '"code":"assignment",' \
  '"message":"bad type"}'
printf '\n'
exit 1
`)

	err = os.WriteFile(
		filepath.Join(execRoot, "pkg", "app.py"),
		[]byte("x = 1\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write package file: %v", err)
	}

	exitCode := runCapturedTool(
		"mypy",
		tool,
		execRoot,
		traceRoot,
		[]string{"pkg/app.py"},
		capturePolicyData{},
		"",
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	matches, err := filepath.Glob(
		filepath.Join(execRoot, ".coding-ethos", "lint-runs", "*.json"),
	)
	if err != nil {
		t.Fatalf("glob execution cwd traces: %v", err)
	}

	if len(matches) != 0 {
		t.Fatalf("trace should not be written under execution cwd: %#v", matches)
	}

	content := singleTraceContent(t, traceRoot)
	for _, want := range []string{
		`"repo_root": "` + traceRoot + `"`,
		`"file": "pkg/app.py"`,
		`"code": "assignment"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestCapturedFindingsClassifyUnparseableToolFailures(t *testing.T) {
	t.Parallel()

	findings := capturedFindings(
		captureRequest{Tool: "pyright", Args: []string{"pkg"}},
		captureExecution{RunArgs: []string{"--outputjson", "pkg"}, ExitCode: 2},
		"pyright: config file not found in <repo>/pyrightconfig.json",
		nil,
	)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}

	if findings[0].RawOutcome["category"] != "configuration_error" {
		t.Fatalf("raw outcome = %#v", findings[0].RawOutcome)
	}

	if !strings.Contains(findings[0].Message, "configuration or usage failed") {
		t.Fatalf("message = %q", findings[0].Message)
	}

	wantOutput := "pyright: config file not found in <repo>/pyrightconfig.json"
	if findings[0].RawOutcome["output"] != wantOutput {
		t.Fatalf("raw outcome output = %#v", findings[0].RawOutcome)
	}
}

func TestCapturedToolResultRecordsCaptureMetadata(t *testing.T) {
	t.Parallel()

	result := capturedToolResult(
		captureRequest{
			Tool:      "mypy",
			Cwd:       "/work/repo",
			TraceRoot: "/work/repo",
			Args:      []string{"pkg/app.py"},
		},
		captureExecution{
			RunArgs:  []string{"--output=json", "pkg/app.py"},
			Stderr:   "mypy: error: cannot read file '/work/repo/pkg/app.py'",
			ExitCode: 2,
		},
	)

	if result.Capture == nil {
		t.Fatal("capture metadata missing")
	}

	if result.Capture.ParseStatus != "tool_config_error" {
		t.Fatalf("parse status = %q", result.Capture.ParseStatus)
	}

	wantOutput := "mypy: error: cannot read file '<repo>/pkg/app.py'"
	if result.Capture.OutputExcerpt != wantOutput {
		t.Fatalf("output excerpt = %q", result.Capture.OutputExcerpt)
	}

	if len(result.Findings) != 1 || result.Findings[0].RawOutcome["output"] == "" {
		t.Fatalf("captured findings missing output: %#v", result.Findings)
	}
}

func TestCapturedToolResultNormalizesAbsoluteDiagnosticsToTraceRoot(t *testing.T) {
	t.Parallel()

	traceRoot := filepath.Join(string(filepath.Separator), "work", "consumer")
	toolRoot := filepath.Join(string(filepath.Separator), "work", "coding-ethos")
	absoluteFile := filepath.Join(traceRoot, "pkg", "app.py")

	result := capturedToolResult(
		captureRequest{
			Tool:      "ruff",
			Cwd:       toolRoot,
			TraceRoot: traceRoot,
			Args:      []string{"check", absoluteFile},
		},
		captureExecution{
			RunArgs: []string{"check", "--output-format=json", absoluteFile},
			Stdout: `[` +
				`{"filename":"` + filepath.ToSlash(absoluteFile) + `",` +
				`"code":"F401","message":"unused import",` +
				`"location":{"row":4,"column":8}}` +
				`]`,
			ExitCode: 1,
		},
	)

	if len(result.Diagnostics) != 1 {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}

	if result.Diagnostics[0].File != "pkg/app.py" {
		t.Fatalf("diagnostic file = %q, want repo-relative", result.Diagnostics[0].File)
	}

	if len(result.Findings) != 1 || result.Findings[0].File != "pkg/app.py" {
		t.Fatalf("findings = %#v", result.Findings)
	}
}

func TestRunCapturedToolLogsForcedStructuredFormats(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	for _, test := range forcedStructuredFormatCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			repo := t.TempDir()
			tool := writeCaptureFixtureTool(t, repo, test.required, test.output)

			exitCode := runCapturedToolForTest(
				test.tool,
				tool,
				repo,
				test.args,
				capturePolicyData{},
				"",
				io.Discard,
			)
			if exitCode != 1 {
				t.Fatalf("exit code = %d, want 1", exitCode)
			}

			content := singleTraceContent(t, repo)
			for _, want := range []string{
				`"source_tool": "` + test.wantTool + `"`,
				`"file": "` + test.wantFile + `"`,
				`"code": "` + test.wantCode + `"`,
			} {
				if !strings.Contains(content, want) {
					t.Fatalf("trace missing %q:\n%s", want, content)
				}
			}
		})
	}
}

type forcedStructuredFormatCase struct {
	name     string
	tool     string
	required string
	output   string
	wantCode string
	wantTool string
	wantFile string
	args     []string
}

func forcedStructuredFormatCases() []forcedStructuredFormatCase {
	return []forcedStructuredFormatCase{
		forcedFormatCase("mypy", "--output=json", mypyJSONFixture(), "no-any-return"),
		forcedFormatCase(
			"pyright",
			"--outputjson",
			pyrightJSONFixture(),
			"reportAssignmentType",
		),
		forcedFormatCase(
			"pylint",
			"--output-format=json",
			pylintJSONFixture(),
			"cyclic-import",
		),
		forcedGolangciCase(),
		forcedActionlintCase(),
		forcedHadolintCase(),
		forcedYamllintCase(),
	}
}

func forcedFormatCase(
	tool string,
	required string,
	output string,
	wantCode string,
) forcedStructuredFormatCase {
	return forcedStructuredFormatCase{
		name:     tool,
		tool:     tool,
		args:     []string{"pkg"},
		required: required,
		output:   output,
		wantCode: wantCode,
		wantTool: tool,
		wantFile: "pkg/app.py",
	}
}

func forcedGolangciCase() forcedStructuredFormatCase {
	return forcedStructuredFormatCase{
		name:     "golangci-lint",
		tool:     "golangci-lint",
		args:     []string{"run", "./..."},
		required: "--output.json.path=stdout",
		output:   golangciJSONFixture(),
		wantCode: "errcheck",
		wantTool: "golangci-lint",
		wantFile: "pkg/app.go",
	}
}

func forcedActionlintCase() forcedStructuredFormatCase {
	return forcedStructuredFormatCase{
		name:     "actionlint",
		tool:     "actionlint",
		args:     []string{".github/workflows/ci.yml"},
		required: "{{json .}}",
		output:   actionlintJSONFixture(),
		wantCode: "syntax-check",
		wantTool: "actionlint",
		wantFile: ".github/workflows/ci.yml",
	}
}

func forcedHadolintCase() forcedStructuredFormatCase {
	return forcedStructuredFormatCase{
		name:     "hadolint",
		tool:     "hadolint",
		args:     []string{"Dockerfile"},
		required: "--format json",
		output:   hadolintJSONFixture(),
		wantCode: "DL3008",
		wantTool: "hadolint",
		wantFile: "Dockerfile",
	}
}

func forcedYamllintCase() forcedStructuredFormatCase {
	return forcedStructuredFormatCase{
		name:     "yamllint",
		tool:     "yamllint",
		args:     []string{"config.yaml"},
		required: "-f parsable",
		output:   `config.yaml:2:5: [error] wrong indentation (indentation)`,
		wantCode: "indentation",
		wantTool: "yamllint",
		wantFile: "config.yaml",
	}
}

type unparseableFailureCase struct {
	name     string
	tool     string
	required string
	args     []string
}

func unparseableFailureCases() []unparseableFailureCase {
	return []unparseableFailureCase{
		{
			name:     "ruff",
			tool:     "ruff",
			args:     []string{"check", "pkg"},
			required: "--output-format=json",
		},
		{name: "mypy", tool: "mypy", args: []string{"pkg"}, required: "--output=json"},
		{
			name:     "pyright",
			tool:     "pyright",
			args:     []string{"pkg"},
			required: "--outputjson",
		},
		{
			name:     "pylint",
			tool:     "pylint",
			args:     []string{"pkg"},
			required: "--output-format=json",
		},
		{
			name:     "shellcheck",
			tool:     "shellcheck",
			args:     []string{"script.sh"},
			required: "--format=json",
		},
		{
			name:     "yamllint",
			tool:     "yamllint",
			args:     []string{"config.yaml"},
			required: "-f parsable",
		},
		{
			name:     "hadolint",
			tool:     "hadolint",
			args:     []string{"Dockerfile"},
			required: "--format json",
		},
		{
			name:     "actionlint",
			tool:     "actionlint",
			args:     []string{".github/workflows/ci.yml"},
			required: "{{json .}}",
		},
		{
			name:     "golangci-lint",
			tool:     "golangci-lint",
			args:     []string{"run", "./..."},
			required: "--output.json.path=stdout",
		},
	}
}

func TestRunCapturedToolRendersUnparseableFailuresForEveryManagedTool(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	for _, test := range unparseableFailureCases() {
		t.Run(test.name, func(t *testing.T) {
			repo := t.TempDir()
			tool := writeFailureCaptureFixtureTool(
				t,
				repo,
				test.required,
				test.tool+": failed to load config from "+repo+"/tool.conf",
				2,
			)

			t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
			output := captureStdout(t, func() {
				exitCode := runCapturedTool(
					test.tool,
					tool,
					repo,
					"",
					test.args,
					capturePolicyData{},
					"",
				)
				if exitCode != 2 {
					t.Fatalf("exit code = %d, want 2", exitCode)
				}
			})

			for _, want := range []string{
				"format: toon",
				"tool: " + test.tool,
				"status: FAIL",
				strings.Join([]string{
					"findings[1]{tool",
					"file",
					"line",
					"column",
					"severity",
					"code",
					"policy_id",
					"skill_id",
					"message",
					"advice",
					"detail}:",
				}, ","),
				test.tool + ",,0,0,error,,tool." + test.tool,
				"category=configuration_error; exit_code=2; output=" +
					test.tool + ": failed to load config from <repo>/tool.conf",
			} {
				if !strings.Contains(output, want) {
					t.Fatalf("output missing %q:\n%s", want, output)
				}
			}

			if strings.Contains(output, "findings[0]") ||
				strings.Contains(output, repo) {
				t.Fatalf("output failed quality checks:\n%s", output)
			}
		})
	}
}

func TestRedactCapturedOutputPathsPrefersLongestPath(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join(string(filepath.Separator), "home", "dev", "repo")
	output := "error in " + filepath.Join(repoRoot, "pkg", "app.py")

	redacted := redactCapturedOutputPaths(output, repoRoot, "")

	if redacted != "error in <repo>/pkg/app.py" {
		t.Fatalf("redacted output = %q", redacted)
	}
}

func TestRunCapturedToolSilentOnCleanSuccess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	tool := writeSuccessCaptureFixtureTool(t, repo, "--output=json")

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
	output := captureStdout(t, func() {
		exitCode := runCapturedTool(
			"mypy",
			tool,
			repo,
			"",
			[]string{"pkg"},
			capturePolicyData{},
			"",
		)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", exitCode)
		}
	})

	if strings.TrimSpace(output) != "" {
		t.Fatalf("clean captured tool produced output:\n%s", output)
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"parse_status": "empty"`,
		`"exit_code": 0`,
		`"run_args": [`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestCapturedOutputExcerptTreatsEmptyMachinePayloadsAsSilent(t *testing.T) {
	t.Parallel()

	for _, output := range []string{"[]", "{}", "null", " \n [ ] \n"} {
		if got := capturedOutputExcerpt(output, "", "", ""); got != "" {
			t.Fatalf("capturedOutputExcerpt(%q) = %q, want empty", output, got)
		}
	}
}

func TestCapturedToolArgsForceMachineReadableOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		tool string
		args []string
		want []string
	}{
		{
			name: "ruff",
			tool: "ruff",
			args: []string{"check", "pkg"},
			want: []string{"check", "--output-format=json", "pkg"},
		},
		{
			name: "pyright",
			tool: "pyright",
			args: []string{"pkg"},
			want: []string{"--outputjson", "pkg"},
		},
		{
			name: "mypy",
			tool: "mypy",
			args: []string{"pkg"},
			want: []string{"--output=json", "pkg"},
		},
		{
			name: "pylint",
			tool: "pylint",
			args: []string{"pkg"},
			want: []string{"--output-format=json", "pkg"},
		},
		{
			name: "shellcheck",
			tool: "shellcheck",
			args: []string{"script.sh"},
			want: []string{"--format=json", "script.sh"},
		},
		{
			name: "yamllint",
			tool: "yamllint",
			args: []string{"config.yaml"},
			want: []string{"-f", "parsable", "config.yaml"},
		},
		{
			name: "hadolint",
			tool: "hadolint",
			args: []string{"Dockerfile"},
			want: []string{"--format", "json", "Dockerfile"},
		},
		{
			name: "actionlint",
			tool: "actionlint",
			args: []string{".github/workflows/ci.yml"},
			want: []string{"-format", "{{json .}}", ".github/workflows/ci.yml"},
		},
		{
			name: "golangci",
			tool: "golangci-lint",
			args: []string{"run", "./..."},
			want: []string{
				"run",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
				"./...",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := capturedToolArgs(test.tool, test.args)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("capturedToolArgs() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestCapturedToolArgsOverrideExplicitOutputFormat(t *testing.T) {
	t.Parallel()

	args := []string{"check", "--output-format=github", "pkg"}
	got := capturedToolArgs("ruff", args)

	want := []string{"check", "--output-format=json", "pkg"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capturedToolArgs() = %#v, want %#v", got, want)
	}
}

func writeCaptureFixtureTool(
	t *testing.T,
	repo string,
	required string,
	output string,
) string {
	t.Helper()

	tool := filepath.Join(repo, "tool-fixture")

	script := `#!/usr/bin/env sh
case " $* " in
  *"` + required + `"*) ;;
  *) echo "missing required output flags: ` + required + `" >&2; exit 2 ;;
esac
cat <<'EOF'
` + output + `
EOF
exit 1
`

	writeExecutableFixture(t, tool, script)

	return tool
}

func writeFailureCaptureFixtureTool(
	t *testing.T,
	repo string,
	required string,
	stderr string,
	exitCode int,
) string {
	t.Helper()

	tool := filepath.Join(repo, "tool-fixture")

	script := `#!/usr/bin/env sh
case " $* " in
  *"` + required + `"*) ;;
  *) echo "missing required output flags: ` + required + `" >&2; exit 2 ;;
esac
echo "` + stderr + `" >&2
exit ` + strconv.Itoa(exitCode) + `
`

	writeExecutableFixture(t, tool, script)

	return tool
}

func writeSuccessCaptureFixtureTool(
	t *testing.T,
	repo string,
	required string,
) string {
	t.Helper()

	tool := filepath.Join(repo, "tool-fixture")

	script := `#!/usr/bin/env sh
case " $* " in
  *"` + required + `"*) ;;
  *) echo "missing required output flags: ` + required + `" >&2; exit 2 ;;
esac
exit 0
`

	writeExecutableFixture(t, tool, script)

	return tool
}

func runCapturedToolForTest(
	tool string,
	toolPath string,
	repo string,
	args []string,
	policyContext capturePolicyData,
	outputFormat string,
	output io.Writer,
) int {
	return runCapturedToolWithRequest(captureRequest{
		Tool:         tool,
		ToolPath:     toolPath,
		Cwd:          repo,
		TraceRoot:    repo,
		Args:         append([]string(nil), args...),
		EvidenceMaps: policyContext.EvidenceMaps,
		Policies:     policyContext.Policies,
		Skills:       policyContext.Skills,
		Output:       output,
	}, outputFormat)
}

func singleTraceContent(t *testing.T, repo string) string {
	t.Helper()

	matches, err := filepath.Glob(
		filepath.Join(repo, ".coding-ethos", "lint-runs", "*.json"),
	)
	if err != nil {
		t.Fatalf("glob traces: %v", err)
	}

	if len(matches) != 1 {
		t.Fatalf("trace files = %#v", matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}

	return string(content)
}

func mypyJSONFixture() string {
	return strings.Join([]string{
		`{"file":"pkg/app.py"`,
		`"line":3`,
		`"column":4`,
		`"severity":"error"`,
		`"code":"no-any-return"`,
		`"message":"Returning Any"}`,
	}, ",")
}

func pyrightJSONFixture() string {
	return strings.Join([]string{
		`{"generalDiagnostics":[{"file":"pkg/app.py"`,
		`"severity":"error"`,
		`"message":"bad type"`,
		`"rule":"reportAssignmentType"`,
		`"range":{"start":{"line":1`,
		`"character":2}}}]}`,
	}, ",")
}

func pylintJSONFixture() string {
	return strings.Join([]string{
		`[{"path":"pkg/app.py"`,
		`"type":"warning"`,
		`"symbol":"cyclic-import"`,
		`"message":"Cyclic import"`,
		`"line":9`,
		`"column":0}]`,
	}, ",")
}

func golangciJSONFixture() string {
	return strings.Join([]string{
		`{"Issues":[{"FromLinter":"errcheck"`,
		`"Text":"unchecked error"`,
		`"Severity":"error"`,
		`"Pos":{"Filename":"pkg/app.go"`,
		`"Line":8`,
		`"Column":2}}]}`,
	}, ",")
}

func actionlintJSONFixture() string {
	return strings.Join([]string{
		`{"filepath":".github/workflows/ci.yml"`,
		`"line":12`,
		`"column":5`,
		`"kind":"syntax-check"`,
		`"message":"property run is not defined"}`,
	}, ",")
}

func hadolintJSONFixture() string {
	return strings.Join([]string{
		`[{"file":"Dockerfile"`,
		`"line":3`,
		`"column":1`,
		`"level":"warning"`,
		`"code":"DL3008"`,
		`"message":"Pin versions in apt get install."}]`,
	}, ",")
}

func writeExecutableFixture(t *testing.T, path, content string) {
	t.Helper()

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write fixture tool: %v", err)
	}

	err = os.Chmod(path, executableFixtureMode)
	if err != nil {
		t.Fatalf("mark fixture tool executable: %v", err)
	}
}

func captureStdout(t *testing.T, operation func()) string {
	t.Helper()

	oldStdout := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}

	os.Stdout = writer

	output := make(chan string, 1)

	go func() {
		var buffer bytes.Buffer

		_, copyErr := io.Copy(&buffer, reader)
		if copyErr != nil {
			output <- fmt.Sprintf("capture stdout failed: %v", copyErr)

			return
		}

		output <- buffer.String()
	}()

	operation()

	err = writer.Close()
	if err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}

	os.Stdout = oldStdout

	return <-output
}
