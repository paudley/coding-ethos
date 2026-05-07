// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	diag "blackcat.ca/coding-ethos/go/diagnostics"
)

func runFormatGroupCommand(cfg Config, files []string) int {
	return runFormatGroup(cfg, files, false)
}

func runFormatGroup(cfg Config, files []string, restage bool) int {
	snapshots := map[string]fileSnapshot(nil)
	if restage {
		snapshots = fileSnapshots(files)
	}

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

	if restage && exit == 0 {
		changed := changedExistingFiles(files, snapshots)
		if len(changed) > 0 && restageFiles(changed) != 0 {
			exit = 1
		}
	}

	return exit
}

type fileSnapshot struct {
	hash  [sha256.Size]byte
	found bool
}

func fileSnapshots(files []string) map[string]fileSnapshot {
	snapshots := make(map[string]fileSnapshot, len(files))
	for _, file := range existingFiles(files) {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		snapshots[file] = fileSnapshot{
			hash:  sha256.Sum256(content),
			found: true,
		}
	}

	return snapshots
}

func changedExistingFiles(
	files []string,
	snapshots map[string]fileSnapshot,
) []string {
	changed := []string{}

	for _, file := range existingFiles(files) {
		before, ok := snapshots[file]
		if !ok || !before.found {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		if sha256.Sum256(content) != before.hash {
			changed = append(changed, file)
		}
	}

	return changed
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

	return runHookToolWithParser("ruff-autofix", repoRoot(), append([]string{
		"uv", "run", "--quiet", "--project", hooksProjectPath(), "ruff", "check",
		"--config", repoPath("ruff.toml"), "--fix", "--quiet", "--ignore-noqa",
		"--output-format", "json",
	}, pyFiles...), parseRuffAutofixFindings)
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
		return defaultPythonVersion
	}

	value, ok := rootConfigValue(rootConfig, "style.python_version")
	if !ok {
		return defaultPythonVersion
	}

	version := strings.TrimSpace(fmt.Sprint(value))
	if version == "" || version == nilString {
		return defaultPythonVersion
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

func runHookTool(name, dir string, command []string) int {
	return runHookToolWithParser(name, dir, command, nil)
}

func runHookToolWithParser(
	name string,
	dir string,
	command []string,
	parseFindings func(string) []hookFinding,
) int {
	result := runExternalTool(
		externalToolRequest{Name: name, Dir: dir, Command: command},
	)

	return reportSharedToolResult(
		name,
		result,
		parseFindings,
		[]string{"Fix the reported diagnostics before committing."},
	)
}

func reportSharedToolResult(
	name string,
	result externalToolResult,
	parseFindings func(string) []hookFinding,
	guidance []string,
) int {
	if result.RunnerFailure != nil {
		fmt.Fprintln(os.Stdout, formatSharedToolRunnerFailure(name, result))

		return 1
	}

	if result.ExitCode != 0 {
		rawOutput := []string(nil)

		var findings []hookFinding
		if parseFindings != nil {
			findings = parseFindings(result.Combined)
		} else {
			findings = parseGenericHookFindings(name, result.Combined)
		}

		if len(findings) == 0 {
			findings = []hookFinding{genericToolFailureFindingForResult(name, result)}
			rawOutput = boundedRawOutputLines(result.Combined)
		}

		fmt.Fprintln(os.Stdout, formatHookReport(hookReport{
			Tool:      name,
			Title:     strings.ToUpper(name) + " FAILED",
			Findings:  findings,
			RawOutput: rawOutput,
			Guidance:  guidance,
		}, selectedHookOutputFormat()))
	}

	return result.ExitCode
}

func formatSharedToolRunnerFailure(name string, result externalToolResult) string {
	message := ""
	if result.RunnerFailure != nil {
		message = result.RunnerFailure.Error()
	}

	return formatHookReport(hookReport{
		Tool:  name,
		Title: strings.ToUpper(name) + " RUNNER FAILED",
		Findings: []hookFinding{{
			Tool:     name,
			Severity: "fatal",
			Message:  message,
		}},
		Guidance: []string{
			"Install the required tool or fix the hook runner configuration.",
		},
	}, selectedHookOutputFormat())
}

func genericToolFailureFinding(name string, exitCode int) hookFinding {
	return hookFinding{
		Tool:     name,
		Severity: "error",
		Message:  "external tool exited with status " + strconv.Itoa(exitCode),
	}
}

func genericToolFailureFindingForResult(
	name string,
	result externalToolResult,
) hookFinding {
	if result.TimedOut {
		return hookFinding{
			Tool:     name,
			Severity: "error",
			Code:     timeoutCode,
			Message:  "external tool timed out",
			Detail:   "timeout exceeded before the tool completed",
		}
	}

	return genericToolFailureFinding(name, result.ExitCode)
}

func parseGenericHookFindings(name, output string) []hookFinding {
	return diagnosticsToHookFindings(diag.Parse(name, output, ""))
}

func boundedRawOutputLines(output string) []string {
	const (
		maxRawOutputLines      = 20
		maxRawOutputLineLength = 500
	)

	output = strings.TrimSpace(output)
	if output == "" {
		return nil
	}

	lines := strings.Split(output, "\n")
	limit := len(lines)
	truncated := false

	if limit > maxRawOutputLines {
		limit = maxRawOutputLines
		truncated = true
	}

	bounded := make([]string, 0, limit+1)
	for _, line := range lines[:limit] {
		line = strings.TrimRight(line, "\r")
		if len(line) > maxRawOutputLineLength {
			line = line[:maxRawOutputLineLength] + " [truncated]"
		}

		bounded = append(bounded, line)
	}

	if truncated {
		bounded = append(bounded, fmt.Sprintf(
			"[truncated: %d additional lines]",
			len(lines)-limit,
		))
	}

	return bounded
}

func parseRuffAutofixFindings(output string) []hookFinding {
	return parseCatalogFindings("ruff", output)
}
