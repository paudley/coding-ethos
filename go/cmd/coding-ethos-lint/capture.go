// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	traceRoot string,
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
		traceRoot,
		stdoutText,
		stderrText,
		evidenceMaps,
	)
	logCapturedToolResult(firstCaptureNonEmpty(traceRoot, cwd), result)
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
	traceRoot string,
	stdout string,
	stderr string,
	evidenceMaps []diagnostics.EvidenceMap,
) lint.Result {
	parsed := diagnostics.Parse(tool, stdout, stderr)
	parsed = normalizeCapturedDiagnosticPaths(parsed, traceRoot)
	parsed = diagnostics.Enrich(parsed, evidenceMaps)
	parsed = diagnostics.Dedupe(parsed)
	outputExcerpt := capturedOutputExcerpt(stdout, stderr, firstCaptureNonEmpty(traceRoot, cwd), cwd)

	return lint.Result{
		Scope:       "tool:" + tool,
		Status:      capturedStatus(exitCode),
		Capture:     capturedToolMetadata(tool, args, runArgs, exitCode, outputExcerpt, parsed),
		Diagnostics: parsed,
		Findings:    capturedFindings(tool, args, runArgs, exitCode, outputExcerpt, parsed),
	}
}

func capturedFindings(
	tool string,
	args []string,
	runArgs []string,
	exitCode int,
	outputExcerpt string,
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
				"output":    outputExcerpt,
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

func capturedToolMetadata(
	tool string,
	args []string,
	runArgs []string,
	exitCode int,
	outputExcerpt string,
	items []diagnostics.Diagnostic,
) *lint.ToolCapture {
	return &lint.ToolCapture{
		Tool:          tool,
		Parser:        tool,
		ParseStatus:   capturedParseStatus(exitCode, items),
		OutputExcerpt: outputExcerpt,
		Args:          append([]string(nil), args...),
		RunArgs:       append([]string(nil), runArgs...),
		ExitCode:      exitCode,
	}
}

func capturedParseStatus(exitCode int, items []diagnostics.Diagnostic) string {
	if len(items) > 0 {
		return "parsed"
	}
	if exitCode == 0 {
		return "empty"
	}
	if exitCode == 2 {
		return "tool_config_error"
	}

	return "parse_error"
}

const maxCapturedOutputExcerpt = 600

func normalizeCapturedDiagnosticPaths(
	items []diagnostics.Diagnostic,
	traceRoot string,
) []diagnostics.Diagnostic {
	traceRoot = strings.TrimSpace(traceRoot)
	if traceRoot == "" {
		return items
	}

	absRoot, err := filepath.Abs(traceRoot)
	if err != nil {
		return items
	}

	out := append([]diagnostics.Diagnostic(nil), items...)
	for index := range out {
		file := strings.TrimSpace(out[index].File)
		if file == "" || !filepath.IsAbs(file) {
			continue
		}

		rel, err := filepath.Rel(absRoot, file)
		if err != nil || rel == "." || rel == "" || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
			continue
		}

		out[index].File = filepath.ToSlash(rel)
		out[index].Message = redactCapturedOutputPaths(out[index].Message, absRoot, "")
		out[index].Advice = redactCapturedOutputPaths(out[index].Advice, absRoot, "")
		out[index].Detail = redactCapturedOutputPaths(out[index].Detail, absRoot, "")
	}

	return out
}

func capturedOutputExcerpt(stdout string, stderr string, repoRoot string, toolRoot string) string {
	output := strings.TrimSpace(firstCaptureNonEmpty(stderr, stdout))
	if output == "" {
		return ""
	}

	output = redactCapturedOutputPaths(output, repoRoot, toolRoot)
	output = strings.Join(strings.Fields(output), " ")
	if len(output) <= maxCapturedOutputExcerpt {
		return output
	}

	return output[:maxCapturedOutputExcerpt] + "..."
}

func redactCapturedOutputPaths(output string, repoRoot string, toolRoot string) string {
	redacted := output
	replacements := map[string]string{}
	if repoRoot = strings.TrimSpace(repoRoot); repoRoot != "" {
		replacements[repoRoot] = "<repo>"
	}
	if toolRoot = strings.TrimSpace(toolRoot); toolRoot != "" && toolRoot != repoRoot {
		replacements[toolRoot] = "<tool-project>"
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
