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
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var errCaptureToolPathRequired = errors.New("--tool-path is required with --capture-tool")

type captureRequest struct {
	Tool         string
	ToolPath     string
	Cwd          string
	TraceRoot    string
	Args         []string
	EvidenceMaps []diagnostics.EvidenceMap
}

type captureExecution struct {
	Stdout   string
	Stderr   string
	RunArgs  []string
	ExitCode int
}

func runCapturedTool(
	tool string,
	toolPath string,
	cwd string,
	traceRoot string,
	args []string,
	evidenceMaps []diagnostics.EvidenceMap,
) int {
	request := captureRequest{
		Tool:         tool,
		ToolPath:     toolPath,
		Cwd:          cwd,
		TraceRoot:    traceRoot,
		Args:         append([]string(nil), args...),
		EvidenceMaps: evidenceMaps,
	}
	if strings.TrimSpace(request.ToolPath) == "" {
		exitErr(errCaptureToolPathRequired)
	}

	execution := executeCapturedTool(request)
	result := capturedToolResult(request, execution)
	logCapturedToolResult(firstCaptureNonEmpty(request.TraceRoot, request.Cwd), result)
	if result.Blocked() || len(result.Diagnostics) > 0 {
		if encodeErr := hookoutput.EncodeLintResult(
			os.Stdout,
			result,
			hookoutput.SelectedFormat(),
		); encodeErr != nil {
			fmt.Fprintf(os.Stderr, "WARN: lint result not rendered: %v\n", encodeErr)
		}
	}

	return execution.ExitCode
}

func executeCapturedTool(request captureRequest) captureExecution {
	runArgs := capturedToolArgs(request.Tool, request.Args)
	command := exec.Command(request.ToolPath, runArgs...)
	if request.Cwd != "" {
		command.Dir = request.Cwd
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Stdin = os.Stdin
	err := command.Run()

	return captureExecution{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		RunArgs:  runArgs,
		ExitCode: capturedExitCode(err),
	}
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
	request captureRequest,
	execution captureExecution,
) lint.Result {
	parsed := diagnostics.Parse(request.Tool, execution.Stdout, execution.Stderr)
	parsed = normalizeCapturedDiagnosticPaths(parsed, request.TraceRoot)
	parsed = diagnostics.Enrich(parsed, request.EvidenceMaps)
	parsed = diagnostics.Dedupe(parsed)
	outputExcerpt := capturedOutputExcerpt(
		execution.Stdout,
		execution.Stderr,
		firstCaptureNonEmpty(request.TraceRoot, request.Cwd),
		request.Cwd,
	)

	return lint.Result{
		Scope:       "tool:" + request.Tool,
		Status:      capturedStatus(execution.ExitCode),
		Capture:     capturedToolMetadata(request, execution, outputExcerpt, parsed),
		Diagnostics: parsed,
		Findings:    capturedFindings(request, execution, outputExcerpt, parsed),
	}
}

func capturedFindings(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
	items []diagnostics.Diagnostic,
) []lint.Finding {
	outcome := capturedOutcome(request.Tool, execution.ExitCode, items)
	if len(items) == 0 {
		if execution.ExitCode == 0 {
			return nil
		}

		return []lint.Finding{{
			RawOutcome: map[string]any{
				"category":  outcome.Category,
				"args":      append([]string(nil), request.Args...),
				"exit_code": execution.ExitCode,
				"run_args":  append([]string(nil), execution.RunArgs...),
				"output":    outputExcerpt,
			},
			CheckID:    "tool." + request.Tool,
			Message:    outcome.Message,
			Severity:   "error",
			SourceTool: request.Tool,
			Status:     "fail",
			Blocking:   true,
		}}
	}

	findings := make([]lint.Finding, 0, len(items))
	for _, item := range items {
		findings = append(findings, lint.Finding{
			RawOutcome: map[string]any{
				"category":  outcome.Category,
				"args":      append([]string(nil), request.Args...),
				"exit_code": execution.ExitCode,
				"run_args":  append([]string(nil), execution.RunArgs...),
			},
			Advice:     item.Advice,
			CheckID:    firstCaptureNonEmpty(item.PolicyID, "tool."+request.Tool),
			Code:       item.Code,
			File:       item.File,
			Message:    item.Message,
			PolicyID:   item.PolicyID,
			Severity:   firstCaptureNonEmpty(item.Severity, "error"),
			SourceTool: firstCaptureNonEmpty(item.Tool, request.Tool),
			Status:     capturedStatus(execution.ExitCode),
			EthosIDs:   append([]string(nil), item.PrincipleIDs...),
			Blocking:   execution.ExitCode != 0,
			Column:     item.Column,
			Line:       item.Line,
		})
	}

	return findings
}

func capturedToolMetadata(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
	items []diagnostics.Diagnostic,
) *lint.ToolCapture {
	return &lint.ToolCapture{
		Tool:          request.Tool,
		Parser:        request.Tool,
		ParseStatus:   capturedParseStatus(execution.ExitCode, items),
		OutputExcerpt: outputExcerpt,
		Args:          append([]string(nil), request.Args...),
		RunArgs:       append([]string(nil), execution.RunArgs...),
		ExitCode:      execution.ExitCode,
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

	paths := make([]string, 0, len(replacements))
	for path := range replacements {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i int, j int) bool {
		if len(paths[i]) == len(paths[j]) {
			return paths[i] < paths[j]
		}

		return len(paths[i]) > len(paths[j])
	})

	for _, path := range paths {
		redacted = strings.ReplaceAll(redacted, path, replacements[path])
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
