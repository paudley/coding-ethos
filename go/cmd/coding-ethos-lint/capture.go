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
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var errCaptureToolPathRequired = errors.New("--tool-path is required with --capture-tool")

func runCapturedTool(
	tool string,
	toolPath string,
	cwd string,
	args []string,
	evidenceMaps []diagnostics.EvidenceMap,
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
	result := capturedToolResult(
		tool,
		args,
		runArgs,
		exitCode,
		cwd,
		stdoutText,
		stderrText,
		evidenceMaps,
	)
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
	cwd string,
	stdout string,
	stderr string,
	evidenceMaps []diagnostics.EvidenceMap,
) lint.Result {
	parsed := diagnostics.Parse(tool, stdout, stderr)
	parsed = diagnostics.Enrich(parsed, evidenceMaps)
	parsed = diagnostics.Dedupe(parsed)

	return lint.Result{
		Scope:       "tool:" + tool,
		Status:      capturedStatus(exitCode),
		Diagnostics: parsed,
		Findings:    capturedFindings(tool, args, runArgs, exitCode, cwd, stdout, stderr, parsed),
	}
}

func capturedFindings(
	tool string,
	args []string,
	runArgs []string,
	exitCode int,
	cwd string,
	stdout string,
	stderr string,
	items []diagnostics.Diagnostic,
) []lint.Finding {
	outcome := capturedOutcome(tool, exitCode, items)
	if len(items) == 0 {
		if exitCode == 0 {
			return nil
		}

		return []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  outcome.Category,
				"args":      append([]string(nil), args...),
				"exit_code": exitCode,
				"run_args":  append([]string(nil), runArgs...),
				"output":    capturedOutputExcerpt(stdout, stderr, cwd),
			},
			CheckID:    "tool." + tool,
			Message:    outcome.Message,
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
				"category":  outcome.Category,
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

const maxCapturedOutputExcerpt = 600

func capturedOutputExcerpt(stdout string, stderr string, cwd string) string {
	output := strings.TrimSpace(firstCaptureNonEmpty(stderr, stdout))
	if output == "" {
		return ""
	}

	output = redactCapturedOutputPaths(output, cwd)
	output = strings.Join(strings.Fields(output), " ")
	if len(output) <= maxCapturedOutputExcerpt {
		return output
	}

	return output[:maxCapturedOutputExcerpt] + "..."
}

func redactCapturedOutputPaths(output string, cwd string) string {
	redacted := output
	replacements := map[string]string{}
	if cwd = strings.TrimSpace(cwd); cwd != "" {
		replacements[cwd] = "<repo>"
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		replacements[home] = "<home>"
	}

	for path, marker := range replacements {
		redacted = strings.ReplaceAll(redacted, path, marker)
	}

	return redacted
}

type capturedOutcomeClass struct {
	Category string
	Message  string
}

func capturedOutcome(
	tool string,
	exitCode int,
	items []diagnostics.Diagnostic,
) capturedOutcomeClass {
	if len(items) > 0 {
		return capturedOutcomeClass{
			Category: "lint_findings",
			Message:  fmt.Sprintf("%s reported diagnostics", tool),
		}
	}
	if exitCode == 0 {
		return capturedOutcomeClass{Category: "success", Message: tool + " passed"}
	}

	switch exitCode {
	case 1:
		return capturedOutcomeClass{
			Category: "tool_error",
			Message:  fmt.Sprintf("%s exited with status %d without parseable diagnostics", tool, exitCode),
		}
	case 2:
		return capturedOutcomeClass{
			Category: "configuration_error",
			Message:  fmt.Sprintf("%s configuration or usage failed with status %d", tool, exitCode),
		}
	default:
		return capturedOutcomeClass{
			Category: "tool_error",
			Message:  fmt.Sprintf("%s exited with status %d without parseable diagnostics", tool, exitCode),
		}
	}
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
	metadata, found := toolcatalog.HookOwnedTool(tool)
	if !found {
		return append([]string(nil), args...)
	}

	parseableArgs, ok := metadata.CaptureArgs(args)
	if !ok {
		return append([]string(nil), args...)
	}

	return parseableArgs
}
