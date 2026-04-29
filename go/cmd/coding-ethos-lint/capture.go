// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
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

	runArgs := capturedToolArgs(tool, args)
	command := exec.Command(toolPath, runArgs...)
	if cwd != "" {
		command.Dir = cwd
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Stdin = os.Stdin

	err := command.Run()
	exitCode := capturedExitCode(err)
	stdoutText := stdout.String()
	stderrText := stderr.String()
	result := capturedToolResult(tool, args, runArgs, exitCode, stdoutText, stderrText)
	logCapturedToolResult(cwd, result)
	if result.Blocked() || len(result.Diagnostics) > 0 {
		if encodeErr := hookoutput.EncodeLintResult(
			os.Stdout,
			result,
			hookoutput.SelectedFormat(),
		); encodeErr != nil {
			fmt.Fprintf(os.Stderr, "WARN: lint result not rendered: %v\n", encodeErr)
		}
	}

	return exitCode
}

func logCapturedToolResult(
	cwd string,
	result lint.Result,
) {
	if _, err := lint.LogResult(cwd, result); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: lint trace not written: %v\n", err)
	}
}

func capturedToolResult(
	tool string,
	args []string,
	runArgs []string,
	exitCode int,
	stdout string,
	stderr string,
) lint.Result {
	parsed := diagnostics.Parse(tool, stdout, stderr)

	return lint.Result{
		Scope:       "tool:" + tool,
		Status:      capturedStatus(exitCode),
		Diagnostics: parsed,
		Findings:    capturedFindings(tool, args, runArgs, exitCode, parsed),
	}
}

func capturedFindings(
	tool string,
	args []string,
	runArgs []string,
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
				"run_args":  append([]string(nil), runArgs...),
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
				"run_args":  append([]string(nil), runArgs...),
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

func capturedToolArgs(tool string, args []string) []string {
	parseableArgs, ok := parseableCaptureArgs(tool, args)
	if !ok {
		return append([]string(nil), args...)
	}

	return parseableArgs
}

func parseableCaptureArgs(tool string, args []string) ([]string, bool) {
	if captureArgsMutate(tool, args) {
		return nil, false
	}
	args = stripCaptureOutputArgs(tool, args)

	switch tool {
	case "ruff":
		return ruffCaptureArgs(args), true
	case "pyright":
		return prependCopy(args, "--outputjson"), true
	case "mypy":
		return prependCopy(args, "--output=json"), true
	case "pylint":
		return prependCopy(args, "--output-format=json"), true
	case "shellcheck":
		return prependCopy(args, "--format=json"), true
	case "yamllint":
		return prependCopy(args, "-f", "parsable"), true
	case "hadolint":
		return prependCopy(args, "--format", "json"), true
	case "actionlint":
		return prependCopy(args, "-format", "{{json .}}"), true
	case "golangci-lint":
		return golangciCaptureArgs(args), true
	default:
		return nil, false
	}
}

func stripCaptureOutputArgs(tool string, args []string) []string {
	switch tool {
	case "ruff":
		return stripArgsWithValues(args, "--output-format")
	case "pyright":
		return stripArgs(args, "--outputjson")
	case "mypy":
		return stripArgsWithValues(args, "--output", "-O")
	case "pylint":
		return stripArgsWithValues(args, "--output-format", "-f")
	case "shellcheck":
		return stripArgsWithValues(args, "--format", "-f")
	case "yamllint":
		return stripArgsWithValues(args, "--format", "-f")
	case "hadolint":
		return stripArgsWithValues(args, "--format", "-f")
	case "actionlint":
		return stripArgsWithValues(args, "-format")
	case "golangci-lint":
		return stripArgsWithValues(
			args,
			"--out-format",
			"--output.json.path",
			"--output.text.path",
		)
	default:
		return append([]string(nil), args...)
	}
}

func stripArgs(args []string, flags ...string) []string {
	stripped := []string{}
	for _, arg := range args {
		matchedFlag := false
		for _, flag := range flags {
			if arg == flag {
				matchedFlag = true

				break
			}
		}
		if !matchedFlag {
			stripped = append(stripped, arg)
		}
	}

	return stripped
}

func stripArgsWithValues(args []string, flags ...string) []string {
	stripped := []string{}
	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		matchedFlag := false
		for _, flag := range flags {
			if arg == flag {
				matchedFlag = true
				skipNext = true

				break
			}
			if strings.HasPrefix(arg, flag+"=") {
				matchedFlag = true

				break
			}
		}
		if matchedFlag {
			continue
		}

		stripped = append(stripped, arg)
	}

	return stripped
}

func ruffCaptureArgs(args []string) []string {
	if len(args) > 0 && args[0] == "check" {
		return appendCopy(
			[]string{"check", "--output-format=json"},
			args[1:]...,
		)
	}

	return prependCopy(args, "--output-format=json")
}

func golangciCaptureArgs(args []string) []string {
	if len(args) > 0 && args[0] == "run" {
		return appendCopy(
			[]string{
				"run",
				"--output.json.path=stdout",
				"--output.text.path=stderr",
			},
			args[1:]...,
		)
	}

	return prependCopy(
		args,
		"--output.json.path=stdout",
		"--output.text.path=stderr",
	)
}

func prependCopy(args []string, extra ...string) []string {
	copied := append([]string(nil), extra...)

	return append(copied, args...)
}

func captureArgsMutate(tool string, args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--fix", "--fix-only", "--unsafe-fixes", "-w", "--write",
			"-init-config":
			return true
		}
	}

	return tool == "ruff" && len(args) > 0 && args[0] == "format"
}

func appendCopy(args []string, extra ...string) []string {
	copied := append([]string(nil), args...)

	return append(copied, extra...)
}
