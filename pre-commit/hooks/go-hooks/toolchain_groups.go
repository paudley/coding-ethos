// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runHadolint(_ Config, paths []string) int {
	files := dockerFiles(existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runHookTool("hadolint", repoRoot(), append([]string{"hadolint"}, files...))
}

func runActionlint(_ Config, paths []string) int {
	if len(workflowFiles(existingFiles(paths))) == 0 {
		return 0
	}

	return runHookTool("actionlint", repoRoot(), []string{"actionlint"})
}

func runPythonComplexity(_ Config, paths []string) int {
	files := formatPythonFiles(paths)
	if len(files) == 0 {
		return 0
	}

	return runPythonHookScript("complexity", "check_complexity.py", files)
}

func runPythonMaintainability(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	return runPythonHookScript("maintainability", "check_maintainability.py", nil)
}

func runPythonVulture(_ Config, paths []string) int {
	if len(formatPythonFiles(paths)) == 0 {
		return 0
	}

	return runPythonHookScript("vulture", "check_vulture.py", nil)
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

	return runHookTool("golangci-lint", worktree, []string{"golangci-lint", "run", "--config", config})
}

func runPythonHookScript(name string, script string, files []string) int {
	command := []string{
		"uv", "run", "--quiet", "--project", hooksProjectPath(),
		"python", filepath.Join(hooksProjectPath(), script),
	}
	command = append(command, files...)

	return runHookTool(name, repoRoot(), command)
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
