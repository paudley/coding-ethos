// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func runHadolint(_ Config, paths []string) int {
	files := dockerFiles(existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"hadolint",
		repoRoot(),
		append([]string{"hadolint"}, files...),
		parseHadolintFindings,
	)
}

func runActionlint(_ Config, paths []string) int {
	if len(workflowFiles(existingFiles(paths))) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"actionlint",
		repoRoot(),
		[]string{"actionlint"},
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
		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:  "gofmt-check",
			Title: "GOFMT CHECK FAILED",
			Findings: []hookFinding{{
				Tool:     "gofmt-check",
				Severity: "error",
				Message:  "Go files are not gofmt-formatted.",
				Detail:   truncateHookDetail(result.Combined),
			}},
			Guidance: []string{"Run gofmt on the listed files before pushing."},
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
	if len(goFiles(existingFiles(paths))) == 0 {
		return 0
	}
	worktree, ok := configuredGoWorktree()
	if !ok {
		return 1
	}
	config := repoPath(".golangci.yml")

	return runHookToolWithParser(
		"golangci-lint",
		worktree,
		[]string{"golangci-lint", "run", "--config", config},
		parseGolangciFindings,
	)
}

func runPythonHookScript(
	name string,
	script string,
	files []string,
	parseFindings func(string) []hookFinding,
) int {
	command := []string{
		"uv", "run", "--quiet", "--project", hooksProjectPath(),
		"python", filepath.Join(hooksProjectPath(), script),
	}
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
	if worktree == "" || worktree == "<nil>" {
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
		if ext == ".yaml" || ext == ".yml" {
			files = append(files, path)
		}
	}

	return files
}

var (
	hadolintPattern    = regexp.MustCompile(`^(.+?):(\d+)\s+([A-Z]+\d+)\s+([^:]+):\s*(.+)$`)
	actionlintPattern  = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\[([^\]]+)])?$`)
	golangciPattern    = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\(([^)]+)\))?$`)
	complexityPattern  = regexp.MustCompile(`^\s*(.+?):(\d+)\s+(.+?)\s+\(complexity:\s*(\d+)\)$`)
	maintainPattern    = regexp.MustCompile(`^\s*(.+?)\s+\(MI:\s*([0-9.]+)\)$`)
	vultureLinePattern = regexp.MustCompile(`^(.+?):(\d+):\s*(.+)$`)
)

func parseHadolintFindings(output string) []hookFinding {
	findings := []hookFinding{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		matches := hadolintPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 6 {
			continue
		}
		lineNo, _ := strconv.Atoi(matches[2])
		findings = append(findings, hookFinding{
			Tool:     "hadolint",
			File:     matches[1],
			Line:     lineNo,
			Severity: strings.TrimSpace(matches[4]),
			Code:     matches[3],
			Message:  strings.TrimSpace(matches[5]),
		})
	}

	return findings
}

func parseActionlintFindings(output string) []hookFinding {
	findings := []hookFinding{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		matches := actionlintPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 6 {
			continue
		}
		lineNo, _ := strconv.Atoi(matches[2])
		column, _ := strconv.Atoi(matches[3])
		findings = append(findings, hookFinding{
			Tool:     "actionlint",
			File:     matches[1],
			Line:     lineNo,
			Column:   column,
			Severity: "error",
			Code:     matches[5],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return findings
}

func parseGolangciFindings(output string) []hookFinding {
	findings := []hookFinding{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		matches := golangciPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 6 {
			continue
		}
		lineNo, _ := strconv.Atoi(matches[2])
		column, _ := strconv.Atoi(matches[3])
		findings = append(findings, hookFinding{
			Tool:     "golangci-lint",
			File:     matches[1],
			Line:     lineNo,
			Column:   column,
			Severity: "error",
			Code:     matches[5],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return findings
}

func parseComplexityFindings(output string) []hookFinding {
	findings := []hookFinding{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		matches := complexityPattern.FindStringSubmatch(line)
		if len(matches) != 5 {
			continue
		}
		lineNo, _ := strconv.Atoi(matches[2])
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
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		matches := maintainPattern.FindStringSubmatch(line)
		if len(matches) != 3 {
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

func parseVultureFindings(output string) []hookFinding {
	findings := []hookFinding{}
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		matches := vultureLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != 4 || strings.HasPrefix(matches[1], "=") {
			continue
		}
		lineNo, _ := strconv.Atoi(matches[2])
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
