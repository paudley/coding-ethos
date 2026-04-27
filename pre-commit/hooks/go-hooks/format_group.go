// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func runFormatGroupCommand(cfg Config, files []string) int {
	return runFormatGroup(cfg, files, false)
}

func runFormatGroup(cfg Config, files []string, restage bool) int {
	exit := fixText(cfg, files)
	if runPyupgrade(files) != 0 {
		exit = 1
	}
	if runRuffFormat(files) != 0 {
		exit = 1
	}
	if runRuffAutofix(files) != 0 {
		exit = 1
	}
	if runGofmtWrite(files) != 0 {
		exit = 1
	}
	if restage && exit == 0 && restageFiles(files) != 0 {
		exit = 1
	}

	return exit
}

func runPyupgrade(files []string) int {
	pyFiles := formatPythonFiles(files)
	if len(pyFiles) == 0 {
		return 0
	}
	version := configuredPythonVersion()
	target := "--py" + strings.ReplaceAll(version, ".", "") + "-plus"

	return runHookTool("pyupgrade", repoRoot(), append([]string{
		"uv", "run", "--quiet", "--project", hooksProjectPath(), "pyupgrade", target,
	}, pyFiles...))
}

func runRuffFormat(files []string) int {
	pyFiles := formatPythonFiles(files)
	if len(pyFiles) == 0 {
		return 0
	}

	return runHookTool("ruff-format", repoRoot(), append([]string{
		"uv", "run", "--quiet", "--project", hooksProjectPath(), "ruff", "format",
		"--config", repoPath("ruff.toml"), "--quiet",
	}, pyFiles...))
}

func runRuffAutofix(files []string) int {
	pyFiles := formatPythonFiles(files)
	if len(pyFiles) == 0 {
		return 0
	}

	return runHookTool("ruff-autofix", repoRoot(), append([]string{
		"uv", "run", "--quiet", "--project", hooksProjectPath(), "ruff", "check",
		"--config", repoPath("ruff.toml"), "--fix", "--quiet", "--ignore-noqa",
	}, pyFiles...))
}

func runGofmtWrite(files []string) int {
	goFiles := goFiles(existingFiles(files))
	if len(goFiles) == 0 {
		return 0
	}

	return runHookTool("gofmt", repoRoot(), append([]string{"gofmt", "-w"}, goFiles...))
}

func formatPythonFiles(files []string) []string {
	selected := []string{}
	for _, path := range existingFiles(files) {
		if !strings.HasSuffix(path, ".py") && !strings.HasSuffix(path, ".pyi") {
			continue
		}
		if strings.HasPrefix(path, ".venv/") ||
			strings.Contains(path, "/.venv/") ||
			strings.HasSuffix(path, "/vulture_whitelist.py") ||
			path == "vulture_whitelist.py" {
			continue
		}
		selected = append(selected, path)
	}

	return selected
}

func configuredPythonVersion() string {
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return "3.13"
	}
	value, ok := rootConfigValue(rootConfig, "style.python_version")
	if !ok {
		return "3.13"
	}
	version := strings.TrimSpace(fmt.Sprint(value))
	if version == "" || version == "<nil>" {
		return "3.13"
	}

	return version
}

func hooksProjectPath() string {
	bundleRoot, err := findBundleRoot()
	if err != nil {
		return filepath.Join("pre-commit", "hooks")
	}

	return filepath.Join(bundleRoot, "hooks")
}

func runHookTool(name string, dir string, command []string) int {
	result := runExternalTool(externalToolRequest{Name: name, Dir: dir, Command: command})
	if result.RunnerFailure != nil {
		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:  name,
			Title: strings.ToUpper(name) + " RUNNER FAILED",
			Findings: []hookFinding{{
				Tool:     name,
				Severity: "fatal",
				Message:  result.RunnerFailure.Error(),
			}},
			Guidance: []string{"Install the required tool or fix the hook runner configuration."},
		}, selectedHookOutputFormat()))

		return 1
	}
	if result.ExitCode != 0 {
		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:  name,
			Title: strings.ToUpper(name) + " FAILED",
			Findings: []hookFinding{{
				Tool:     name,
				Severity: "error",
				Message:  "external tool exited with status " + fmt.Sprint(result.ExitCode),
				Detail:   truncateHookDetail(result.Combined),
			}},
			Guidance: []string{"Fix the reported diagnostics before committing."},
		}, selectedHookOutputFormat()))
	}

	return result.ExitCode
}

func truncateHookDetail(value string) string {
	const maxDetailLength = 4000
	value = strings.TrimSpace(value)
	if len(value) <= maxDetailLength {
		return value
	}

	return value[:maxDetailLength] + "\n[truncated]"
}
