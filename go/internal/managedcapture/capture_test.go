// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture //nolint:testpackage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolprotocol"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const executableFixtureMode os.FileMode = 0o700

const (
	highVolumeStderrBytes = 2 * 1024 * 1024
	streamDrainTimeout    = 2 * time.Second
)

func TestPrepareManagedWritablePathsCreatesDeclaredCacheDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := prepareManagedWritablePaths(root, sandbox.Evidence{
		WritePaths: []string{
			".coding-ethos/cache",
			managedRuntimePath("lint-runs/"),
			".ruff_cache",
			"__pycache__",
			"pkg/app.py",
		},
	})
	if err != nil {
		t.Fatalf("prepareManagedWritablePaths() returned error: %v", err)
	}

	for _, path := range []string{
		filepath.Join(root, ".coding-ethos", "cache"),
		filepath.Join(root, ".coding-ethos", "lint-runs"),
		filepath.Join(root, ".ruff_cache"),
		filepath.Join(root, "__pycache__"),
	} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			t.Fatalf("expected writable dir %q, stat=%#v err=%v", path, info, statErr)
		}
	}

	if _, statErr := os.Stat(filepath.Join(root, "pkg", "app.py")); statErr == nil {
		t.Fatal("prepareManagedWritablePaths created formatter target file path")
	}
}

func TestManagedWritableDirRejectsTraversal(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		".coding-ethos/cache/../../outside",
		".coding-ethos/cache/..",
		".ruff_cache/../outside",
	} {
		if managedWritableDir(path) {
			t.Fatalf("managedWritableDir(%q) = true, want false", path)
		}
	}
}

func TestPrepareManagedWritablePathRejectsTraversal(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := prepareManagedWritablePath(root, ".coding-ethos/cache/../../outside")
	if err == nil {
		t.Fatal("prepareManagedWritablePath() returned nil for traversal path")
	}

	outside := filepath.Join(root, "..", "outside")
	if _, statErr := os.Stat(outside); statErr == nil {
		t.Fatalf("prepareManagedWritablePath created outside path %q", outside)
	}
}

const captureOutputHelperEnv = "CAPTURE_TEST_HELPER_OUTPUT"

func TestCapturedOutputHelperProcess(t *testing.T) {
	output, enabled := os.LookupEnv(captureOutputHelperEnv)
	if !enabled {
		return
	}

	var writer io.Writer = os.Stdout
	_, _ = writer.Write([]byte(output))
	os.Exit(0)
}

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

func TestRunCapturedToolCodeIntelIngestsTraceAndIndexesChangedFiles(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	target := filepath.Join(repo, "pkg", "app.py")
	err := os.MkdirAll(filepath.Dir(target), executableFixtureMode)
	if err != nil {
		t.Fatalf("create package dir: %v", err)
	}

	err = os.WriteFile(target, []byte("VALUE = 1\n"), 0o600)
	if err != nil {
		t.Fatalf("write python file: %v", err)
	}

	tool := filepath.Join(repo, "formatter-fixture")
	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
printf 'VALUE = 2\n' > pkg/app.py
exit 0
`)

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:           "ruff-format",
		Parser:         "ruff",
		Category:       toolcatalog.CategoryFormat,
		ToolPath:       tool,
		Cwd:            repo,
		TraceRoot:      repo,
		Args:           []string{"format", "pkg/app.py"},
		DiagnosticKind: toolcatalog.DiagnosticKindFormatterChangedFiles,
		FileExtensions: []string{".py"},
		CodeIntel:      true,
	}, hookoutput.FormatTOON)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	store, err := codeintel.OpenReadOnly(
		context.Background(),
		codeintel.DefaultDBPath(repo),
	)
	if err != nil {
		t.Fatalf("open code-intel store: %v", err)
	}
	defer store.Close()

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("code-intel stats: %v", err)
	}

	if stats.Traces != 1 || stats.Files != 1 {
		t.Fatalf("code-intel stats = %#v, want one trace and one file", stats)
	}
}

func TestCopyProcessOutputDrainsStdoutAndStderrConcurrently(t *testing.T) {
	t.Parallel()

	stdoutReader, stdoutWriter := openPipeForTest(t, "stdout")
	stderrReader, stderrWriter := openPipeForTest(t, "stderr")

	defer stdoutReader.Close()
	defer stderrReader.Close()

	buffers := captureBuffers{}

	copyDone := copyProcessOutput(&buffers, stdoutReader, stderrReader)
	stderrWritten := make(chan error, 1)

	go func() {
		_, err := stderrWriter.Write(bytes.Repeat([]byte("x"), highVolumeStderrBytes))
		stderrWritten <- errors.Join(err, stderrWriter.Close())
	}()

	select {
	case err := <-stderrWritten:
		if err != nil {
			t.Fatalf("write stderr fixture: %v", err)
		}
	case <-time.After(streamDrainTimeout):
		t.Fatal("stderr writer blocked; copyProcessOutput did not drain stderr concurrently")
	}

	_, writeErr := stdoutWriter.WriteString("stdout still drains\n")
	if writeErr != nil {
		t.Fatalf("write stdout fixture: %v", writeErr)
	}

	closeErr := stdoutWriter.Close()
	if closeErr != nil {
		t.Fatalf("close stdout fixture: %v", closeErr)
	}

	select {
	case err := <-copyDone:
		if err != nil {
			t.Fatalf("copy process output: %v", err)
		}
	case <-time.After(streamDrainTimeout):
		t.Fatal("copyProcessOutput did not finish after both streams closed")
	}

	if got := buffers.stdout.String(); got != "stdout still drains\n" {
		t.Fatalf("stdout buffer = %q", got)
	}

	if got := buffers.stderr.Len(); got != highVolumeStderrBytes {
		t.Fatalf("stderr bytes = %d, want %d", got, highVolumeStderrBytes)
	}
}

func openPipeForTest(t *testing.T, name string) (*os.File, *os.File) {
	t.Helper()

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("open %s pipe: %v", name, err)
	}

	return reader, writer
}

func ruffTracePolicyContext() PolicyContext {
	return PolicyContext{
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
		PolicyContext{
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

func TestRunCapturedESLintRendersSARIF(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	tool := filepath.Join(repo, "eslint-fixture")

	writeExecutableFixture(t, tool, `#!/usr/bin/env sh
case " $* " in
  *" --format json "*) ;;
  *) echo "missing json format flag" >&2; exit 2 ;;
esac
cat <<'JSON'
[
  {
    "filePath": "pkg/app.js",
    "messages": [
      {
"ruleId": "no-undef",
"severity": 2,
"message": "'missingName' is not defined.",
"line": 3,
"column": 12,
"endLine": 3,
"endColumn": 23
      }
    ]
  }
]
JSON
exit 1
`)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"eslint",
		tool,
		repo,
		[]string{"pkg/app.js"},
		PolicyContext{
			EvidenceMaps: []diagnostics.EvidenceMap{{
				Source:       "eslint",
				Codes:        []string{"no-undef"},
				PolicyID:     "javascript.static_analysis",
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
		`"ruleId": "javascript.static_analysis"`,
		`"uri": "pkg/app.js"`,
		`"startLine": 3`,
		`"policy_id": "javascript.static_analysis"`,
		`"skill_id": "lint-remediation"`,
		`"source_tool": "eslint"`,
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
	if exitCode != BlockedExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, BlockedExitCode)
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

func ruffWarningPolicyContext() PolicyContext {
	return PolicyContext{
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
			PolicyContext{},
		)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", exitCode)
		}
	})

	for _, want := range []string{
		"tool: ruff",
		"status: WARN",
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

func TestFormatToolPassingOutputDoesNotRenderToonWarning(t *testing.T) {
	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	tool := writeSuccessWithOutputCaptureFixtureTool(
		t,
		repo,
		"fixed pkg/app.py",
	)

	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")
	output := captureStdout(t, func() {
		exitCode := runCapturedToolWithRequest(captureRequest{
			Tool:      "ruff-autofix",
			Parser:    "ruff",
			Category:  toolcatalog.CategoryFormat,
			ToolPath:  tool,
			Cwd:       repo,
			TraceRoot: repo,
			Args:      []string{"check", "pkg/app.py"},
		}, hookoutput.FormatTOON)
		if exitCode != 0 {
			t.Fatalf("exit code = %d, want 0", exitCode)
		}
	})

	if strings.TrimSpace(output) != "" {
		t.Fatalf("formatter success output rendered unexpectedly:\n%s", output)
	}

	trace := singleTraceContent(t, repo)
	for _, want := range []string{
		`"tool": "ruff-autofix"`,
		`"parse_status": "output"`,
		`"stdout": "fixed pkg/app.py\n"`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace missing %q:\n%s", want, trace)
		}
	}
}

func TestRunCapturedToolSuppressesEmptyGolangciLintJSONReport(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	result := capturedToolResult(
		captureRequest{
			Tool:      "golangci-lint",
			Cwd:       repo,
			TraceRoot: repo,
			Args:      []string{"run", "./..."},
		},
		captureExecution{
			Stdout: `{"Issues":[],"Report":{"Linters":[{"Name":"unused"}]}}
0 issues.`,
			ExitCode: 0,
			RunArgs:  []string{"run", "./..."},
		},
	)

	if len(result.Findings) != 0 || result.Capture.ParseStatus != "empty" {
		t.Fatalf(
			"empty golangci-lint report produced findings/status: %#v %#v",
			result.Findings,
			result.Capture,
		)
	}
}

func TestRunCapturedToolSuppressesCleanBanditJSONReport(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	result := capturedToolResult(
		captureRequest{
			Tool:      "bandit",
			Parser:    "bandit",
			Cwd:       repo,
			TraceRoot: repo,
			Args:      []string{".bandit.yml"},
		},
		captureExecution{
			Stdout:   `{"errors":[],"generated_at":"2026-05-17T19:15:39Z","metrics":{"_totals":{"loc":1}},"results":[]}`,
			ExitCode: 0,
			RunArgs:  []string{"bandit", "-q", "-f", "json", "-c", ".bandit.yml"},
		},
	)

	if len(result.Findings) != 0 {
		t.Fatalf("clean Bandit report produced findings: %#v", result.Findings)
	}

	if result.Capture.ParseStatus != "output" {
		t.Fatalf("parse status = %q, want output", result.Capture.ParseStatus)
	}

	if !strings.Contains(result.Capture.OutputExcerpt, `"results":[]`) {
		t.Fatalf("clean Bandit report was not retained as evidence: %#v", result.Capture)
	}
}

func TestRunCapturedToolReportsBanditErrorsAsVisibleOutput(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	result := capturedToolResult(
		captureRequest{
			Tool:      "bandit",
			Parser:    "bandit",
			Cwd:       repo,
			TraceRoot: repo,
			Args:      []string{".bandit.yml"},
		},
		captureExecution{
			Stdout:   `{"errors":["pkg/missing.py: No such file"],"results":[]}`,
			ExitCode: 0,
			RunArgs:  []string{"bandit", "-q", "-f", "json", "-c", ".bandit.yml"},
		},
	)

	if len(result.Findings) == 0 {
		t.Fatalf("Bandit report with errors did not produce visible-output finding")
	}

	if result.Capture.ParseStatus != capturedOutputKey {
		t.Fatalf("parse status = %q, want %s", result.Capture.ParseStatus, capturedOutputKey)
	}
}

func TestRunCapturedToolSuppressesCleanRadonComplexityJSONReport(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	result := capturedToolResult(
		captureRequest{
			Tool:      "python-complexity",
			Parser:    "radon-complexity",
			Cwd:       repo,
			TraceRoot: repo,
			Args:      []string{"coding_ethos/cli.py"},
		},
		captureExecution{
			Stdout:   `{"coding_ethos/cli.py":[{"type":"function","rank":"A","name":"main","complexity":1,"lineno":10}]}`,
			ExitCode: 0,
			RunArgs:  []string{"radon", "cc", "-j", "coding_ethos/cli.py"},
		},
	)

	if len(result.Findings) != 0 {
		t.Fatalf("clean Radon complexity report produced findings: %#v", result.Findings)
	}

	if result.Capture.ParseStatus != "output" {
		t.Fatalf("parse status = %q, want output", result.Capture.ParseStatus)
	}

	if !strings.Contains(result.Capture.OutputExcerpt, `"complexity":1`) {
		t.Fatalf("clean Radon report was not retained as evidence: %#v", result.Capture)
	}
}

func TestRunCapturedToolSuppressesCleanRadonMaintainabilityJSONReport(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	result := capturedToolResult(
		captureRequest{
			Tool:      "python-maintainability",
			Parser:    "radon-maintainability",
			Cwd:       repo,
			TraceRoot: repo,
			Args:      []string{"coding_ethos/cli.py"},
		},
		captureExecution{
			Stdout:   `{"coding_ethos/cli.py":{"mi":67.25,"rank":"A"}}`,
			ExitCode: 0,
			RunArgs:  []string{"radon", "mi", "-j", "coding_ethos/cli.py"},
		},
	)

	if len(result.Findings) != 0 {
		t.Fatalf("clean Radon maintainability report produced findings: %#v", result.Findings)
	}

	if result.Capture.ParseStatus != "output" {
		t.Fatalf("parse status = %q, want output", result.Capture.ParseStatus)
	}

	if !strings.Contains(result.Capture.OutputExcerpt, `"mi":67.25`) {
		t.Fatalf("clean Radon report was not retained as evidence: %#v", result.Capture)
	}
}

func TestCapturedOutputExcerptSuppressesPassingToolSummaries(t *testing.T) {
	t.Parallel()

	for _, output := range []string{
		"0 issues.",
		"18 files left unchanged",
	} {
		t.Run(output, func(t *testing.T) {
			t.Parallel()

			got := capturedOutputExcerpt(output, "", "/repo", "")
			if got != "" {
				t.Fatalf("capturedOutputExcerpt(%q) = %q, want empty", output, got)
			}
		})
	}
}

func TestRunCapturedToolRecordsSandboxDenialInTraceAndSARIF(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	tool := filepath.Join(repo, "bin", "ruff")
	if err := os.MkdirAll(filepath.Dir(tool), 0o700); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}

	var output bytes.Buffer

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:      "ruff",
		ToolPath:  tool,
		Cwd:       repo,
		TraceRoot: repo,
		Args:      []string{"check", "pkg/app.py"},
		Output:    &output,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"."},
			WritePaths:     []string{".coding-ethos/cache"},
		},
	}, hookoutput.FormatSARIF)
	if exitCode != BlockedExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, BlockedExitCode)
	}

	for _, want := range []string{
		`"sandbox": {`,
		`"profile": "lint-offline"`,
		`"denied": true`,
		`"reason": "sandbox wrapper is required: stat `,
		`no such file or directory`,
		`"policies": [`,
		`"runtime.sandbox_denial"`,
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

func TestRunCapturedToolReportsNativeLaunchFailureAsSandboxDenial(
	t *testing.T,
) {
	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	repo := t.TempDir()
	tool := filepath.Join(repo, "bin", "ruff")
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	if err := os.MkdirAll(filepath.Dir(tool), 0o700); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}
	if err := os.WriteFile(tool, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write tool fixture: %v", err)
	}
	if err := os.WriteFile(wrapper, []byte("#!/bin/sh\n"), 0o600); err != nil {
		t.Fatalf("write non-executable wrapper fixture: %v", err)
	}

	var output bytes.Buffer

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:               "ruff",
		ToolPath:           tool,
		Cwd:                repo,
		TraceRoot:          repo,
		Args:               []string{"check", "pkg/app.py"},
		SandboxBackendPath: wrapper,
		Output:             &output,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			ReadPaths:      []string{"."},
			WritePaths:     []string{".coding-ethos/cache"},
		},
	}, hookoutput.FormatSARIF)
	if exitCode != BlockedExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, BlockedExitCode)
	}

	for _, want := range []string{
		`"runtime.sandbox_denial"`,
		`"denied": true`,
		`"reason": "sandbox wrapper is required: `,
		`is not executable`,
	} {
		if !strings.Contains(strings.ToLower(output.String()), strings.ToLower(want)) {
			t.Fatalf("SARIF output missing %q:\n%s", want, output.String())
		}
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"policy_id": "runtime.sandbox_denial"`,
		`"code": "SANDBOX_DENIED"`,
		`"denied": true`,
		`"reason": "sandbox wrapper is required: `,
		`is not executable`,
	} {
		if !strings.Contains(strings.ToLower(content), strings.ToLower(want)) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestCapturedSandboxRuntimeDenialReasonUsesWrapperMarker(t *testing.T) {
	t.Parallel()

	reason, denied := capturedSandboxRuntimeDenialReason(processResult{
		exitCode: capturedSandboxWrapperFailure,
		stderr:   "coding-ethos-sandbox: apply native sandbox filesystem policy: permission denied",
	})
	if !denied {
		t.Fatal("capturedSandboxRuntimeDenialReason() denied = false")
	}
	if !strings.Contains(reason, "coding-ethos-sandbox:") {
		t.Fatalf("reason = %q, want sandbox marker", reason)
	}
}

func TestBuildCapturedSandboxPlanAllowsHookSubprocessesInsideAgentShell(
	t *testing.T,
) {
	repo := t.TempDir()
	tool := filepath.Join(repo, "bin", "ruff")
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")
	runDir := filepath.Join(repo, ".coding-ethos", "cache", "agent-shell", "run-test")
	realGit := filepath.Join(runDir, "real-git")

	if err := os.MkdirAll(filepath.Dir(tool), 0o700); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create agent-shell run dir: %v", err)
	}

	writeExecutableFixture(t, tool, "#!/usr/bin/env sh\nexit 0\n")
	writeExecutableFixture(t, wrapper, "#!/usr/bin/env sh\nexit 0\n")
	writeExecutableFixture(t, realGit, "#!/usr/bin/env sh\nexit 0\n")

	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "1")
	t.Setenv("CODING_ETHOS_SANDBOX_ROOT", repo)
	t.Setenv("CODING_ETHOS_REAL_GIT", realGit)

	plan, cacheEnv, err := buildCapturedSandboxPlan(captureRequest{
		Tool:               "ruff",
		ToolPath:           tool,
		Cwd:                repo,
		TraceRoot:          repo,
		SandboxBackendPath: wrapper,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths:     []string{".coding-ethos/cache"},
		},
	}, []string{"check", "."})
	if err != nil {
		t.Fatalf("build captured sandbox plan: %v", err)
	}

	if !plan.Evidence.RequiresProcesses || plan.Evidence.ProcessIsolated {
		t.Fatalf("agent-shell nested capture must preserve subprocesses: %#v", plan.Evidence)
	}
	if !plan.Evidence.Enabled || !plan.Evidence.RepoReadOnly {
		t.Fatalf("nested capture lost filesystem sandbox evidence: %#v", plan.Evidence)
	}

	cleanupSandboxCacheEnv(cacheEnv)
}

func TestBuildCapturedSandboxPlanPreparesGoTestTempWritePath(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	tool := filepath.Join(repo, "bin", "go")
	wrapper := filepath.Join(repo, "bin", "coding-ethos-sandbox")

	if err := os.MkdirAll(filepath.Dir(tool), 0o700); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}

	writeExecutableFixture(t, tool, "#!/usr/bin/env sh\nexit 0\n")
	writeExecutableFixture(t, wrapper, "#!/usr/bin/env sh\nexit 0\n")

	plan, cacheEnv, err := buildCapturedSandboxPlan(captureRequest{
		Tool:               goTestTool,
		ToolPath:           tool,
		Cwd:                repo,
		TraceRoot:          repo,
		SandboxBackendPath: wrapper,
		Capabilities: sandbox.Capabilities{
			SandboxProfile: "lint-offline",
			WritePaths: []string{
				".coding-ethos/cache",
				goTestSandboxTempDir(repo),
			},
		},
	}, []string{"test", "./..."})
	if err != nil {
		t.Fatalf("build captured sandbox plan: %v", err)
	}
	defer func() {
		err := plan.Close()
		if err != nil {
			t.Fatalf("close sandbox plan: %v", err)
		}
	}()
	defer cleanupSandboxCacheEnv(cacheEnv)

	if cacheEnv.TempDir == "" ||
		!slices.Contains(plan.Evidence.WritePaths, cacheEnv.TempDir) {
		t.Fatalf(
			"go-test temp dir not declared writable: temp=%q evidence=%#v",
			cacheEnv.TempDir,
			plan.Evidence.WritePaths,
		)
	}

	info, statErr := os.Stat(cacheEnv.TempDir)
	if statErr != nil || !info.IsDir() {
		t.Fatalf("go-test temp dir was not prepared: stat=%#v err=%v", info, statErr)
	}
}

func TestCapturedSandboxRuntimeDenialReasonIgnoresToolExit126(t *testing.T) {
	t.Parallel()

	_, denied := capturedSandboxRuntimeDenialReason(processResult{
		exitCode: capturedSandboxWrapperFailure,
		stderr:   "tool-specific exit 126",
	})
	if denied {
		t.Fatal("capturedSandboxRuntimeDenialReason() denied = true")
	}
}

func TestCaptureSandboxWrapperPathUsesPlatformBinaryName(t *testing.T) {
	t.Parallel()

	path := captureSandboxWrapperPath()
	if path == "" {
		t.Fatal("captureSandboxWrapperPath() = empty")
	}

	if runtime.GOOS == windowsGOOS {
		if filepath.Base(path) != "coding-ethos-sandbox.exe" {
			t.Fatalf("wrapper path = %q, want .exe helper", path)
		}

		return
	}

	if filepath.Base(path) != "coding-ethos-sandbox" {
		t.Fatalf("wrapper path = %q, want unix helper", path)
	}
}

func TestRunCapturedToolBlocksParsedErrorWhenToolExitsZero(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	tool := writeSuccessWithOutputCaptureFixtureTool(
		t,
		repo,
		`{"Action":"run","Package":"pkg","Test":"TestFail"}`+"\n"+
			`{"Action":"output","Package":"pkg","Test":"TestFail",`+
			`"Output":"    pkg/app_test.go:12: failed\n"}`+"\n"+
			`{"Action":"fail","Package":"pkg","Test":"TestFail"}`,
	)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"go-test",
		tool,
		repo,
		nil,
		PolicyContext{},
		hookoutput.FormatTOON,
		&output,
	)
	if exitCode != BlockedExitCode {
		t.Fatalf(
			"exit code = %d, want %d; output:\n%s",
			exitCode,
			BlockedExitCode,
			output.String(),
		)
	}

	if !strings.Contains(output.String(), "status: FAIL") ||
		!strings.Contains(output.String(), "failed") {
		t.Fatalf("missing parsed go-test diagnostic:\n%s", output.String())
	}
}

func TestRunCapturedGoVetFailureFormatsParsedDiagnostics(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	for _, format := range []string{
		hookoutput.FormatTOON,
		hookoutput.FormatJSON,
		hookoutput.FormatSARIF,
	} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			output, trace := runCapturedGoVetFailureForTest(t, format)
			assertGoVetParsedDiagnosticOutput(t, format, output)
			assertGoVetParsedDiagnosticTrace(t, trace)
		})
	}
}

func runCapturedGoVetFailureForTest(t *testing.T, format string) (string, string) {
	t.Helper()

	const vetOutput = "# blackcat.ca/coding-ethos/go/pkg\n" +
		"pkg/app.go:12:4: fmt.Println call has possible Printf formatting directive %s"

	repo := t.TempDir()
	tool := writeCaptureFixtureTool(t, repo, "vet", vetOutput)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"go-vet",
		tool,
		repo,
		[]string{"vet", "./..."},
		PolicyContext{},
		format,
		&output,
	)
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1; output:\n%s", exitCode, output.String())
	}

	return output.String(), singleTraceContent(t, repo)
}

func assertGoVetParsedDiagnosticOutput(
	t *testing.T,
	format string,
	content string,
) {
	t.Helper()

	for _, want := range goVetParsedDiagnosticOutput(format) {
		if !strings.Contains(content, want) {
			t.Fatalf("%s output missing %q:\n%s", format, want, content)
		}
	}

	for _, unwanted := range []string{
		"without parseable diagnostics",
		"external command failed",
	} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("%s output used generic failure %q:\n%s", format, unwanted, content)
		}
	}
}

func assertGoVetParsedDiagnosticTrace(t *testing.T, trace string) {
	t.Helper()

	for _, want := range []string{
		`"tool": "go-vet"`,
		`"parse_status": "parsed"`,
		`"output_excerpt": "# blackcat.ca/coding-ethos/go/pkg pkg/app.go:12:4`,
	} {
		if !strings.Contains(trace, want) {
			t.Fatalf("trace missing %q:\n%s", want, trace)
		}
	}
}

func goVetParsedDiagnosticOutput(format string) []string {
	switch format {
	case hookoutput.FormatTOON:
		return []string{
			"tool: go-vet",
			"findings[1]",
			"go-vet,pkg/app.go,12,4,error,vet",
			"fmt.Println call has possible Printf formatting directive %s",
		}
	case hookoutput.FormatJSON:
		return []string{
			`"scope": "tool:go-vet"`,
			`"status": "blocked"`,
			`"parse_status": "parsed"`,
			`"diagnostics": [`,
			`"tool": "go-vet"`,
			`"file": "pkg/app.go"`,
			`"line": 12`,
			`"column": 4`,
			`"code": "vet"`,
		}
	case hookoutput.FormatSARIF:
		return []string{
			`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
			`"ruleId": "go-vet:vet"`,
			`"uri": "pkg/app.go"`,
			`"startLine": 12`,
			`"startColumn": 4`,
			`"source_tool": "go-vet"`,
		}
	default:
		return nil
	}
}

func TestRunCapturedGoTestCoverageCanBePromotedByCEL(t *testing.T) {
	repo := t.TempDir()
	tool, args := capturedOutputHelperTool(
		t,
		`{"Action":"output","Package":"blackcat.ca/coding-ethos/go/pkg",`+
			`"Output":"pkg/app.go:12: App 74.2%\n"}`+"\n",
	)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"go-test",
		tool,
		repo,
		args,
		PolicyContext{
			Policies: []policy.Policy{{
				ID:              "testing.coverage_floor",
				Category:        "expression",
				DefaultSeverity: "block",
				Message:         "Go test package coverage is below the required floor.",
				Suggestion:      "Add meaningful tests for the under-covered package.",
				AppliesTo:       policy.AppliesTo{Tools: []string{"go-test"}},
				DefenseLayers:   policy.CodeDefenseLayers(),
				SupportedModes:  []string{"block", "record", "advise"},
				PrincipleIDs:    []string{"testing-as-specification"},
				Evaluators: []policy.Evaluator{{
					Kind: "cel",
					Name: "cel.expression",
					Options: map[string]any{
						"when": `coverage.exists(item,
							item.tool == "go-test" &&
							item.file == "pkg/app.go" &&
							item.percent < 80.0
						)`,
					},
				}},
			}},
		},
		hookoutput.FormatSARIF,
		&output,
	)
	if exitCode != BlockedExitCode {
		t.Fatalf(
			"exit code = %d, want %d; output:\n%s",
			exitCode,
			BlockedExitCode,
			output.String(),
		)
	}

	for _, want := range []string{
		`"ruleId": "testing.coverage_floor"`,
		`"level": "error"`,
		`"uri": "pkg/app.go"`,
		`"code": "coverage-file"`,
		`"cel_expression": "coverage.exists`,
		`"executionSuccessful": false`,
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("SARIF output missing %q:\n%s", want, output.String())
		}
	}
}

func TestRunCapturedGoTestCoverageIsTraceEvidenceWithoutTOONNoise(t *testing.T) {
	repo := t.TempDir()
	tool, args := capturedOutputHelperTool(
		t,
		`{"Action":"output","Package":"blackcat.ca/coding-ethos/go/pkg",`+
			`"Output":"coverage: 82.4% of statements\n"}`+"\n",
	)

	var output bytes.Buffer

	exitCode := runCapturedToolForTest(
		"go-test",
		tool,
		repo,
		args,
		PolicyContext{},
		hookoutput.FormatTOON,
		&output,
	)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}

	if output.String() != "" {
		t.Fatalf("record-only coverage should not render TOON output:\n%s", output.String())
	}

	content := singleTraceContent(t, repo)
	for _, want := range []string{
		`"tool": "go-test"`,
		`"code": "coverage-package"`,
		`"coverage_percent": 82.4`,
		`"package": "blackcat.ca/coding-ethos/go/pkg"`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}

func TestCapturedToolUsesCatalogParserAlias(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	tool := writeSuccessWithOutputCaptureFixtureTool(
		t,
		repo,
		`[{"filename":"pkg/app.py","location":{"row":4,"column":5},`+
			`"code":"F401","message":"unused import"}]`,
	)

	var output bytes.Buffer

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:      "ruff-autofix",
		Parser:    "ruff",
		ToolPath:  tool,
		Cwd:       repo,
		TraceRoot: repo,
		Output:    &output,
	}, hookoutput.FormatTOON)
	if exitCode != BlockedExitCode {
		t.Fatalf("exit code = %d, want %d", exitCode, BlockedExitCode)
	}

	for _, want := range []string{
		"F401",
		"pkg/app.py",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("captured output missing %q:\n%s", want, output.String())
		}
	}

	content := singleTraceContent(t, repo)
	if !strings.Contains(content, `"parser": "ruff"`) {
		t.Fatalf("trace did not record parser alias:\n%s", content)
	}
}

func TestCapturedOutputExcerptPreservesBothStreams(t *testing.T) {
	t.Parallel()

	excerpt := capturedOutputExcerpt(
		"stdout diagnostic",
		"stderr diagnostic",
		"/repo",
		"/repo",
	)

	if !strings.Contains(excerpt, "stdout diagnostic") ||
		!strings.Contains(excerpt, "stderr diagnostic") {
		t.Fatalf("excerpt dropped a stream: %q", excerpt)
	}
}

func TestFormatterChangedFilesBecomeDiagnostics(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()

	source := filepath.Join(repo, "pkg", "app.py")

	err := os.MkdirAll(filepath.Dir(source), 0o755)
	if err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	err = os.WriteFile(source, []byte("print(1)\n"), 0o600)
	if err != nil {
		t.Fatalf("write fixture source: %v", err)
	}

	tool := filepath.Join(repo, "formatter-fixture")
	writeExecutableFixture(t, tool, `#!/bin/sh
printf 'print(2)\n' > "$1"
exit 0
`)

	var output bytes.Buffer

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:           "pyupgrade",
		Parser:         "fallback",
		Category:       toolcatalog.CategoryFormat,
		DiagnosticKind: toolcatalog.DiagnosticKindFormatterChangedFiles,
		ToolPath:       tool,
		Cwd:            repo,
		TraceRoot:      repo,
		Args:           []string{"pkg/app.py"},
		Output:         &output,
	}, hookoutput.FormatTOON)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0:\n%s", exitCode, output.String())
	}

	if strings.TrimSpace(output.String()) != "" {
		t.Fatalf("formatter changed-file output rendered unexpectedly:\n%s", output.String())
	}

	content := singleTraceContent(t, repo)
	if !strings.Contains(content, `"parse_status": "changed_files"`) {
		t.Fatalf("trace did not record changed-file parse status:\n%s", content)
	}

	if !strings.Contains(content, `"category": "format"`) ||
		!strings.Contains(content, `"formatted"`) ||
		!strings.Contains(content, `"args":`) ||
		!strings.Contains(content, `"pkg/app.py"`) {
		t.Fatalf("trace did not record formatter arguments:\n%s", content)
	}
}

func TestFormatterChangedFilesRenderStructuredDiagnosticsForMachineFormats(
	t *testing.T,
) {
	t.Parallel()

	if runtime.GOOS == windowsGOOS {
		t.Skip("shell fixture uses POSIX sh")
	}

	for _, format := range []string{hookoutput.FormatJSON, hookoutput.FormatSARIF} {
		t.Run(format, func(t *testing.T) {
			t.Parallel()

			output := runFormatterChangedFileForTest(t, format)
			for _, want := range formatterChangedFileOutput(format) {
				if !strings.Contains(output, want) {
					t.Fatalf("%s output missing %q:\n%s", format, want, output)
				}
			}

			for _, unwanted := range []string{
				"external command failed",
				"without parseable diagnostics",
			} {
				if strings.Contains(output, unwanted) {
					t.Fatalf("%s output used generic failure %q:\n%s", format, unwanted, output)
				}
			}
		})
	}
}

func runFormatterChangedFileForTest(t *testing.T, format string) string {
	t.Helper()

	repo := t.TempDir()
	source := filepath.Join(repo, "pkg", "app.py")

	err := os.MkdirAll(filepath.Dir(source), 0o755)
	if err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	err = os.WriteFile(source, []byte("print(1)\n"), 0o600)
	if err != nil {
		t.Fatalf("write fixture source: %v", err)
	}

	tool := filepath.Join(repo, "formatter-fixture")
	writeExecutableFixture(t, tool, `#!/bin/sh
printf 'print(2)\n' > "$1"
exit 0
`)

	var output bytes.Buffer

	exitCode := runCapturedToolWithRequest(captureRequest{
		Tool:           "pyupgrade",
		Parser:         "fallback",
		Category:       toolcatalog.CategoryFormat,
		DiagnosticKind: toolcatalog.DiagnosticKindFormatterChangedFiles,
		ToolPath:       tool,
		Cwd:            repo,
		TraceRoot:      repo,
		Args:           []string{"pkg/app.py"},
		Output:         &output,
	}, format)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0:\n%s", exitCode, output.String())
	}

	return output.String()
}

func formatterChangedFileOutput(format string) []string {
	switch format {
	case hookoutput.FormatJSON:
		return []string{
			`"scope": "tool:pyupgrade"`,
			`"status": "resolved"`,
			`"diagnostics": [`,
			`"tool": "pyupgrade"`,
			`"file": "pkg/app.py"`,
			`"code": "formatted"`,
			`"category": "formatter_changed_file"`,
		}
	case hookoutput.FormatSARIF:
		return []string{
			`"$schema": "https://json.schemastore.org/sarif-2.1.0.json"`,
			`"ruleId": "pyupgrade:formatted"`,
			`"uri": "pkg/app.py"`,
			`"source_tool": "pyupgrade"`,
		}
	default:
		return nil
	}
}

func TestPrepareSandboxCgroupSkipsWhenSandboxDisabled(t *testing.T) {
	t.Parallel()

	cgroup, evidence, err := prepareSandboxCgroup(sandbox.Evidence{
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

func TestAppendEvidenceReasonPreservesExistingReason(t *testing.T) {
	t.Parallel()

	got := appendEvidenceReason(
		"delegated cgroup directory could not be opened",
		"prepare sandbox cgroup limits: delegated cgroup directory could not be opened",
	)
	want := "delegated cgroup directory could not be opened; " +
		"prepare sandbox cgroup limits: delegated cgroup directory could not be opened"
	if got != want {
		t.Fatalf("appendEvidenceReason() = %q", got)
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
			PolicyContext{},
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
		PolicyContext{},
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
			PolicyContext{
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
		PolicyContext{},
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

func TestCapturedFindingsClassifyFormatterPermissionFailures(t *testing.T) {
	t.Parallel()

	findings := capturedFindings(
		captureRequest{Tool: "ruff", Args: []string{"format", "scripts/phase2_setup.py"}},
		captureExecution{
			RunArgs:  []string{"format", "scripts/phase2_setup.py"},
			ExitCode: capturedConfigurationExitCode,
		},
		"error: Failed to write\nscripts/phase2_setup.py: Permission denied (os error 13)",
		nil,
	)
	if len(findings) != 1 {
		t.Fatalf("findings = %#v", findings)
	}

	if findings[0].RawOutcome["category"] != "permission_error" {
		t.Fatalf("raw outcome = %#v", findings[0].RawOutcome)
	}

	if !strings.Contains(findings[0].Message, "target path is not writable") {
		t.Fatalf("message = %q", findings[0].Message)
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
				PolicyContext{},
				"",
				io.Discard,
			)
			content := singleTraceContent(t, repo)
			if exitCode != 1 {
				t.Fatalf("exit code = %d, want 1; trace:\n%s", exitCode, content)
			}

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
					PolicyContext{},
				)
				if exitCode != 2 {
					t.Fatalf("exit code = %d, want 2", exitCode)
				}
			})

			for _, want := range []string{
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
				test.tool + ",,0,0,fatal,,tool." + test.tool,
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
			PolicyContext{},
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

func TestCapturedOutputExcerptSuppressesPassingGoTestJSON(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		`{"Action":"run","Package":"pkg","Test":"TestOK"}`,
		`{"Action":"pass","Package":"pkg","Test":"TestOK","Elapsed":0.01}`,
		`{"Action":"pass","Package":"pkg","Elapsed":0.02}`,
	}, "\n")

	if got := capturedOutputExcerpt(output, "", "", ""); got != "" {
		t.Fatalf("passing go test JSON excerpt = %q, want empty", got)
	}
}

func TestCapturedOutputExcerptKeepsFailingGoTestJSON(t *testing.T) {
	t.Parallel()

	output := strings.Join([]string{
		`{"Action":"run","Package":"pkg","Test":"TestFail"}`,
		`{"Action":"output","Package":"pkg","Test":"TestFail",` +
			`"Output":"    pkg/app_test.go:12: failed\n"}`,
		`{"Action":"fail","Package":"pkg","Test":"TestFail","Elapsed":0.01}`,
	}, "\n")

	if got := capturedOutputExcerpt(output, "", "", ""); got == "" {
		t.Fatal("failing go test JSON excerpt should be retained")
	}
}

func TestCapturedProcessEnvRemovesCodingEthosGitShimPath(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	shimDir := filepath.Join(root, "shim")
	realDir := filepath.Join(root, "real")

	err := os.MkdirAll(shimDir, 0o755)
	if err != nil {
		t.Fatalf("create shim dir: %v", err)
	}

	err = os.MkdirAll(realDir, 0o755)
	if err != nil {
		t.Fatalf("create real dir: %v", err)
	}

	shim := strings.Join([]string{
		"#!/usr/bin/env bash",
		"exec /repo/bin/coding-ethos-run policy-git \"$@\"",
		"",
	}, "\n")

	shimPath := filepath.Join(shimDir, "git")

	err = os.WriteFile(shimPath, []byte(shim), 0o600)
	if err != nil {
		t.Fatalf("write git shim: %v", err)
	}

	err = os.Chmod(shimPath, 0o700)
	if err != nil {
		t.Fatalf("chmod git shim: %v", err)
	}

	env := capturedProcessEnv([]string{
		"PATH=" + strings.Join(
			[]string{shimDir, realDir},
			string(os.PathListSeparator),
		),
		"CODE_ETHOS_CONSUMER_ROOT=/repo",
		"CODING_ETHOS_EXEC_STACK=coding-ethos-run",
		"CODING_ETHOS_SANDBOX_ACTIVE=1",
		"CODING_ETHOS_REAL_GIT=/usr/bin/git",
		"GIT_DIR=/repo/.git",
		"GIT_INDEX_FILE=/repo/.git/index",
		"MANAGED_TOOLCHAIN_MANIFEST=/repo/build/toolchain/manifest.tsv",
		"OTHER=value",
		"TMPDIR=/tmp/host",
		"GOCACHE=/tmp/go-cache",
		"GOPATH=/tmp/go-path",
		"GOMODCACHE=/tmp/go-mod-cache",
		"GOLANGCI_LINT_CACHE=/tmp/golangci-cache",
		"GOROOT=/tmp/go-root",
		"CGO_ENABLED=0",
		"CC=/tmp/host-cc",
		"COMPILER_PATH=/tmp/host-compiler-path",
		"AS=/tmp/host-as",
	}, sandboxCacheEnvironment{
		TempDir:         "/repo/.coding-ethos/cache/sandbox-tmp",
		GoCache:         "/repo/.coding-ethos/cache/go-build",
		GoPath:          "/repo/.coding-ethos/cache/go-path",
		GoModCache:      "/repo/.coding-ethos/cache/go-path/pkg/mod",
		GolangCILintDir: "/repo/.coding-ethos/cache/golangci-lint",
		GoRoot:          "/repo/go-root",
		CGOEnabled:      "1",
		CC:              "/usr/bin/gcc",
		CompilerPath:    "/usr/bin",
		Assembler:       "/usr/bin/as",
	}, "ruff")

	if !capturedEnvPathContains(env, realDir) {
		t.Fatalf("captured env PATH = %#v, want entry %q", env, realDir)
	}

	if slices.Contains(env, "PATH="+shimDir) {
		t.Fatalf("captured env kept git shim dir: %#v", env)
	}

	if !slices.Contains(env, "TMPDIR=/repo/.coding-ethos/cache/sandbox-tmp") ||
		slices.Contains(env, "TMPDIR=/tmp/host") {
		t.Fatalf("captured env did not replace TMPDIR: %#v", env)
	}

	for _, want := range []string{
		"GOCACHE=/repo/.coding-ethos/cache/go-build",
		"GOPATH=/repo/.coding-ethos/cache/go-path",
		"GOMODCACHE=/repo/.coding-ethos/cache/go-path/pkg/mod",
		"GOLANGCI_LINT_CACHE=/repo/.coding-ethos/cache/golangci-lint",
		"GOROOT=/repo/go-root",
		"CGO_ENABLED=1",
		"CC=/usr/bin/gcc",
		"COMPILER_PATH=/usr/bin",
		"AS=/usr/bin/as",
		"CODING_ETHOS_REAL_GIT=/usr/bin/git",
	} {
		if !slices.Contains(env, want) {
			t.Fatalf("captured env missing cache override %q: %#v", want, env)
		}
	}

	for _, inherited := range []string{
		"GOPATH=/tmp/go-path",
		"GOMODCACHE=/tmp/go-mod-cache",
	} {
		if slices.Contains(env, inherited) {
			t.Fatalf("captured env kept host Go path %q: %#v", inherited, env)
		}
	}

	for _, blocked := range []string{
		"CODE_ETHOS_CONSUMER_ROOT=/repo",
		"CODING_ETHOS_EXEC_STACK=coding-ethos-run",
		"CODING_ETHOS_SANDBOX_ACTIVE=1",
		"CODING_ETHOS_AGENT_SHELL_SANDBOX=1",
		"GIT_DIR=/repo/.git",
		"GIT_INDEX_FILE=/repo/.git/index",
		"MANAGED_TOOLCHAIN_MANIFEST=/repo/build/toolchain/manifest.tsv",
	} {
		if slices.Contains(env, blocked) {
			t.Fatalf("captured env kept internal runtime variable %q: %#v", blocked, env)
		}
	}
}

func capturedEnvPathContains(env []string, entry string) bool {
	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if !ok || name != "PATH" {
			continue
		}

		return slices.Contains(filepath.SplitList(value), entry)
	}

	return false
}

func TestCapturedProcessEnvMarksOnlyManagedActionlint(t *testing.T) {
	t.Parallel()

	inherited := []string{
		toolprotocol.ActionlintShellcheckEnv + "=untrusted",
		"OTHER=value",
	}
	want := toolprotocol.ActionlintShellcheckEnvironment()

	actionlintEnv := capturedProcessEnv(
		inherited,
		sandboxCacheEnvironment{},
		toolprotocol.ActionlintTool,
	)
	if count := countEnvironmentEntry(actionlintEnv, want); count != 1 {
		t.Fatalf(
			"managed actionlint protocol marker count = %d, want 1: %#v",
			count,
			actionlintEnv,
		)
	}

	shellcheckEnv := capturedProcessEnv(
		inherited,
		sandboxCacheEnvironment{},
		toolprotocol.ShellcheckTool,
	)
	for _, item := range shellcheckEnv {
		if strings.HasPrefix(item, toolprotocol.ActionlintShellcheckEnv+"=") {
			t.Fatalf(
				"ordinary ShellCheck inherited actionlint protocol marker: %#v",
				shellcheckEnv,
			)
		}
	}
}

func countEnvironmentEntry(env []string, want string) int {
	count := 0
	for _, item := range env {
		if item == want {
			count++
		}
	}

	return count
}

func TestCapturedProcessEnvAddsUsablePathWhenInheritedPathMissing(t *testing.T) {
	t.Parallel()

	env := capturedProcessEnv([]string{"OTHER=value"}, sandboxCacheEnvironment{}, "ruff")

	for _, item := range env {
		name, value, ok := strings.Cut(item, "=")
		if !ok || name != "PATH" {
			continue
		}

		pathEntries := filepath.SplitList(value)
		if !slices.Contains(pathEntries, "/usr/bin") &&
			!slices.Contains(pathEntries, "/bin") {
			t.Fatalf("captured env PATH lacks system executable dirs: %#v", env)
		}

		return
	}

	t.Fatalf("captured env omitted PATH: %#v", env)
}

func TestManagedSubprocessPathPrefixRejectsShimRealGitEnv(t *testing.T) {
	root := t.TempDir()
	shimDir := filepath.Join(root, "shim")
	err := os.MkdirAll(shimDir, 0o755)
	if err != nil {
		t.Fatalf("create shim dir: %v", err)
	}

	shim := filepath.Join(shimDir, "git")
	writeExecutableFixture(
		t,
		shim,
		"#!/usr/bin/env sh\nexec coding-ethos-run policy-git \"$@\"\n",
	)
	writeExecutableFixture(
		t,
		filepath.Join(shimDir, "coding-ethos-run"),
		"#!/usr/bin/env sh\nexit 0\n",
	)
	t.Setenv("CODING_ETHOS_REAL_GIT", shim)

	realGit, err := resolvedManagedSubprocessGit(context.Background())
	if err != nil {
		t.Fatalf("resolvedManagedSubprocessGit() returned error: %v", err)
	}

	prefix, err := managedSubprocessPathPrefix(filepath.Join(root, "tmp"), realGit)
	if err != nil {
		t.Fatalf("managedSubprocessPathPrefix() returned error: %v", err)
	}

	target, err := os.Readlink(filepath.Join(prefix, "git"))
	if err != nil {
		t.Fatalf("read managed git link: %v", err)
	}

	if target == shim {
		t.Fatalf("managed subprocess git used coding-ethos shim: %s", target)
	}

	if realGit == shim {
		t.Fatalf("managed subprocess real git env used coding-ethos shim: %s", realGit)
	}
}

func TestManagedSubprocessPathPrefixIsDeterministic(t *testing.T) {
	root := t.TempDir()
	realGit := filepath.Join(root, "real-git")
	writeExecutableFixture(t, realGit, "#!/usr/bin/env sh\nexit 0\n")
	tempDir := filepath.Join(root, "tmp")

	first, err := managedSubprocessPathPrefix(tempDir, realGit)
	if err != nil {
		t.Fatalf("first managedSubprocessPathPrefix() returned error: %v", err)
	}
	second, err := managedSubprocessPathPrefix(tempDir, realGit)
	if err != nil {
		t.Fatalf("second managedSubprocessPathPrefix() returned error: %v", err)
	}

	if first != second {
		t.Fatalf("managed subprocess path changed: first=%q second=%q", first, second)
	}
}

func TestManagedSubprocessPathPrefixReplacesStaleGitFile(t *testing.T) {
	root := t.TempDir()
	realGit := filepath.Join(root, "real-git")
	writeExecutableFixture(t, realGit, "#!/usr/bin/env sh\nexit 0\n")
	tempDir := filepath.Join(root, "tmp")

	prefix, err := managedSubprocessPathPrefix(tempDir, realGit)
	if err != nil {
		t.Fatalf("managedSubprocessPathPrefix() returned error: %v", err)
	}

	gitPath := filepath.Join(prefix, "git")
	if err := os.Remove(gitPath); err != nil {
		t.Fatalf("remove managed git link: %v", err)
	}
	writeExecutableFixture(t, gitPath, "#!/usr/bin/env sh\nexit 1\n")

	prefix, err = managedSubprocessPathPrefix(tempDir, realGit)
	if err != nil {
		t.Fatalf("managedSubprocessPathPrefix() with stale file returned error: %v", err)
	}

	target, err := os.Readlink(filepath.Join(prefix, "git"))
	if err != nil {
		t.Fatalf("read managed git link: %v", err)
	}
	if target != realGit {
		t.Fatalf("managed git link = %q, want %q", target, realGit)
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
				"--allow-parallel-runners",
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

func TestCapturedESLintArgsForceMachineReadableOutput(t *testing.T) {
	t.Parallel()

	args := []string{"web/app.js"}
	want := []string{"--format", "json", "web/app.js"}

	got := capturedToolArgs("eslint", args)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("capturedToolArgs() = %#v, want %#v", got, want)
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

	script := `#!/bin/sh
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

	script := `#!/bin/sh
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

	script := `#!/bin/sh
case " $* " in
  *"` + required + `"*) ;;
  *) echo "missing required output flags: ` + required + `" >&2; exit 2 ;;
esac
exit 0
`

	writeExecutableFixture(t, tool, script)

	return tool
}

func writeSuccessWithOutputCaptureFixtureTool(
	t *testing.T,
	repo string,
	output string,
) string {
	t.Helper()

	tool := filepath.Join(repo, "tool-fixture")

	script := `#!/bin/sh
cat <<'EOF'
` + output + `
EOF
exit 0
`

	writeExecutableFixture(t, tool, script)

	return tool
}

func capturedOutputHelperTool(t *testing.T, output string) (string, []string) {
	t.Helper()

	tool, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve test helper executable: %v", err)
	}

	t.Setenv(captureOutputHelperEnv, output)

	return tool, []string{"-test.run=^TestCapturedOutputHelperProcess$"}
}

func runCapturedToolForTest(
	tool string,
	toolPath string,
	repo string,
	args []string,
	policyContext PolicyContext,
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

	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL,
		executableFixtureMode,
	)
	if err != nil {
		t.Fatalf("create fixture tool: %v", err)
	}

	_, err = file.WriteString(content)
	if err != nil {
		_ = file.Close()
		t.Fatalf("write fixture tool: %v", err)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("close fixture tool: %v", err)
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
