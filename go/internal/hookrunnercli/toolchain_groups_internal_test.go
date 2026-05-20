// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:lll // Uses process-global fixtures.
package hookrunnercli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const (
	coverageFloorPolicyID = "testing.go_coverage_floor"
	coverageGoalPolicyID  = "testing.go_coverage_goal"
	coverageWarnDecision  = "warn"
)

func TestParseGofmtCheckFindings(t *testing.T) {
	t.Parallel()

	findings := parseGofmtCheckFindings("pkg/app.go\ncmd/main.go\n")
	if len(findings) != 2 {
		t.Fatalf("parseGofmtCheckFindings() = %#v, want two findings", findings)
	}

	got := findings[0]
	if got.Tool != "gofmt-check" ||
		got.File != toolCatalogGoFile ||
		got.Severity != testSeverityError ||
		got.Message != "Go file is not gofmt-formatted." {
		t.Fatalf("unexpected finding: %#v", got)
	}
}

func TestKubeLinterFiltersNonKubernetesYAML(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "deploy/pod.yaml"),
		"apiVersion: v1\nkind: Pod\nmetadata:\n  name: app\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, ".github/workflows/ci.yml"),
		"name: CI\non: [push]\njobs:\n  test:\n    runs-on: ubuntu-latest\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "config.yaml"),
		"tooling:\n  enabled: true\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(tempDir, "deploy/multi.yaml"),
		"tooling:\n  enabled: true\n---\napiVersion: apps/v1\nkind: Deployment\n",
	)

	pod := filepath.Join(tempDir, "deploy/pod.yaml")
	multi := filepath.Join(tempDir, "deploy/multi.yaml")
	got := kubernetesManifestFiles([]string{
		filepath.Join(tempDir, ".github/workflows/ci.yml"),
		filepath.Join(tempDir, "config.yaml"),
		pod,
		multi,
	})
	want := []string{pod, multi}

	if !slices.Equal(got, want) {
		t.Fatalf("kubernetesManifestFiles() = %#v, want %#v", got, want)
	}
}

func TestParsePythonQualityFindings(t *testing.T) {
	t.Parallel()

	assertComplexityFinding(t)
	assertMaintainabilityFinding(t)
	assertMaintainabilityTimeoutFinding(t)
	assertVultureFinding(t)
}

func TestPythonQualityCommandsRunExternalToolsAndReportFindings(t *testing.T) {
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	mustWriteTestFile(t, "pkg/app.py", "def helper():\n    return 1\n")

	fakeBin := writePythonQualityToolFixtures(t, tempDir)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	var (
		complexityExit      int
		maintainabilityExit int
		vultureExit         int
	)

	stdout := captureStdout(t, func() {
		complexityExit = runPythonComplexity(Config{}, []string{"pkg/app.py"})
		maintainabilityExit = runPythonMaintainability(Config{}, []string{"pkg/app.py"})
		vultureExit = runPythonVulture(Config{}, []string{"pkg/app.py"})
	})

	if !nativeSandboxAvailable {
		if complexityExit == 0 || !strings.Contains(stdout, "runtime.sandbox_denial") {
			t.Fatalf("nested sandbox output missing denial:\n%s", stdout)
		}

		return
	}

	assertPythonQualityExits(t, complexityExit, maintainabilityExit, vultureExit, stdout)
	assertPythonQualityOutput(t, stdout)
}

func writePythonQualityToolFixtures(t *testing.T, tempDir string) string {
	t.Helper()

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", ".venv", "bin", "radon"),
		`#!/usr/bin/env sh
case "$*" in
  *"cc -j -e"*)
    printf '{"pkg/app.py":[{"type":"function","rank":"C","lineno":3,"name":"complex","complexity":19}]}'
    exit 0
    ;;
  *"mi . -j -e"*)
    printf '{"pkg/app.py":{"mi":42.5,"rank":"C"}}'
    exit 0
    ;;
esac
printf 'unexpected radon invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", ".venv", "bin", "vulture"),
		`#!/usr/bin/env sh
printf "pkg/app.py:17: unused function 'helper' (90%% confidence)\n"
exit 1
`,
	)
	mustWriteExecutable(t, filepath.Join(fakeBin, "uv"), `#!/usr/bin/env sh
printf 'unexpected uv invocation: %s\n' "$*" >&2
exit 2
`)

	return fakeBin
}

func assertPythonQualityExits(
	t *testing.T,
	complexityExit int,
	maintainabilityExit int,
	vultureExit int,
	stdout string,
) {
	t.Helper()

	if complexityExit != managedcapture.BlockedExitCode {
		t.Fatalf(
			"runPythonComplexity() = %d, want %d:\n%s",
			complexityExit,
			managedcapture.BlockedExitCode,
			stdout,
		)
	}

	if maintainabilityExit != 0 {
		t.Fatalf("runPythonMaintainability() = %d, want 0:\n%s", maintainabilityExit, stdout)
	}

	if vultureExit != 1 {
		t.Fatalf("runPythonVulture() = %d, want 1:\n%s", vultureExit, stdout)
	}
}

func assertPythonQualityOutput(t *testing.T, stdout string) {
	t.Helper()

	for _, want := range []string{
		"tool: python-complexity",
		"complex",
		"tool: python-vulture",
		"unused function",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("quality output missing %q:\n%s", want, stdout)
		}
	}
}

func TestGoToolchainCommandsRunConfiguredWorktree(t *testing.T) {
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, "go:\n  worktree: go\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
	managedLintConfig := filepath.Join(tempDir, ".golangci.yml")
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin", "golangci-lint"),
		`#!/usr/bin/env sh
case " $* " in
  *" run --allow-parallel-runners --output.json.path=stdout --output.text.path=stderr --config `+managedLintConfig+` ./... "*)
    exit 0
    ;;
esac
printf 'unexpected golangci-lint invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "go"),
		`#!/usr/bin/env sh
case "$1 $2" in
  "vet ./..."|"test -json")
    exit 0
    ;;
esac
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "gofmt"),
		"#!/usr/bin/env sh\nprintf 'go/main.go\\n'\nexit 0\n",
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)

	got := runGoVet(Config{}, []string{"go/main.go"})
	if !nativeSandboxAvailable && got != 0 {
		return
	}

	if got != 0 {
		t.Fatalf("runGoVet() = %d, want 0", got)
	}

	if got := runGoTests(Config{}, []string{"go/main.go"}); got != 0 {
		t.Fatalf("runGoTests() = %d, want 0", got)
	}

	if got := runGolangciLint(Config{}, []string{"go/main.go"}); got != 0 {
		t.Fatalf("runGolangciLint() = %d, want 0", got)
	}

	stdout := captureStdout(t, func() {
		if got := runGoFormatCheck(Config{}, []string{"go/main.go"}); got != 1 {
			t.Fatalf("runGoFormatCheck() = %d, want 1", got)
		}
	})
	if !strings.Contains(stdout, "GOFMT CHECK FAILED") ||
		!strings.Contains(stdout, "go/main.go") ||
		!strings.Contains(stdout, "gofmt-check,go/main.go,1,0,error,format") {
		t.Fatalf("gofmt output missing expected finding:\n%s", stdout)
	}
}

func TestRunGoCoverageThresholdBlocksBelowFloor(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)
	writeGoCoveragePolicyBundle(t, tempDir)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, "go:\n  worktree: go\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "go"),
		`#!/usr/bin/env sh
case "$1 $2" in
  "list -buildvcs=false")
    printf 'blackcat.ca/coding-ethos/go/pkg\n'
    printf 'blackcat.ca/coding-ethos/go/internal/e2e\n'
    exit 0
    ;;
  "test -buildvcs=false")
    exit 0
    ;;
  "tool cover")
    printf 'total:\t(statements)\t79.8%%\n'
    exit 0
    ;;
esac
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout := captureStdout(t, func() {
		if got := runGoCoverageThreshold(Config{}, []string{"go/main.go"}); got != 1 {
			t.Fatalf("runGoCoverageThreshold() = %d, want 1", got)
		}
	})

	for _, want := range []string{
		"tool: go-test",
		"status: FAIL",
		"GO COVERAGE POLICY FAILED",
		coverageFloorPolicyID,
		"Go test coverage is below the required 80% floor.",
		"trace_id:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("coverage output missing %q:\n%s", want, stdout)
		}
	}
}

func TestRunGoCoverageThresholdWarnsBelowGoal(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)
	writeGoCoveragePolicyBundle(t, tempDir)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, "go:\n  worktree: go\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "go"),
		`#!/usr/bin/env sh
case "$1 $2" in
  "list -buildvcs=false")
    printf 'blackcat.ca/coding-ethos/go/pkg\n'
    printf 'blackcat.ca/coding-ethos/go/internal/e2e\n'
    exit 0
    ;;
  "test -buildvcs=false")
    exit 0
    ;;
  "tool cover")
    printf 'pkg/app.go:12:\tApp\t88.5%%\n'
    printf 'total:\t(statements)\t85.0%%\n'
    exit 0
    ;;
esac
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout := captureStdout(t, func() {
		if got := runGoCoverageThreshold(Config{}, []string{"go/main.go"}); got != 0 {
			t.Fatalf("runGoCoverageThreshold() = %d, want warning-only success", got)
		}
	})

	for _, want := range []string{
		"status: WARN",
		coverageGoalPolicyID,
		"Go test coverage is below the 90% project goal.",
		"pkg/app.go",
		"trace_id:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("coverage warning output missing %q:\n%s", want, stdout)
		}
	}
}

func TestGoCoverageDisplayFindingsSummarizesLargeWarningSets(t *testing.T) {
	t.Parallel()

	findings := make([]hookFinding, 0, 26)

	findings = append(findings, hookFinding{
		Tool:     goTestToolName,
		Severity: coverageWarnDecision,
		Code:     "coverage-total",
		PolicyID: coverageGoalPolicyID,
		Message:  "total coverage below goal",
	})
	for index := range 25 {
		findings = append(findings, hookFinding{
			Tool:     goTestToolName,
			File:     fmt.Sprintf("pkg/file_%02d.go", index),
			Severity: coverageWarnDecision,
			Code:     "coverage-file",
			PolicyID: coverageGoalPolicyID,
			Message:  "file coverage below goal",
		})
	}

	display := goCoverageDisplayFindings(findings)
	if len(display) != 13 {
		t.Fatalf("display findings = %d, want 13", len(display))
	}

	if display[0].Code != "coverage-total" {
		t.Fatalf("first display finding = %#v, want total coverage", display[0])
	}

	last := display[len(display)-1]
	if last.Code != "coverage-summary" ||
		!strings.Contains(last.Message, "additional coverage finding") {
		t.Fatalf("last display finding = %#v, want coverage summary", last)
	}

	if summary := goCoverageReportSummary(findings); !strings.Contains(
		summary,
		"full detail is retained in SARIF and hook traces",
	) {
		t.Fatalf("summary = %q", summary)
	}
}

func TestRunGoCoverageThresholdExcludesE2EPackages(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)
	writeGoCoveragePolicyBundle(t, tempDir)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, "go:\n  worktree: go\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
	testArgsLog := filepath.Join(tempDir, "go-test-args.log")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "go"),
		`#!/usr/bin/env sh
case "$1 $2" in
  "list -buildvcs=false")
    printf 'blackcat.ca/coding-ethos/go/pkg\n'
    printf 'blackcat.ca/coding-ethos/go/internal/e2e\n'
    exit 0
    ;;
  "test -buildvcs=false")
    printf '%s\n' "$*" > `+shellQuoteForTest(testArgsLog)+`
    case "$*" in
      *"/internal/e2e"*)
        printf 'e2e package should not be included in short coverage gate\n' >&2
        exit 3
        ;;
    esac
    exit 0
    ;;
  "tool cover")
    printf 'total:\t(statements)\t95.0%%\n'
    exit 0
    ;;
esac
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	if got := runGoCoverageThreshold(Config{}, []string{"go/main.go"}); got != 0 {
		t.Fatalf("runGoCoverageThreshold() = %d, want 0", got)
	}

	testArgs, err := os.ReadFile(testArgsLog)
	if err != nil {
		t.Fatalf("read go test args: %v", err)
	}

	if strings.Contains(string(testArgs), "/internal/e2e") {
		t.Fatalf("go coverage included e2e package: %s", testArgs)
	}
}

func TestRunGoCoverageThresholdReportsCommandFailure(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)
	writeGoCoveragePolicyBundle(t, tempDir)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(t, overridePath, "go:\n  worktree: go\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "go"),
		`#!/usr/bin/env sh
case "$1 $2" in
  "list -buildvcs=false")
    printf 'go list failed\n' >&2
    exit 4
    ;;
esac
printf 'unexpected go invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout := captureStdout(t, func() {
		if got := runGoCoverageThreshold(Config{}, []string{"go/main.go"}); got != 4 {
			t.Fatalf("runGoCoverageThreshold() = %d, want 4", got)
		}
	})

	for _, want := range []string{
		"GO COVERAGE COMMAND FAILED",
		"UNPARSEABLE_OUTPUT",
		"go-list exited with status 4 without parseable diagnostics",
		"trace_id:",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("coverage command failure output missing %q:\n%s", want, stdout)
		}
	}
}

func TestEvaluateGoCoveragePolicyBlocksBelowFloor(t *testing.T) {
	t.Parallel()

	context := evaluators.Context{
		Cwd:       "/repo",
		EventName: "lint-capture",
		Provider:  "lint",
		Scope:     "tool:go-test",
		Tool:      goTestToolName,
		Diagnostics: []diagnostics.Diagnostic{{
			Metadata: map[string]any{"coverage_percent": 79.8},
			Tool:     goTestToolName,
			Code:     "coverage-total",
			Severity: "record",
			Message:  "Go test coverage is 79.80%.",
		}},
	}

	decisions, err := evaluateGoCoveragePolicy(
		testGoCoverageFloorPolicy(),
		context,
		evaluators.DefaultRegistry(),
	)
	if err != nil {
		t.Fatalf("evaluateGoCoveragePolicy: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("coverage decisions = %#v, want one block decision", decisions)
	}

	decision := decisions[0]
	if decision.PolicyID != coverageFloorPolicyID ||
		decision.Severity != coverageDecisionBlock ||
		decision.Message != "Go test coverage is below the required 80% floor." {
		t.Fatalf("coverage decision = %#v", decision)
	}

	if len(decision.Diagnostics) != 1 {
		t.Fatalf("coverage diagnostics = %#v, want one diagnostic", decision.Diagnostics)
	}

	diagnostic := decision.Diagnostics[0]
	if diagnostic.Tool != goTestToolName ||
		diagnostic.PolicyID != coverageFloorPolicyID ||
		diagnostic.Metadata["implementation"] != "cel" {
		t.Fatalf("coverage diagnostic = %#v", diagnostic)
	}
}

func TestEvaluateGoCoveragePolicyAllowsPassingCoverage(t *testing.T) {
	t.Parallel()

	context := evaluators.Context{
		Cwd:       "/repo",
		EventName: "lint-capture",
		Provider:  "lint",
		Scope:     "tool:go-test",
		Tool:      goTestToolName,
		Diagnostics: []diagnostics.Diagnostic{{
			Metadata: map[string]any{"coverage_percent": 80.0},
			Tool:     goTestToolName,
			Code:     "coverage-total",
			Severity: "record",
			Message:  "Go test coverage is 80.00%.",
		}},
	}

	decisions, err := evaluateGoCoveragePolicy(
		testGoCoverageFloorPolicy(),
		context,
		evaluators.DefaultRegistry(),
	)
	if err != nil {
		t.Fatalf("evaluateGoCoveragePolicy: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("coverage decisions = %#v, want none", decisions)
	}
}

func TestEvaluateGoCoveragePolicyWarnsBelowGoalForFile(t *testing.T) {
	t.Parallel()

	context := evaluators.Context{
		Cwd:       "/repo",
		EventName: "lint-capture",
		Provider:  "lint",
		Scope:     "tool:go-test",
		Tool:      goTestToolName,
		Diagnostics: []diagnostics.Diagnostic{{
			Metadata: map[string]any{
				"coverage_percent": 88.5,
				"package":          "blackcat.ca/coding-ethos/go/pkg",
			},
			Tool:     goTestToolName,
			Code:     "coverage-file",
			File:     "pkg/app.go",
			Line:     12,
			Severity: "record",
			Message:  "Go test coverage for pkg/app.go is 88.50%.",
		}},
	}

	decisions, err := evaluateGoCoveragePolicy(
		testGoCoverageGoalPolicy(),
		context,
		evaluators.DefaultRegistry(),
	)
	if err != nil {
		t.Fatalf("evaluateGoCoveragePolicy: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("coverage decisions = %#v, want one warning decision", decisions)
	}

	decision := decisions[0]
	if decision.PolicyID != coverageGoalPolicyID ||
		decision.Severity != coverageWarnDecision ||
		decision.Decision != coverageWarnDecision {
		t.Fatalf("coverage warning decision = %#v", decision)
	}

	diagnostic := decision.Diagnostics[0]
	if diagnostic.File != "pkg/app.go" ||
		diagnostic.Line != 12 ||
		diagnostic.Metadata["coverage_percent"] != 88.5 {
		t.Fatalf("coverage warning diagnostic = %#v", diagnostic)
	}
}

func TestGoCoverageReportStatusWarnsWithoutBlocking(t *testing.T) {
	t.Parallel()

	status, exitCode := goCoverageReportStatus([]policy.Decision{{
		Decision: coverageWarnDecision,
		Severity: coverageWarnDecision,
	}})
	if status != statusWarn || exitCode != 0 {
		t.Fatalf(
			"goCoverageReportStatus(warning) = %q, %d; want WARN, 0",
			status,
			exitCode,
		)
	}

	status, exitCode = goCoverageReportStatus([]policy.Decision{{
		Decision: coverageDecisionBlock,
		Severity: coverageDecisionBlock,
	}})
	if status != statusFail || exitCode != 1 {
		t.Fatalf(
			"goCoverageReportStatus(block) = %q, %d; want FAIL, 1",
			status,
			exitCode,
		)
	}
}

func TestGoCoverageFindingsPreserveDecisionMetadata(t *testing.T) {
	t.Parallel()

	findings := goCoverageFindings([]policy.Decision{{
		Message:      "coverage below floor",
		PolicyID:     coverageFloorPolicyID,
		Severity:     coverageDecisionBlock,
		Suggestion:   "add meaningful tests",
		PrincipleIDs: []string{"testing-as-specification"},
		Diagnostics: []diagnostics.Diagnostic{{
			Metadata: map[string]any{"coverage_percent": 79.8},
			Tool:     "policy",
			Code:     "coverage-total",
			Message:  "coverage below floor",
		}},
	}})

	if len(findings) != 1 {
		t.Fatalf("goCoverageFindings() = %#v, want one finding", findings)
	}

	got := findings[0]
	if got.Tool != goTestToolName ||
		got.PolicyID != coverageFloorPolicyID ||
		got.Severity != coverageDecisionBlock ||
		got.Code != "coverage-total" ||
		got.SkillID != "lint-remediation" ||
		got.Metadata["coverage_percent"] != 79.8 {
		t.Fatalf("coverage finding = %#v", got)
	}
}

func TestGoCoveragePolicyAppliesOnlyToGoTestTools(t *testing.T) {
	t.Parallel()

	if !goCoveragePolicyApplies(policy.Policy{
		AppliesTo: policy.AppliesTo{Tools: []string{goTestToolName}},
	}) {
		t.Fatal("goCoveragePolicyApplies() rejected go-test")
	}

	if goCoveragePolicyApplies(policy.Policy{
		AppliesTo: policy.AppliesTo{Tools: []string{"pytest"}},
	}) {
		t.Fatal("goCoveragePolicyApplies() accepted pytest")
	}
}

func TestGoCoveragePolicyConfiguredDetectsPolicyBundleBar(t *testing.T) {
	tempDir := t.TempDir()
	ethosRoot := filepath.Join(tempDir, "code-ethos")

	configured, err := goCoveragePolicyConfiguredAt(ethosRoot)
	if err == nil {
		t.Fatal("goCoveragePolicyConfigured() accepted missing policy bundle")
	}
	if configured {
		t.Fatal("goCoveragePolicyConfigured() reported missing bundle as configured")
	}

	writeNoGoCoveragePolicyBundle(t, tempDir)

	configured, err = goCoveragePolicyConfiguredAt(ethosRoot)
	if err != nil {
		t.Fatalf("goCoveragePolicyConfigured() rejected valid non-coverage bundle: %v", err)
	}
	if configured {
		t.Fatal("goCoveragePolicyConfigured() accepted bundle without coverage policy")
	}

	writeGoCoveragePolicyBundle(t, tempDir)

	configured, err = goCoveragePolicyConfiguredAt(ethosRoot)
	if err != nil {
		t.Fatalf("goCoveragePolicyConfigured() rejected coverage policy bundle: %v", err)
	}
	if !configured {
		t.Fatal("goCoveragePolicyConfigured() rejected coverage policy bundle")
	}
}

func TestGoCoverageProfilePathCreatesTemporaryFile(t *testing.T) {
	t.Parallel()

	path, cleanup, err := goCoverageProfilePath()
	if err != nil {
		t.Fatalf("goCoverageProfilePath: %v", err)
	}

	_, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("coverage profile was not created: %v", statErr)
	}

	cleanup()

	_, statErr = os.Stat(path)
	if !os.IsNotExist(statErr) {
		t.Fatalf("coverage profile cleanup error = %v, want removed file", statErr)
	}
}

func testGoCoverageFloorPolicy() policy.Policy {
	return policy.Policy{
		ID:              coverageFloorPolicyID,
		DefaultSeverity: coverageDecisionBlock,
		Message:         "Go test coverage is below the required 80% floor.",
		Suggestion:      "Add meaningful Go tests before committing.",
		AppliesTo: policy.AppliesTo{
			Tools: []string{goTestToolName},
		},
		Evaluators: []policy.Evaluator{{
			Name: "cel.expression",
			Options: map[string]any{
				"skill_id": "lint-remediation",
				"when": `coverage.exists(item,
					item.tool == "go-test" &&
					item.total &&
					item.percent < 80.0)`,
			},
		}},
		PrincipleIDs: []string{
			"testing-as-specification",
			"functional-testing-is-the-proof",
		},
	}
}

func testGoCoverageGoalPolicy() policy.Policy {
	return policy.Policy{
		ID:              coverageGoalPolicyID,
		DefaultSeverity: coverageWarnDecision,
		Message:         "Go test coverage is below the 90% project goal.",
		Suggestion:      "Add meaningful total-suite and per-file Go coverage.",
		AppliesTo: policy.AppliesTo{
			Tools: []string{goTestToolName},
		},
		Evaluators: []policy.Evaluator{{
			Name: "cel.expression",
			Options: map[string]any{
				"skill_id": "lint-remediation",
				"when": `coverage.exists(item,
					item.tool == "go-test" &&
					(item.total || item.code == "coverage-file") &&
					item.percent < 90.0 &&
					(item.package == "" ||
					item.package.startsWith("blackcat.ca/coding-ethos/go")))`,
			},
		}},
		PrincipleIDs: []string{
			"testing-as-specification",
			"functional-testing-is-the-proof",
		},
	}
}

func writeGoCoveragePolicyBundle(t *testing.T, root string) {
	t.Helper()

	payload, err := json.Marshal(policy.Bundle{
		Version: 1,
		Policies: map[string]policy.Policy{
			coverageFloorPolicyID: testGoCoverageFloorPolicy(),
			coverageGoalPolicyID:  testGoCoverageGoalPolicy(),
		},
		Skills: map[string]policy.Skill{},
	})
	if err != nil {
		t.Fatalf("marshal coverage policy bundle: %v", err)
	}

	mustWriteTestFile(
		t,
		filepath.Join(root, "code-ethos", "build", "policy", "policy-bundle.json"),
		string(payload),
	)
}

func writeNoGoCoveragePolicyBundle(t *testing.T, root string) {
	t.Helper()

	payload, err := json.Marshal(policy.Bundle{
		Version:  1,
		Policies: map[string]policy.Policy{},
		Skills:   map[string]policy.Skill{},
	})
	if err != nil {
		t.Fatalf("marshal non-coverage policy bundle: %v", err)
	}

	mustWriteTestFile(
		t,
		filepath.Join(root, "code-ethos", "build", "policy", "policy-bundle.json"),
		string(payload),
	)
}

func TestPathWithoutHookGitShimsRemovesRuntimeAndLocalShims(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	realGitDir := filepath.Join(tempDir, "usr", "bin")
	runtimeDir := filepath.Join(tempDir, ".git", "coding-ethos-hooks", "bin")
	localBinDir := filepath.Join(tempDir, "coding-ethos", "bin")
	normalDir := filepath.Join(tempDir, "tools", "bin")

	for _, dir := range []string{realGitDir, runtimeDir, localBinDir, normalDir} {
		err := os.MkdirAll(dir, 0o700)
		if err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}

	mustWriteExecutable(
		t,
		filepath.Join(localBinDir, "git"),
		"#!/usr/bin/env sh\nexport CODING_ETHOS_REAL_GIT=/usr/bin/git\nexec /repo/bin/coding-ethos-run policy-git \"$@\"\n",
	)

	path := strings.Join(
		[]string{runtimeDir, localBinDir, normalDir, realGitDir},
		string(os.PathListSeparator),
	)

	got := pathWithoutHookGitShims(path, filepath.Join(realGitDir, "git"))

	want := strings.Join([]string{realGitDir, normalDir}, string(os.PathListSeparator))
	if got != want {
		t.Fatalf("pathWithoutHookGitShims() = %q, want %q", got, want)
	}
}

func TestCatalogLintCommandsRunExternalToolsAndParseFindings(t *testing.T) {
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := setupGitHookTestRepo(t)
	bundleRoot := writeManagedToolchainBundle(t, tempDir)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	writeCatalogLintFixtures(t)
	writeCatalogLintTools(t, tempDir)

	if !nativeSandboxAvailable {
		stdout := captureStdout(t, func() {
			if got := runHadolint(Config{}, []string{"Dockerfile"}); got == 0 {
				t.Fatal("runHadolint() succeeded inside unavailable nested sandbox")
			}
		})
		if !strings.Contains(stdout, "runtime.sandbox_denial") {
			t.Fatalf("nested sandbox output missing denial:\n%s", stdout)
		}

		return
	}

	stdout := captureStdout(t, func() {
		assertCatalogCommand(t, "hadolint", runHadolint, "Dockerfile")
		assertCatalogCommand(
			t,
			"actionlint",
			runActionlint,
			".github/workflows/ci.yml",
		)
		assertCatalogCommand(t, "bandit", runBandit, "pkg/app.py")
		assertCatalogCommand(t, "sqlfluff", runSQLFluff, "queries/app.sql")
		assertCatalogCommand(t, "tombi", runTombi, "config.toml")
		assertCatalogCommand(t, "dotenv-linter", runDotenvLinter, ".env.example")
	})

	for _, want := range []string{
		"tool: hadolint",
		"tool: actionlint",
		"tool: bandit",
		"tool: sqlfluff",
		"tool: tombi",
		"tool: dotenv-linter",
		"Dockerfile",
		".github/workflows/ci.yml",
		"pkg/app.py",
		"queries/app.sql",
		"config.toml",
		".env.example",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("catalog lint output missing %q:\n%s", want, stdout)
		}
	}
}

func writeManagedToolchainBundle(t *testing.T, root string) string {
	t.Helper()

	ethosRoot := filepath.Join(root, "code-ethos")
	bundleRoot := filepath.Join(ethosRoot, "pre-commit")
	mustWriteTestFile(t, filepath.Join(ethosRoot, "config.yaml"), "version: 1\n")
	buildTestSandboxHelper(t, filepath.Join(ethosRoot, "bin", "coding-ethos-sandbox"))
	mustWriteTestFile(
		t,
		filepath.Join(ethosRoot, "build", "policy", "policy-bundle.json"),
		`{"version":1,"policies":{},"skills":{},"evidence_maps":[]}`,
	)
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "managed-toolchain.tsv"),
		"",
	)
	mustWriteTestFile(t, filepath.Join(bundleRoot, "hooks", "pyproject.toml"), "")

	_, err := toolconfigs.Sync(ethosRoot, root, "")
	if err != nil {
		t.Fatalf("sync generated tool configs: %v", err)
	}

	return bundleRoot
}

func writeCatalogLintFixtures(t *testing.T) {
	t.Helper()

	mustWriteTestFile(t, "Dockerfile", "FROM ubuntu\n")
	mustWriteTestFile(t, ".github/workflows/ci.yml", "name: ci\n")
	mustWriteTestFile(t, "pkg/app.py", "print('ok')\n")
	mustWriteTestFile(t, "queries/app.sql", "select 1\n")
	mustWriteTestFile(t, "config.toml", "bad = true\n")
	mustWriteTestFile(t, ".env.example", "lower=value\n")
}

func writeCatalogLintTools(t *testing.T, tempDir string) {
	t.Helper()

	fakeBin := filepath.Join(tempDir, "bin")
	goBin := filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin")
	githubBin := filepath.Join(
		tempDir,
		"code-ethos",
		"build",
		"toolchain",
		"github-bin",
	)
	mustWriteExecutable(
		t,
		filepath.Join(githubBin, "hadolint"),
		`#!/usr/bin/env sh
printf '[{"line":3,"column":1,"file":"Dockerfile","level":"warning","code":"DL3008","message":"Pin versions in apt get install."}]\n'
exit 1
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(goBin, "actionlint"),
		`#!/usr/bin/env sh
printf '%s\n' '{"filepath":".github/workflows/ci.yml","line":12,"column":5,"kind":"syntax-check","message":"property \"run\" is not defined"}'
exit 1
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(githubBin, "dotenv-linter"),
		"#!/usr/bin/env sh\nprintf '.env.example:3 LowercaseKey: The key should be uppercase\\n'\nexit 1\n",
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "uv"),
		`#!/usr/bin/env sh
case "$*" in
  *" bandit "*)
    printf '{"results":[{"filename":"pkg/app.py","line_number":10,"issue_severity":"HIGH","test_id":"B602","issue_text":"subprocess call with shell=True"}]}'
    exit 1
    ;;
  *" sqlfluff "*)
    printf '[{"filepath":"queries/app.sql","violations":[{"line_no":2,"line_pos":7,"code":"LT01","description":"Expected single whitespace."}]}]'
    exit 1
    ;;
  *" tombi "*)
    printf 'Error: invalid key\n    at config.toml:2:4\n'
    exit 1
    ;;
esac
printf 'unexpected uv invocation: %s\n' "$*" >&2
exit 2
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "gofmt"),
		`#!/usr/bin/env sh
exit 1
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func assertCatalogCommand(
	t *testing.T,
	name string,
	run func(Config, []string) int,
	file string,
) {
	t.Helper()

	if got := run(Config{}, []string{file}); got != 1 {
		t.Fatalf("%s command = %d, want 1", name, got)
	}
}

func assertComplexityFinding(t *testing.T) {
	t.Helper()

	complexity := parseComplexityFindings(
		"  pkg/app.py:42 build_payload (complexity: 19)",
	)
	if len(complexity) != 1 || complexity[0].Code != "cyclomatic-complexity" ||
		complexity[0].Line != 42 {
		t.Fatalf("parseComplexityFindings() = %#v", complexity)
	}

	radon := parseRadonComplexityFindings(
		`{"pkg/app.py":[{"type":"function","rank":"C","lineno":42,"name":"build_payload","complexity":19}]}`,
		15,
	)
	if len(radon) != 1 ||
		radon[0].Code != "cyclomatic-complexity" ||
		radon[0].Message != "build_payload" ||
		radon[0].Detail != "complexity: 19" {
		t.Fatalf("parseRadonComplexityFindings() = %#v", radon)
	}
}

func assertMaintainabilityFinding(t *testing.T) {
	t.Helper()

	maintainability := parseMaintainabilityFindings("  pkg/app.py (MI: 42.50)")
	if len(maintainability) != 1 || maintainability[0].Code != "maintainability-index" {
		t.Fatalf("parseMaintainabilityFindings() = %#v", maintainability)
	}

	radon := parseRadonMaintainabilityFindings(
		`{"pkg/app.py":{"mi":42.5,"rank":"C"}}`,
		50,
	)
	if len(radon) != 1 ||
		radon[0].Code != "maintainability-index" ||
		radon[0].Detail != "MI: 42.50" {
		t.Fatalf("parseRadonMaintainabilityFindings() = %#v", radon)
	}
}

func assertMaintainabilityTimeoutFinding(t *testing.T) {
	t.Helper()

	timeout := parseMaintainabilityFindings("Error: radon timed out after 60s")
	if len(timeout) != 1 ||
		timeout[0].Code != timeoutCode ||
		timeout[0].Message != "radon timed out after 60s" ||
		timeout[0].Advice == "" {
		t.Fatalf("parseMaintainabilityFindings(timeout) = %#v", timeout)
	}
}

func assertVultureFinding(t *testing.T) {
	t.Helper()

	vulture := parseVultureFindings(
		"pkg/app.py:17: unused function 'helper' (60% confidence)",
	)
	if len(vulture) != 1 || vulture[0].Code != "unused-code" || vulture[0].Line != 17 {
		t.Fatalf("parseVultureFindings() = %#v", vulture)
	}
}
