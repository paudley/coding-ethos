// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

var errCaptureToolPathRequired = errors.New("--tool-path is required with --capture-tool")

func runCapturedTool(
	tool string,
	toolPath string,
	cwd string,
	args []string,
) int {
	if strings.TrimSpace(toolPath) == "" {
		exitErr(errCaptureToolPathRequired)
	}

	command := exec.Command(toolPath, args...)
	if cwd != "" {
		command.Dir = cwd
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = io.MultiWriter(os.Stdout, &stdout)
	command.Stderr = io.MultiWriter(os.Stderr, &stderr)
	command.Stdin = os.Stdin

	err := command.Run()
	exitCode := capturedExitCode(err)
	logCapturedToolResult(tool, cwd, args, exitCode, stdout.String(), stderr.String())

	return exitCode
}

func logCapturedToolResult(
	tool string,
	cwd string,
	args []string,
	exitCode int,
	stdout string,
	stderr string,
) {
	parsed := diagnostics.Parse(tool, stdout, stderr)
	result := lint.Result{
		Scope:       "tool:" + tool,
		Status:      capturedStatus(exitCode),
		Diagnostics: parsed,
		Findings:    capturedFindings(tool, args, exitCode, parsed),
	}
	if _, err := lint.LogResult(cwd, result); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: lint trace not written: %v\n", err)
	}
}

func capturedFindings(
	tool string,
	args []string,
	exitCode int,
	items []diagnostics.Diagnostic,
) []lint.Finding {
	if len(items) == 0 {
		if exitCode == 0 {
			return nil
		}

		return []lint.Finding{{
			RawOutcome: map[string]any{
				"args":      append([]string(nil), args...),
				"exit_code": exitCode,
			},
			CheckID:    "tool." + tool,
			Message:    fmt.Sprintf("%s exited with status %d", tool, exitCode),
			Severity:   "error",
			SourceTool: tool,
			Status:     "fail",
			Blocking:   true,
		}}
	}

	findings := make([]lint.Finding, 0, len(items))
	for _, item := range items {
		findings = append(findings, lint.Finding{
			RawOutcome: map[string]any{
				"args":      append([]string(nil), args...),
				"exit_code": exitCode,
			},
			Advice:     item.Advice,
			CheckID:    firstCaptureNonEmpty(item.PolicyID, "tool."+tool),
			Code:       item.Code,
			File:       item.File,
			Message:    item.Message,
			PolicyID:   item.PolicyID,
			Severity:   firstCaptureNonEmpty(item.Severity, "error"),
			SourceTool: firstCaptureNonEmpty(item.Tool, tool),
			Status:     capturedStatus(exitCode),
			EthosIDs:   append([]string(nil), item.PrincipleIDs...),
			Blocking:   exitCode != 0,
			Column:     item.Column,
			Line:       item.Line,
		})
	}

	return findings
}

func capturedExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return 127
}

func capturedStatus(exitCode int) string {
	if exitCode == 0 {
		return "resolved"
	}

	return "blocked"
}

func firstCaptureNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
