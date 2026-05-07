// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:lll // Parses external tool output and builds fixed command argv lists.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const (
	complexityThreshold      = 15
	maintainabilityThreshold = 50
	radonExcludePattern      = ".venv/*,node_modules/*"
	timeoutCode              = "timeout"
	vultureMinConfidence     = 80
	vultureTimeoutSeconds    = 120
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

	return runHookToolWithParser(
		name,
		repoRoot(),
		toolchainCommandForFiles(name, files),
		func(output string) []hookFinding { return parseCatalogFindings(name, output) },
	)
}

func runPythonComplexity(_ Config, paths []string) int {
	files := formatPythonFiles(paths)
	if len(files) == 0 {
		return 0
	}

	command := append(radonCommand("cc"), files...)
	command = append(command, "-j", "-e", radonExcludePattern)

	result := runExternalTool(externalToolRequest{
		Name:    "complexity",
		Dir:     repoRoot(),
		Command: command,
	})
	if result.RunnerFailure != nil || result.ExitCode != 0 {
		return reportExternalQualityFailure(
			"complexity",
			result,
			parseComplexityFindings,
		)
	}

	findings := parseRadonComplexityFindings(result.Combined, complexityThreshold)
	if len(findings) == 0 {
		return 0
	}

	fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
		Tool:     "complexity",
		Title:    "COMPLEXITY FAILED",
		Findings: findings,
		Guidance: []string{
			"Extract helper functions, use early returns, or split branches before committing.",
		},
	}, selectedHookOutputFormat()))

	return 1
}

func runPythonMaintainability(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	result := runExternalTool(externalToolRequest{
		Name: "maintainability",
		Dir:  repoRoot(),
		Command: append(
			radonCommand("mi"),
			".",
			"-j",
			"-e",
			radonExcludePattern,
		),
	})
	if result.RunnerFailure != nil || result.ExitCode != 0 {
		return reportExternalQualityFailure(
			"maintainability",
			result,
			parseMaintainabilityFindings,
		)
	}

	// Advisory mode: parse the JSON so the check stays exercised, but keep
	// success silent until maintainability becomes a blocking gate.
	_ = parseRadonMaintainabilityFindings(result.Combined, maintainabilityThreshold)

	return 0
}

func runPythonVulture(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	command := append(
		[]string{
			"uv",
			"run",
			"--quiet",
			"--project",
			hooksProjectPath(),
			"vulture",
			".",
		},
		vultureWhitelistArgs()...,
	)
	command = append(
		command,
		"--min-confidence",
		strconv.Itoa(vultureMinConfidence),
		"--exclude",
		strings.Join(vultureExcludePatterns(), ","),
	)

	result := runExternalTool(externalToolRequest{
		Name:           "vulture",
		Dir:            repoRoot(),
		Command:        command,
		TimeoutSeconds: vultureTimeoutSeconds,
	})
	if result.ExitCode == 0 && result.RunnerFailure == nil {
		return 0
	}

	return reportExternalQualityFailure("vulture", result, parseVultureFindings)
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
		rawOutput := []string(nil)

		if len(findings) == 0 {
			findings = []hookFinding{{
				Tool:     "gofmt-check",
				Severity: "error",
				Message:  "Go files are not gofmt-formatted.",
			}}
			rawOutput = boundedRawOutputLines(result.Combined)
		}

		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:      "gofmt-check",
			Title:     "GOFMT CHECK FAILED",
			Findings:  findings,
			RawOutput: rawOutput,
			Guidance:  []string{"Run gofmt on the listed files before pushing."},
		}, selectedHookOutputFormat()))

		return 1
	}

	if result.RunnerFailure != nil {
		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:  "gofmt-check",
			Title: "GOFMT CHECK RUNNER FAILED",
			Findings: []hookFinding{{
				Tool:     "gofmt-check",
				Severity: "fatal",
				Message:  result.RunnerFailure.Error(),
			}},
			Guidance: []string{"Install gofmt or fix the Go toolchain configuration."},
		}, selectedHookOutputFormat()))

		return 1
	}

	return result.ExitCode
}

func parseGofmtCheckFindings(output string) []hookFinding {
	files := strings.Fields(strings.TrimSpace(output))
	if len(files) == 0 {
		return nil
	}

	findings := make([]hookFinding, 0, len(files))
	for _, file := range files {
		findings = append(findings, hookFinding{
			Tool:     "gofmt-check",
			File:     file,
			Severity: "error",
			Message:  "Go file is not gofmt-formatted.",
		})
	}

	return findings
}

func runGoVet(_ Config, paths []string) int {
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}

	return runHookTool("go-vet", worktree, []string{"go", "vet", "./..."})
}

func runGoTests(_ Config, paths []string) int {
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}

	result := runExternalTool(externalToolRequest{
		Name: "go-test",
		Dir:  worktree,
		Command: []string{
			"go",
			"test",
			"-json",
			"-buildvcs=false",
			"-timeout=30s",
			"-short",
			"./...",
		},
		Env: hookTestToolEnv(),
	})

	return reportSharedToolResult(
		"go-test",
		result,
		nil,
		[]string{"Fix the reported diagnostics before committing."},
	)
}

func hookTestToolEnv() []string {
	return []string{
		"PATH=" + pathWithoutHookGitShims(
			os.Getenv("PATH"),
			os.Getenv("CODING_ETHOS_REAL_GIT"),
		),
	}
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

	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}

	return runHookToolWithParser(
		"golangci-lint",
		worktree,
		toolchainCommandWithRepoConfig(
			"golangci-lint",
			repoPath(toolchainRepoConfig("golangci-lint")),
		),
		func(output string) []hookFinding { return parseCatalogFindings("golangci-lint", output) },
	)
}

func configuredGoWorktree() (string, bool) {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return "", false
	}

	raw, ok := rootConfigValue(rootConfig, "go.worktree")

	worktree := "go"
	if ok {
		worktree = strings.TrimSpace(fmt.Sprint(raw))
	}

	if worktree == "" || worktree == nilString {
		worktree = "go"
	}

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

func radonCommand(command string) []string {
	return []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		hooksProjectPath(),
		"radon",
		command,
	}
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

func reportExternalQualityFailure(
	name string,
	result externalToolResult,
	parseFindings func(string) []hookFinding,
) int {
	if result.RunnerFailure != nil {
		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:  name,
			Title: strings.ToUpper(name) + " RUNNER FAILED",
			Findings: []hookFinding{{
				Tool:     name,
				Severity: "fatal",
				Message:  result.RunnerFailure.Error(),
			}},
			Guidance: []string{
				"Install the required tool or fix the hook runner configuration.",
			},
		}, selectedHookOutputFormat()))

		return 1
	}

	findings := parseFindings(result.Combined)
	rawOutput := []string(nil)

	if len(findings) == 0 {
		findings = []hookFinding{genericToolFailureFindingForResult(name, result)}
		rawOutput = boundedRawOutputLines(result.Combined)
	}

	fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
		Tool:      name,
		Title:     strings.ToUpper(name) + " FAILED",
		Findings:  findings,
		RawOutput: rawOutput,
		Guidance:  []string{"Fix the reported diagnostics before committing."},
	}, selectedHookOutputFormat()))

	return 1
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
	maintainPattern    = regexp.MustCompile(`^\s*(.+?)\s+\(MI:\s*([0-9.]+)\)$`)
	vultureLinePattern = regexp.MustCompile(`^(.+?):(\d+):\s*(.+)$`)
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

type radonComplexityItem struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Rank       string `json:"rank"`
	LineNo     int    `json:"lineno"`
	Complexity int    `json:"complexity"`
}

func parseRadonComplexityFindings(output string, threshold int) []hookFinding {
	results := map[string][]radonComplexityItem{}

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &results)
	if err != nil {
		return parseComplexityFindings(output)
	}

	findings := []hookFinding{}

	for file, items := range results {
		for _, item := range items {
			if item.Complexity <= threshold {
				continue
			}

			findings = append(findings, hookFinding{
				Tool:     "complexity",
				File:     file,
				Line:     item.LineNo,
				Severity: "error",
				Code:     "cyclomatic-complexity",
				Message:  item.Name,
				Detail:   fmt.Sprintf("complexity: %d", item.Complexity),
				Metadata: map[string]any{
					"rank":      item.Rank,
					"type":      item.Type,
					"threshold": threshold,
				},
			})
		}
	}

	return findings
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

type radonMaintainabilityItem struct {
	Rank string  `json:"rank"`
	MI   float64 `json:"mi"`
}

func parseRadonMaintainabilityFindings(output string, threshold int) []hookFinding {
	results := map[string]radonMaintainabilityItem{}

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &results)
	if err != nil {
		return parseMaintainabilityFindings(output)
	}

	findings := []hookFinding{}

	for file, item := range results {
		if item.MI >= float64(threshold) {
			continue
		}

		findings = append(findings, hookFinding{
			Tool:     "maintainability",
			File:     file,
			Severity: "warning",
			Code:     "maintainability-index",
			Message:  "Maintainability index below configured threshold.",
			Detail:   fmt.Sprintf("MI: %.2f", item.MI),
			Metadata: map[string]any{
				"rank":      item.Rank,
				"threshold": threshold,
			},
		})
	}

	return findings
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
		Advice:   "Run the maintainability check directly, then simplify or split the slow module before committing.",
	}
	if strings.Contains(strings.ToLower(message), "timed out") {
		finding.Code = timeoutCode
	}

	return finding, true
}

func parseVultureFindings(output string) []hookFinding {
	findings := []hookFinding{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := vultureLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 4 || strings.HasPrefix(matches[1], "=") {
			continue
		}

		lineNo, ok := parseDiagnosticInt(matches[2])
		if !ok {
			continue
		}

		findings = append(findings, hookFinding{
			Tool:     "vulture",
			File:     matches[1],
			Line:     lineNo,
			Severity: "warning",
			Code:     "unused-code",
			Message:  strings.TrimSpace(matches[3]),
		})
	}

	return findings
}
