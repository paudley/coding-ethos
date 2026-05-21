// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

const (
	complexityThreshold      = 15
	coverageCodeFile         = "coverage-file"
	coverageCodeTotal        = "coverage-total"
	coverageDecisionBlock    = "block"
	coverageDecisionWarn     = "warn"
	goCoverageGoalPolicyID   = "testing.go_coverage_goal"
	goCoverageTempDirMode    = 0o700
	maintainabilityThreshold = 50
	timeoutCode              = "timeout"
	vultureMinConfidence     = 80
	gitShimProbeBytes        = 4096
	goTestToolName           = "go-test"
)

func runHadolint(_ Config, paths []string) int {
	return runCatalogLintTool("hadolint", paths)
}

func runActionlint(_ Config, paths []string) int {
	return runCatalogLintTool("actionlint", paths)
}

func runBandit(_ Config, paths []string) int {
	return runCatalogLintTool("bandit", paths)
}

func runSQLFluff(_ Config, paths []string) int {
	return runCatalogLintTool("sqlfluff", paths)
}

func runTombi(_ Config, paths []string) int {
	return runCatalogLintTool("tombi", paths)
}

func runDotenvLinter(_ Config, paths []string) int {
	return runCatalogLintTool("dotenv-linter", paths)
}

func runESLint(_ Config, paths []string) int {
	return runCatalogLintTool("eslint", paths)
}

func runTSC(_ Config, paths []string) int {
	return runCatalogLintTool("tsc", paths)
}

func runCatalogLintTool(name string, paths []string) int {
	files := toolchainFiles(name, existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runManagedPolicyTool(name, managedPolicyToolArgsForFiles(name, files))
}

func runPythonComplexity(_ Config, paths []string) int {
	files := formatPythonFiles(paths)
	if len(files) == 0 {
		return 0
	}

	return runManagedPolicyTool(
		"python-complexity",
		managedPolicyToolArgsForFiles("python-complexity", files),
	)
}

func runPythonMaintainability(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	return runManagedPolicyTool(
		"python-maintainability",
		managedPolicyToolArgsForFiles("python-maintainability", nil),
	)
}

func runPythonVulture(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	args := append(
		managedPolicyToolArgsForFiles("python-vulture", nil),
		vultureWhitelistArgs()...,
	)
	args = append(
		args,
		"--min-confidence",
		strconv.Itoa(vultureMinConfidence),
		"--exclude",
		strings.Join(vultureExcludePatterns(), ","),
	)

	return runManagedPolicyTool("python-vulture", args)
}

func runGoFormatCheck(_ Config, paths []string) int {
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}

	result := runExternalTool(
		externalToolRequest{
			Name:    "gofmt-check",
			Dir:     worktree,
			Command: []string{"gofmt", "-l", "."},
		},
	)
	if strings.TrimSpace(result.Combined) != "" {
		findings := parseGofmtCheckFindings(result.Combined)

		if len(findings) == 0 {
			findings = []hookFinding{{
				Tool:     "gofmt-check",
				Severity: "error",
				Message:  "Go files are not gofmt-formatted.",
				Detail: "stdout/stderr were captured but are not rendered because " +
					"they were not parsed into file diagnostics.",
			}}
		}

		emitHookReport(os.Stdout, hookReport{
			Tool:     "gofmt-check",
			Title:    "GOFMT CHECK FAILED",
			Findings: findings,
			Guidance: []string{"Run gofmt on the listed files before pushing."},
		}, selectedHookOutputFormat())

		return 1
	}

	if result.RunnerFailure != nil {
		emitHookReport(os.Stdout, hookReport{
			Tool:  "gofmt-check",
			Title: "GOFMT CHECK RUNNER FAILED",
			Findings: []hookFinding{{
				Tool:     "gofmt-check",
				Severity: "fatal",
				Message:  result.RunnerFailure.Error(),
			}},
			Guidance: []string{"Install gofmt or fix the Go toolchain configuration."},
		}, selectedHookOutputFormat())

		return 1
	}

	return result.ExitCode
}

func parseGofmtCheckFindings(output string) []hookFinding {
	return diagnosticsToHookFindings(
		diagnostics.Parse("gofmt-check", output, ""),
		"gofmt-check",
	)
}

func runGoVet(_ Config, paths []string) int {
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktreeName()
	if !ok {
		return 1
	}

	return runManagedPolicyTool("go-vet", []string{worktree})
}

func runGoTests(_ Config, paths []string) int {
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktreeName()
	if !ok {
		return 1
	}

	return runManagedPolicyTool(goTestToolName, []string{worktree})
}

func runGoCoverageThreshold(cfg Config, paths []string) int {
	if cfg.HookStage == hookStagePreCommit {
		return 0
	}

	shouldRun, exitCode := shouldRunGoCoverage(paths)
	if !shouldRun {
		return exitCode
	}

	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}

	coverProfile, cleanup, err := goCoverageProfilePath(worktree)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}
	defer cleanup()

	packages, listResult := goCoveragePackages(worktree)
	if listResult.RunnerFailure != nil || listResult.ExitCode != 0 {
		return reportGoCoverageToolFailure("go-list", listResult)
	}

	if len(packages) == 0 {
		return 0
	}

	testResult := runExternalTool(externalToolRequest{
		Name: goTestToolName,
		Dir:  worktree,
		Command: append([]string{
			"go",
			"test",
			"-buildvcs=false",
			"-timeout=30s",
			"-short",
			"-covermode=atomic",
			"-coverprofile=" + coverProfile,
		}, packages...),
	})
	if testResult.RunnerFailure != nil || testResult.ExitCode != 0 {
		return reportGoCoverageToolFailure(goTestToolName, testResult)
	}

	coverResult := runExternalTool(externalToolRequest{
		Name:    goTestToolName,
		Dir:     worktree,
		Command: []string{"go", "tool", "cover", "-func=" + coverProfile},
	})
	if coverResult.RunnerFailure != nil || coverResult.ExitCode != 0 {
		return reportGoCoverageToolFailure(goTestToolName, coverResult)
	}

	return reportGoCoveragePolicy(coverResult.Combined)
}

func shouldRunGoCoverage(paths []string) (bool, int) {
	if len(goFiles(existingFiles(paths))) == 0 {
		return false, 0
	}

	configured, err := goCoveragePolicyConfigured()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return false, 1
	}

	return configured, 0
}

func goCoveragePolicyConfigured() (bool, error) {
	bundleRoot, err := findBundleRoot()
	if err != nil {
		return false, err
	}

	return goCoveragePolicyConfiguredAt(filepath.Dir(bundleRoot))
}

func goCoveragePolicyConfiguredAt(ethosRoot string) (bool, error) {
	policyContext, err := managedPolicyContext(ethosRoot)
	if err != nil {
		return false, err
	}

	return slices.ContainsFunc(policyContext.Policies, goCoveragePolicyApplies), nil
}

func goCoveragePackages(worktree string) ([]string, externalToolResult) {
	result := runExternalTool(externalToolRequest{
		Name:    "go-list",
		Dir:     worktree,
		Command: []string{"go", "list", "-buildvcs=false", "./..."},
	})
	if result.RunnerFailure != nil || result.ExitCode != 0 {
		return nil, result
	}

	packages := []string{}

	for line := range strings.Lines(result.Stdout) {
		packageName := strings.TrimSpace(line)
		if packageName == "" || strings.HasSuffix(packageName, "/internal/e2e") {
			continue
		}

		packages = append(packages, packageName)
	}

	return packages, externalToolResult{}
}

func reportGoCoverageToolFailure(tool string, result externalToolResult) int {
	finding := genericToolFailureFindingForResult(tool, result)
	if result.RunnerFailure != nil {
		finding.Message = result.RunnerFailure.Error()
	}

	findings := []hookFinding{finding}

	if !result.TimedOut && result.RunnerFailure == nil {
		parsed := diagnosticsToHookFindings(
			diagnostics.Parse(tool, result.Stdout, result.Stderr),
			tool,
		)
		if len(parsed) > 0 {
			findings = append(findings[:0], parsed...)
			findings = append(findings, finding)
		}
	}

	emitHookReport(os.Stdout, hookReport{
		Tool:     goTestToolName,
		Title:    "GO COVERAGE COMMAND FAILED",
		Status:   statusFail,
		Findings: findings,
		Guidance: []string{"Fix the Go coverage command failure before continuing."},
	}, selectedHookOutputFormat())

	if result.ExitCode != 0 {
		return result.ExitCode
	}

	return 1
}

func goCoverageProfilePath(worktree string) (string, func(), error) {
	tempDir := filepath.Join(worktree, sandbox.SandboxTempWritePath)

	err := os.MkdirAll(tempDir, goCoverageTempDirMode)
	if err != nil {
		return "", func() {}, fmt.Errorf("create Go coverage temp dir: %w", err)
	}

	file, err := os.CreateTemp(tempDir, "coding-ethos-go-coverage-*.out")
	if err != nil {
		return "", func() {}, fmt.Errorf("create Go coverage profile: %w", err)
	}

	path := file.Name()

	closeErr := file.Close()
	if closeErr != nil {
		_ = os.Remove(path)

		return "", func() {}, fmt.Errorf("close Go coverage profile: %w", closeErr)
	}

	return path, func() { _ = os.Remove(path) }, nil
}

func reportGoCoveragePolicy(output string) int {
	diagnosticItems := diagnostics.Parse(goTestToolName, output, "")

	decisions, err := evaluateGoCoveragePolicies(diagnosticItems)
	if err != nil {
		emitHookReport(os.Stdout, hookReport{
			Tool:  goTestToolName,
			Title: "GO COVERAGE POLICY FAILED",
			Findings: []hookFinding{{
				Tool:     goTestToolName,
				Severity: "error",
				Message:  err.Error(),
			}},
			Guidance: []string{"Fix Go coverage policy evaluation before continuing."},
		}, selectedHookOutputFormat())

		return 1
	}

	if len(decisions) == 0 {
		return 0
	}

	status, exitCode := goCoverageReportStatus(decisions)
	findings := goCoverageFindings(decisions)

	emitHookReport(os.Stdout, hookReport{
		Tool:            goTestToolName,
		Title:           "GO COVERAGE POLICY FAILED",
		Status:          status,
		Summary:         goCoverageReportSummary(findings),
		Findings:        findings,
		DisplayFindings: goCoverageDisplayFindings(findings),
		Guidance:        []string{"Add meaningful Go tests before committing or pushing."},
	}, selectedHookOutputFormat())

	return exitCode
}

func goCoverageReportStatus(decisions []policy.Decision) (string, int) {
	for _, decision := range decisions {
		if decision.Decision == coverageDecisionBlock ||
			decision.Severity == coverageDecisionBlock {
			return statusFail, 1
		}
	}

	return statusWarn, 0
}

func goCoverageFindings(decisions []policy.Decision) []hookFinding {
	findings := []hookFinding{}

	for _, decision := range decisions {
		for _, diagnostic := range decision.Diagnostics {
			findings = append(findings, hookFinding{
				Metadata:     diagnostic.Metadata,
				Tool:         goTestToolName,
				File:         diagnostic.File,
				Severity:     decision.Severity,
				Code:         diagnostic.Code,
				PolicyID:     decision.PolicyID,
				SkillID:      "lint-remediation",
				Message:      decision.Message,
				Advice:       decision.Suggestion,
				PrincipleIDs: decision.PrincipleIDs,
				Line:         diagnostic.Line,
				Column:       diagnostic.Column,
			})
		}
	}

	return findings
}

func goCoverageReportSummary(findings []hookFinding) string {
	total := 0
	fileWarnings := 0
	blocking := 0

	for _, finding := range findings {
		if finding.Code == coverageCodeTotal {
			total++
		}

		if finding.Code == coverageCodeFile {
			fileWarnings++
		}

		if finding.Severity == coverageDecisionBlock {
			blocking++
		}
	}

	if len(findings) == 0 {
		return ""
	}

	parts := []string{
		fmt.Sprintf("%d coverage policy finding(s)", len(findings)),
	}
	if total > 0 {
		parts = append(parts, fmt.Sprintf("%d total-suite", total))
	}

	if fileWarnings > 0 {
		parts = append(parts, fmt.Sprintf("%d file/function detail", fileWarnings))
	}

	if blocking > 0 {
		parts = append(parts, fmt.Sprintf("%d blocking", blocking))
	}

	parts = append(parts, "full detail is retained in SARIF and hook traces")

	return strings.Join(parts, "; ")
}

func goCoverageDisplayFindings(findings []hookFinding) []hookFinding {
	const maxCoverageDisplayFindings = 12

	if len(findings) <= maxCoverageDisplayFindings {
		return findings
	}

	display := make([]hookFinding, 0, maxCoverageDisplayFindings+1)

	for _, finding := range findings {
		if finding.Code == coverageCodeTotal {
			display = append(display, finding)
		}
	}

	for _, finding := range findings {
		if finding.Code != coverageCodeFile {
			continue
		}

		display = append(display, finding)

		if len(display) >= maxCoverageDisplayFindings {
			break
		}
	}

	omitted := len(findings) - len(display)
	if omitted > 0 {
		display = append(display, hookFinding{
			Tool:     goTestToolName,
			Severity: coverageDecisionWarn,
			Code:     "coverage-summary",
			PolicyID: goCoverageGoalPolicyID,
			SkillID:  "lint-remediation",
			Message: fmt.Sprintf(
				"%d additional coverage finding(s) omitted from toon output.",
				omitted,
			),
			Advice: "Use the trace_id SARIF or hook trace for complete " +
				"per-file coverage evidence.",
		})
	}

	return display
}

func evaluateGoCoveragePolicies(
	diagnosticItems []diagnostics.Diagnostic,
) ([]policy.Decision, error) {
	bundleRoot, consumer, _, err := loadBundleConsumerAndConfig()
	if err != nil {
		return nil, err
	}

	policyContext, err := managedPolicyContext(filepath.Dir(bundleRoot))
	if err != nil {
		return nil, err
	}

	context := evaluators.Context{
		Cwd:         consumer,
		EventName:   "lint-capture",
		Provider:    "lint",
		Scope:       "tool:" + goTestToolName,
		Tool:        goTestToolName,
		Diagnostics: diagnosticItems,
	}
	registry := evaluators.DefaultRegistry()
	decisions := []policy.Decision{}

	for _, policyDef := range policyContext.Policies {
		if !goCoveragePolicyApplies(policyDef) {
			continue
		}

		evaluated, evalErr := evaluateGoCoveragePolicy(
			policyDef,
			context,
			registry,
		)
		if evalErr != nil {
			return nil, evalErr
		}

		decisions = append(decisions, evaluated...)
	}

	return decisions, nil
}

func evaluateGoCoveragePolicy(
	policyDef policy.Policy,
	context evaluators.Context,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	decisions := []policy.Decision{}

	for _, evaluatorSpec := range policyDef.Evaluators {
		if evaluatorSpec.Name != "cel.expression" {
			continue
		}

		evaluator, found := registry.Lookup(evaluatorSpec.Name)
		if !found {
			continue
		}

		evaluated, err := evaluateGoCoverageDiagnostics(
			evaluator,
			policyDef,
			context,
			evaluatorSpec.Options,
		)
		if err != nil {
			return nil, err
		}

		decisions = append(decisions, evaluated...)
	}

	return decisions, nil
}

func evaluateGoCoverageDiagnostics(
	evaluator evaluators.Evaluator,
	policyDef policy.Policy,
	context evaluators.Context,
	options map[string]any,
) ([]policy.Decision, error) {
	if len(context.Diagnostics) == 0 {
		return evaluateGoCoverageDiagnostic(evaluator, policyDef, context, options)
	}

	decisions := []policy.Decision{}

	for _, diagnostic := range context.Diagnostics {
		item := diagnostic
		diagnosticContext := context
		diagnosticContext.Diagnostic = &item
		diagnosticContext.Diagnostics = nil

		evaluated, err := evaluateGoCoverageDiagnostic(
			evaluator,
			policyDef,
			diagnosticContext,
			options,
		)
		if err != nil {
			return nil, err
		}

		decisions = append(decisions, evaluated...)
	}

	return decisions, nil
}

func evaluateGoCoverageDiagnostic(
	evaluator evaluators.Evaluator,
	policyDef policy.Policy,
	context evaluators.Context,
	options map[string]any,
) ([]policy.Decision, error) {
	context.EvaluatorOptions = options

	evaluated, err := evaluator.Evaluate(policyDef, context)
	if err != nil {
		return nil, fmt.Errorf(
			"evaluate Go coverage policy %s: %w",
			policyDef.ID,
			err,
		)
	}

	return evaluated, nil
}

func goCoveragePolicyApplies(policyDef policy.Policy) bool {
	return slices.Contains(policyDef.AppliesTo.Tools, goTestToolName)
}

func pathWithoutHookGitShims(rawPath, realGit string) string {
	kept := []string{}

	realGitDir := strings.TrimSpace(filepath.Dir(realGit))
	if realGitDir != "." && realGitDir != "" {
		kept = append(kept, realGitDir)
	}

	for _, directory := range filepath.SplitList(rawPath) {
		if directory == "" {
			continue
		}

		normalized := filepath.ToSlash(filepath.Clean(directory))
		if strings.Contains(normalized, "coding-ethos-hooks") ||
			directoryContainsCodingEthosGitShim(directory) {
			continue
		}

		if realGitDir != "" && filepath.Clean(directory) == filepath.Clean(realGitDir) {
			continue
		}

		kept = append(kept, directory)
	}

	return strings.Join(kept, string(os.PathListSeparator))
}

func directoryContainsCodingEthosGitShim(directory string) bool {
	payload, err := readRootedFilePrefix(
		filepath.Join(directory, "git"),
		gitShimProbeBytes,
	)
	if err != nil {
		return false
	}

	text := string(payload)

	return strings.Contains(text, "CODING_ETHOS_REAL_GIT") &&
		strings.Contains(text, "policy-git")
}

func runGolangciLint(cfg Config, paths []string) int {
	if len(toolchainFiles("golangci-lint", existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktreeName()
	if !ok {
		return 1
	}

	args := []string{}
	if cfg.HookStage == hookStagePreCommit {
		args = append(args, "--new-from-rev=HEAD")
	}

	args = append(args, worktree)

	return runManagedPolicyTool("golangci-lint", args)
}

func managedPolicyToolArgsForFiles(name string, files []string) []string {
	tool := toolchainCatalogTool(name)
	runtime := tool.RuntimeSpec()
	fileSpec := tool.FileMatchSpec()

	args := []string{}
	if len(runtime.Command) > 1 {
		args = append(args, runtime.Command[1:]...)
	}

	if fileSpec.PassFilesAsArgs {
		args = append(args, files...)
	}

	return args
}

func runManagedPolicyTool(name string, args []string) int {
	bundleRoot, consumer, _, err := loadBundleConsumerAndConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	ethosRoot := filepath.Dir(bundleRoot)

	policyContext, err := managedPolicyContext(ethosRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	return managedcapture.Run(managedcapture.Options{
		PolicyContext: policyContext,
		Tool:          name,
		EthosRoot:     ethosRoot,
		ConsumerRoot:  consumer,
		InvocationCwd: repoRoot(),
		OutputFormat:  selectedHookOutputFormat(),
		Args:          args,
	})
}

func managedPolicyContext(ethosRoot string) (managedcapture.PolicyContext, error) {
	file, err := openPolicyBundleFile(policyBundlePath(ethosRoot))
	if err != nil {
		return managedcapture.PolicyContext{}, err
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return managedcapture.PolicyContext{}, fmt.Errorf("decode policy bundle: %w", err)
	}

	policies := make([]policy.Policy, 0, len(bundle.Policies))
	for _, item := range bundle.Policies {
		policies = append(policies, item)
	}

	return managedcapture.PolicyContext{
		Skills:       bundle.Skills,
		EvidenceMaps: bundle.EvidenceMaps,
		Policies:     policies,
	}, nil
}

func policyBundlePath(ethosRoot string) string {
	return filepath.Join(ethosRoot, "build", "policy", "policy-bundle.json")
}

func openPolicyBundleFile(path string) (*os.File, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open policy bundle: %w", err)
	}

	return file, nil
}

func configuredGoWorktreeName() (string, bool) {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return "", false
	}

	worktree := configuredGoWorktreeValue(rootConfig)
	path := repoPath(worktree)

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(
			os.Stderr,
			"FATAL: go.worktree is set to %q, but that directory does not exist\n",
			worktree,
		)

		return "", false
	}

	return worktree, true
}

func configuredGoWorktree() (string, bool) {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return "", false
	}

	worktree := configuredGoWorktreeValue(rootConfig)
	path := repoPath(worktree)

	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(
			os.Stderr,
			"FATAL: go.worktree is set to %q, but that directory does not exist\n",
			worktree,
		)

		return "", false
	}

	return path, true
}

func configuredGoWorktreeValue(rootConfig map[string]any) string {
	raw, ok := rootConfigValue(rootConfig, "go.worktree")

	worktree := "go"
	if ok {
		worktree = strings.TrimSpace(fmt.Sprint(raw))
	}

	if worktree == "" || worktree == nilString {
		worktree = "go"
	}

	return worktree
}

func vultureWhitelistArgs() []string {
	for _, candidate := range []string{
		filepath.Join(hooksProjectPath(), "vulture_whitelist.py"),
		filepath.Join(filepath.Dir(hooksProjectPath()), "vulture_whitelist.py"),
	} {
		_, err := os.Stat(candidate)
		if err == nil {
			return []string{candidate}
		}
	}

	return nil
}

func vultureExcludePatterns() []string {
	return []string{
		".venv",
		"*/.venv",
		"*/.venv/*",
		"lib/python/.venv",
		"lib/python/.venv/*",
		".lint-cache",
		"*/.lint-cache",
		"*/.lint-cache/*",
		"lib/python/.lint-cache",
		"lib/python/.lint-cache/*",
		"__pycache__",
		"*/__pycache__",
		"node_modules",
		"*/node_modules",
		"tests",
		"*/tests",
		"*/tests/*",
	}
}

func goFiles(paths []string) []string {
	files := []string{}

	for _, path := range paths {
		if strings.HasSuffix(path, ".go") {
			files = append(files, path)
		}
	}

	return files
}

func dockerFiles(paths []string) []string {
	files := []string{}

	for _, path := range paths {
		if strings.HasPrefix(filepath.Base(path), "Dockerfile") {
			files = append(files, path)
		}
	}

	return files
}

func workflowFiles(paths []string) []string {
	files := []string{}

	for _, path := range paths {
		if !strings.HasPrefix(path, ".github/workflows/") {
			continue
		}

		ext := strings.ToLower(filepath.Ext(path))
		if ext == extYaml || ext == extYml {
			files = append(files, path)
		}
	}

	return files
}

var (
	complexityPattern = regexp.MustCompile(
		`^\s*(.+?):(\d+)\s+(.+?)\s+\(complexity:\s*(\d+)\)$`,
	)
	maintainPattern = regexp.MustCompile(`^\s*(.+?)\s+\(MI:\s*([0-9.]+)\)$`)
)

const (
	complexityMatchParts = 5
	maintainMatchParts   = 3
)

func parseComplexityFindings(output string) []hookFinding {
	findings := []hookFinding{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := complexityPattern.FindStringSubmatch(line)
		if len(matches) != complexityMatchParts {
			continue
		}

		lineNo, ok := parseDiagnosticInt(matches[2])
		if !ok {
			continue
		}

		findings = append(findings, hookFinding{
			Tool:     "complexity",
			File:     matches[1],
			Line:     lineNo,
			Severity: "error",
			Code:     "cyclomatic-complexity",
			Message:  strings.TrimSpace(matches[3]),
			Detail:   "complexity: " + matches[4],
		})
	}

	return findings
}

func parseRadonComplexityFindings(output string, threshold int) []hookFinding {
	if threshold == complexityThreshold {
		findings := diagnosticsToHookFindings(
			diagnostics.Parse("radon-complexity", output, ""),
			"complexity",
		)
		if len(findings) > 0 {
			return findings
		}
	}

	return parseComplexityFindings(output)
}

func parseMaintainabilityFindings(output string) []hookFinding {
	findings := []hookFinding{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		if finding, ok := parseMaintainabilityToolError(line); ok {
			findings = append(findings, finding)

			continue
		}

		matches := maintainPattern.FindStringSubmatch(line)
		if len(matches) != maintainMatchParts {
			continue
		}

		findings = append(findings, hookFinding{
			Tool:     "maintainability",
			File:     matches[1],
			Severity: "warning",
			Code:     "maintainability-index",
			Message:  "Maintainability index below configured threshold.",
			Detail:   "MI: " + matches[2],
		})
	}

	return findings
}

func parseRadonMaintainabilityFindings(output string, threshold int) []hookFinding {
	if threshold == maintainabilityThreshold {
		findings := diagnosticsToHookFindings(
			diagnostics.Parse("radon-maintainability", output, ""),
			"maintainability",
		)
		if len(findings) > 0 {
			return findings
		}
	}

	return parseMaintainabilityFindings(output)
}

func parseMaintainabilityToolError(line string) (hookFinding, bool) {
	message := strings.TrimSpace(strings.TrimPrefix(line, "Error:"))
	if message == strings.TrimSpace(line) || message == "" {
		return hookFinding{}, false
	}

	finding := hookFinding{
		Tool:     "maintainability",
		Severity: "error",
		Message:  message,
		Advice: "Run the maintainability check directly, then simplify or split " +
			"the slow module before committing.",
	}
	if strings.Contains(strings.ToLower(message), "timed out") {
		finding.Code = timeoutCode
	}

	return finding, true
}

func parseVultureFindings(output string) []hookFinding {
	return diagnosticsToHookFindings(
		diagnostics.Parse("vulture", output, ""),
		"vulture",
	)
}

func diagnosticsToHookFindings(
	items []diagnostics.Diagnostic,
	toolName string,
) []hookFinding {
	findings := make([]hookFinding, 0, len(items))
	for _, item := range items {
		findings = append(findings, hookFinding{
			Metadata:     item.Metadata,
			Advice:       item.Advice,
			Confidence:   item.Confidence,
			Tool:         firstNonEmpty(toolName, item.Tool),
			File:         item.File,
			Severity:     item.Severity,
			Code:         item.Code,
			PolicyID:     item.PolicyID,
			SkillID:      item.SkillID,
			Message:      item.Message,
			Meaning:      item.Meaning,
			Detail:       item.Detail,
			AdviceSteps:  append([]string(nil), item.AdviceSteps...),
			PrincipleIDs: append([]string(nil), item.PrincipleIDs...),
			Rerun:        append([]string(nil), item.Rerun...),
			Tags:         append([]string(nil), item.Tags...),
			Line:         item.Line,
			Column:       item.Column,
		})
	}

	return findings
}
