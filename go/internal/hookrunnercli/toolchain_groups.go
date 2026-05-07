// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	complexityThreshold      = 15
	maintainabilityThreshold = 50
	timeoutCode              = "timeout"
	vultureMinConfidence     = 80
	gitShimProbeBytes        = 4096
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

	return runManagedPolicyTool("go-test", []string{worktree})
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

func runGolangciLint(_ Config, paths []string) int {
	if len(toolchainFiles("golangci-lint", existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktreeName()
	if !ok {
		return 1
	}

	return runManagedPolicyTool("golangci-lint", []string{worktree})
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

	policyContext, ok := managedPolicyContext(ethosRoot)
	if !ok {
		return 1
	}

	return managedcapture.Run(managedcapture.Options{
		PolicyContext: policyContext,
		Tool:          name,
		EthosRoot:     ethosRoot,
		ConsumerRoot:  consumer,
		InvocationCwd: repoRoot(),
		SandboxMode:   policyToolSandboxMode(),
		OutputFormat:  selectedHookOutputFormat(),
		Args:          args,
	})
}

func managedPolicyContext(ethosRoot string) (managedcapture.PolicyContext, bool) {
	file, ok := openPolicyBundleFile(policyBundlePath(ethosRoot))
	if !ok {
		return managedcapture.PolicyContext{}, false
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: decode policy bundle: %v\n", err)

		return managedcapture.PolicyContext{}, false
	}

	policies := make([]policy.Policy, 0, len(bundle.Policies))
	for _, item := range bundle.Policies {
		policies = append(policies, item)
	}

	return managedcapture.PolicyContext{
		Skills:       bundle.Skills,
		EvidenceMaps: bundle.EvidenceMaps,
		Policies:     policies,
	}, true
}

func policyBundlePath(ethosRoot string) string {
	return filepath.Join(ethosRoot, "build", "policy", "policy-bundle.json")
}

func openPolicyBundleFile(path string) (*os.File, bool) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: open policy bundle: %v\n", err)

		return nil, false
	}

	return file, true
}

func policyToolSandboxMode() string {
	if strings.TrimSpace(os.Getenv("CODING_ETHOS_POLICY_TOOL_SHIM")) == "" {
		return ""
	}

	return strings.TrimSpace(os.Getenv("CODING_ETHOS_SANDBOX_MODE"))
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
