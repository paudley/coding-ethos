// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:mnd,lll // Parses external tool output and builds fixed command argv lists.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func runHadolint(_ Config, paths []string) int {
	files := toolchainFiles("hadolint", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"hadolint",
		repoRoot(),
		toolchainCommandWithFiles("hadolint", files),
		parseHadolintFindings,
	)
}

func runActionlint(_ Config, paths []string) int {
	if len(toolchainFiles("actionlint", existingFiles(paths))) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"actionlint",
		repoRoot(),
		toolchainCommand("actionlint"),
		parseActionlintFindings,
	)
}

func runPythonComplexity(_ Config, paths []string) int {
	files := formatPythonFiles(paths)
	if len(files) == 0 {
		return 0
	}

	return runPythonHookScript(
		"complexity",
		"check_complexity.py",
		files,
		parseComplexityFindings,
	)
}

func runPythonMaintainability(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	return runPythonHookScript(
		"maintainability",
		"check_maintainability.py",
		nil,
		parseMaintainabilityFindings,
	)
}

func runPythonVulture(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	return runPythonHookScript("vulture", "check_vulture.py", nil, parseVultureFindings)
}

func runGoFormatCheck(_ Config, paths []string) int {
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}

	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}

	result := runExternalTool(externalToolRequest{Name: "gofmt-check", Dir: worktree, Command: []string{"gofmt", "-l", "."}})
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

	return runHookTool("go-test", worktree, []string{"go", "test", "./..."})
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
		toolchainCommandWithRepoConfig("golangci-lint", repoPath(".golangci.yml")),
		parseGolangciFindings,
	)
}

func runPythonHookScript(
	name string,
	script string,
	files []string,
	parseFindings func(string) []hookFinding,
) int {
	command := make([]string, 0, len(files)+7)
	command = append(command,
		"uv", "run", "--quiet", "--project", hooksProjectPath(),
		"python", filepath.Join(hooksProjectPath(), script),
	)
	command = append(command, files...)

	return runHookToolWithParser(name, repoRoot(), command, parseFindings)
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
		fmt.Fprintf(os.Stderr, "FATAL: go.worktree is set to %q, but that directory does not exist\n", worktree)

		return "", false
	}

	return path, true
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
	complexityPattern  = regexp.MustCompile(`^\s*(.+?):(\d+)\s+(.+?)\s+\(complexity:\s*(\d+)\)$`)
	maintainPattern    = regexp.MustCompile(`^\s*(.+?)\s+\(MI:\s*([0-9.]+)\)$`)
	vultureLinePattern = regexp.MustCompile(`^(.+?):(\d+):\s*(.+)$`)
)

const (
	complexityMatchParts = 5
	maintainMatchParts   = 3
)

func parseHadolintFindings(output string) []hookFinding {
	return parseCatalogFindings("hadolint", output)
}

func parseActionlintFindings(output string) []hookFinding {
	return parseCatalogFindings("actionlint", output)
}

func parseGolangciFindings(output string) []hookFinding {
	return parseCatalogFindings("golangci-lint", output)
}

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
		finding.Code = "timeout"
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
