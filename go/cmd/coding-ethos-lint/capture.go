// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var errCaptureToolPathRequired = errors.New("--tool-path is required with --capture-tool")

type captureRequest struct {
	Tool               string
	ToolPath           string
	ToolPrefix         []string
	Cwd                string
	TraceRoot          string
	Args               []string
	SandboxMode        string
	SandboxBackendPath string
	Capabilities       sandbox.Capabilities
	EvidenceMaps       []diagnostics.EvidenceMap
	Policies           []policy.Policy
	Skills             map[string]policy.Skill
}

type captureExecution struct {
	Stdout   string
	Stderr   string
	RunArgs  []string
	Sandbox  *lint.SandboxEvidence
	ExitCode int
}

func runCapturedTool(
	tool string,
	toolPath string,
	cwd string,
	traceRoot string,
	args []string,
	policyContext capturePolicyData,
	outputFormat string,
) int {
	request := captureRequest{
		Tool:         tool,
		ToolPath:     toolPath,
		Cwd:          cwd,
		TraceRoot:    traceRoot,
		Args:         append([]string(nil), args...),
		EvidenceMaps: policyContext.EvidenceMaps,
		Policies:     policyContext.Policies,
		Skills:       policyContext.Skills,
	}
	if strings.TrimSpace(request.ToolPath) == "" {
		exitErr(errCaptureToolPathRequired)
	}

	return runCapturedToolWithRequest(request, outputFormat)
}

func executeCapturedTool(request captureRequest) captureExecution {
	runArgs := capturedToolArgs(request.Tool, request.Args)
	runArgs = append(append([]string(nil), request.ToolPrefix...), runArgs...)
	plan, planErr := sandbox.BuildPlan(sandbox.Request{
		Mode:         request.SandboxMode,
		Tool:         request.Tool,
		Executable:   request.ToolPath,
		Cwd:          request.Cwd,
		RepoRoot:     firstCaptureNonEmpty(request.TraceRoot, request.Cwd),
		Args:         runArgs,
		BackendPath:  request.SandboxBackendPath,
		Capabilities: request.Capabilities,
	})
	defer func() {
		if err := plan.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "WARN: sandbox resources not closed: %v\n", err)
		}
	}()
	evidence := lintSandboxEvidence(plan.Evidence)
	if planErr != nil {
		diagnostic := sandboxDenialDiagnostic(plan.Evidence)

		return captureExecution{
			Stderr:   diagnostic.Message + " " + diagnostic.Detail,
			RunArgs:  runArgs,
			Sandbox:  evidence,
			ExitCode: blockedExitCode,
		}
	}

	commandContext, cancel := sandbox.CommandContext(context.Background(), plan.Evidence.TimeoutSeconds)
	defer cancel()
	command := exec.CommandContext(commandContext, plan.Executable, plan.Args...)
	if request.Cwd != "" {
		command.Dir = request.Cwd
	}
	command.ExtraFiles = plan.ExtraFiles

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	command.Stdin = os.Stdin

	cgroup, appliedEvidence, cgroupErr := prepareSandboxCgroup(plan.Evidence)
	evidence = lintSandboxEvidence(appliedEvidence)
	if cgroupErr != nil && appliedEvidence.Mode == sandbox.ModeRequired {
		diagnostic := sandboxDenialDiagnostic(appliedEvidence)

		return captureExecution{
			Stdout:   stdout.String(),
			Stderr:   diagnostic.Message + " " + diagnostic.Detail,
			RunArgs:  runArgs,
			Sandbox:  evidence,
			ExitCode: blockedExitCode,
		}
	}
	if cgroup != nil {
		defer func() { _ = cgroup.Close() }()
		cgroup.ConfigureCommand(command)
	}

	if err := command.Start(); err != nil {
		return captureExecution{
			Stdout:   stdout.String(),
			Stderr:   capturedExecutionError(stderr.String(), err),
			RunArgs:  runArgs,
			Sandbox:  evidence,
			ExitCode: capturedExitCode(err),
		}
	}
	err := command.Wait()
	if commandContext.Err() == context.DeadlineExceeded {
		appliedEvidence.Denied = true
		appliedEvidence.Reason = "sandboxed tool exceeded timeout"
		evidence = lintSandboxEvidence(appliedEvidence)
	}

	return captureExecution{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		RunArgs:  runArgs,
		Sandbox:  evidence,
		ExitCode: capturedExitCode(err),
	}
}

func prepareSandboxCgroup(evidence sandbox.Evidence) (*sandbox.Cgroup, sandbox.Evidence, error) {
	if !evidence.Enabled {
		return nil, evidence, nil
	}

	return sandbox.PrepareCgroupLimits(evidence)
}

func capturedExecutionError(stderr string, err error) string {
	if err == nil {
		return stderr
	}
	if strings.TrimSpace(stderr) != "" {
		return stderr
	}

	return err.Error()
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

	result := lint.Result{
		Scope:       "tool:" + request.Tool,
		Status:      capturedStatus(execution.ExitCode),
		Capture:     capturedToolMetadata(request, execution, outputExcerpt, parsed),
		Diagnostics: parsed,
		Findings:    capturedFindings(request, execution, outputExcerpt, parsed),
	}
	result = applyCapturePolicies(request, result)
	result.SkillHints = lint.SkillHintsForDiagnostics(parsed, request.Skills)

	return result
}

func capturedFindings(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
	items []diagnostics.Diagnostic,
) []lint.Finding {
	outcome := capturedOutcome(request.Tool, execution.ExitCode, items)
	if len(items) == 0 {
		if execution.Sandbox != nil && execution.Sandbox.Denied {
			diagnostic := sandboxDenialDiagnostic(sandboxEvidenceFromLint(*execution.Sandbox))

			return []lint.Finding{{
				RawOutcome: map[string]any{
					"category": outcome.Category,
					"args":     append([]string(nil), request.Args...),
					"sandbox":  execution.Sandbox,
				},
				Advice:     diagnostic.Advice,
				CheckID:    diagnostic.PolicyID,
				Code:       diagnostic.Code,
				Message:    diagnostic.Message,
				PolicyID:   diagnostic.PolicyID,
				SkillID:    diagnostic.SkillID,
				Severity:   diagnostic.Severity,
				SourceTool: diagnostic.Tool,
				Status:     "blocked",
				EthosIDs:   append([]string(nil), diagnostic.PrincipleIDs...),
				Blocking:   true,
			}}
		}
		if execution.ExitCode == 0 {
			if outputExcerpt == "" {
				return nil
			}

			return []lint.Finding{capturedPassingOutputFinding(request, execution, outputExcerpt)}
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
			SkillID:    item.SkillID,
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

func capturedPassingOutputFinding(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
) lint.Finding {
	file := firstCapturedArgFile(request.Args)

	return lint.Finding{
		RawOutcome: map[string]any{
			"category":  "tool_output",
			"args":      append([]string(nil), request.Args...),
			"exit_code": execution.ExitCode,
			"run_args":  append([]string(nil), execution.RunArgs...),
			"output":    outputExcerpt,
		},
		CheckID:    "tool." + request.Tool + ".output",
		Code:       "TOOL_OUTPUT",
		File:       file,
		Message:    request.Tool + " emitted output while passing",
		PolicyID:   "tool.output_visible",
		Severity:   "warning",
		SourceTool: request.Tool,
		Status:     "warn",
		Blocking:   false,
	}
}

func applyCapturePolicies(request captureRequest, result lint.Result) lint.Result {
	if len(request.Policies) == 0 {
		return result
	}

	outputDiagnostics := lint.OutputDiagnostics(result)
	context := evaluators.Context{
		Cwd:         firstCaptureNonEmpty(request.TraceRoot, request.Cwd),
		EventName:   "lint-capture",
		Provider:    "lint",
		Scope:       result.Scope,
		Tool:        request.Tool,
		Files:       capturedDiagnosticFiles(outputDiagnostics),
		Argv:        append([]string(nil), request.Args...),
		Diagnostics: outputDiagnostics,
		Findings:    capturedFindingActivations(result.Findings),
	}
	if len(outputDiagnostics) == 1 {
		context.Diagnostic = &outputDiagnostics[0]
	}

	registry := evaluators.DefaultRegistry()
	for _, policyDef := range request.Policies {
		if !capturePolicyAppliesToTool(policyDef, request.Tool) {
			continue
		}
		decisions, err := evaluateCapturePolicy(policyDef, context, registry)
		if err != nil {
			result.Status = "blocked"
			result.Findings = append(result.Findings, capturedPolicyErrorFinding(policyDef, err))
			continue
		}
		if len(decisions) == 0 {
			continue
		}
		result.Decisions = append(result.Decisions, decisions...)
		for _, decision := range decisions {
			result.Diagnostics = append(result.Diagnostics, decision.Diagnostics...)
			result.Findings = append(result.Findings, capturedDecisionFindings(decision, context.Files)...)
			if decision.Decision == "block" || decision.Severity == "block" {
				result.Status = "blocked"
			}
		}
	}

	return result
}

func capturedPolicyErrorFinding(policyDef policy.Policy, err error) lint.Finding {
	return lint.Finding{
		RawOutcome: map[string]any{
			"category": "capture_policy_error",
			"error":    err.Error(),
		},
		CheckID:  policyDef.ID,
		Message:  "Captured tool policy evaluation failed.",
		PolicyID: policyDef.ID,
		Severity: "error",
		Status:   "fail",
		Blocking: true,
	}
}

func evaluateCapturePolicy(
	policyDef policy.Policy,
	context evaluators.Context,
	registry evaluators.Registry,
) ([]policy.Decision, error) {
	decisions := []policy.Decision{}
	for _, evaluatorSpec := range policyDef.Evaluators {
		if evaluatorSpec.Name != "cel.expression" {
			continue
		}
		evaluator, ok := registry.Lookup(evaluatorSpec.Name)
		if !ok {
			continue
		}
		context.EvaluatorOptions = evaluatorSpec.Options
		evaluated, err := evaluator.Evaluate(policyDef, context)
		if err != nil {
			return nil, err
		}
		decisions = append(decisions, evaluated...)
	}

	return decisions, nil
}

func capturePolicyAppliesToTool(policyDef policy.Policy, tool string) bool {
	for _, candidate := range policyDef.AppliesTo.Tools {
		if strings.EqualFold(strings.TrimSpace(candidate), tool) {
			return true
		}
	}

	return false
}

func capturedDecisionFindings(
	decision policy.Decision,
	files []string,
) []lint.Finding {
	if len(decision.Diagnostics) == 0 {
		return []lint.Finding{{
			CheckID:    decision.PolicyID,
			PolicyID:   decision.PolicyID,
			Status:     capturedDecisionStatus(decision),
			Severity:   decision.Severity,
			Message:    decision.Message,
			Advice:     decision.Suggestion,
			EthosIDs:   append([]string(nil), decision.PrincipleIDs...),
			Files:      append([]string(nil), files...),
			Blocking:   capturedDecisionBlocks(decision),
			RawOutcome: capturedDecisionRawOutcome(decision),
		}}
	}

	findings := make([]lint.Finding, 0, len(decision.Diagnostics))
	for _, diagnostic := range decision.Diagnostics {
		findings = append(findings, lint.Finding{
			CheckID:    firstCaptureNonEmpty(diagnostic.PolicyID, decision.PolicyID),
			PolicyID:   firstCaptureNonEmpty(diagnostic.PolicyID, decision.PolicyID),
			SourceTool: firstCaptureNonEmpty(diagnostic.Tool, "policy"),
			Status:     capturedDecisionStatus(decision),
			Severity:   firstCaptureNonEmpty(diagnostic.Severity, decision.Severity),
			Code:       diagnostic.Code,
			File:       diagnostic.File,
			Line:       diagnostic.Line,
			Column:     diagnostic.Column,
			SkillID:    diagnostic.SkillID,
			Message:    diagnostic.Message,
			Advice:     firstCaptureNonEmpty(diagnostic.Advice, decision.Suggestion),
			EthosIDs:   append([]string(nil), diagnostic.PrincipleIDs...),
			Files:      append([]string(nil), files...),
			Blocking:   capturedDecisionBlocks(decision),
			RawOutcome: capturedDecisionRawOutcome(decision),
		})
	}

	return findings
}

func capturedDecisionStatus(decision policy.Decision) string {
	if capturedDecisionBlocks(decision) {
		return "fail"
	}
	if decision.Decision == "record" || decision.Severity == "record" {
		return "pass"
	}

	return decision.Decision
}

func capturedDecisionBlocks(decision policy.Decision) bool {
	return decision.Decision == "block" || decision.Severity == "block"
}

func capturedDecisionRawOutcome(decision policy.Decision) map[string]any {
	outcome := map[string]any{}
	for key, value := range decision.Evidence {
		outcome[key] = value
	}

	return outcome
}

func capturedFindingActivations(findings []lint.Finding) []evaluators.Finding {
	activations := make([]evaluators.Finding, 0, len(findings))
	for _, finding := range findings {
		activations = append(activations, evaluators.Finding{
			Tool:         finding.SourceTool,
			Code:         finding.Code,
			Message:      finding.Message,
			File:         finding.File,
			Severity:     finding.Severity,
			PolicyID:     finding.PolicyID,
			SkillID:      finding.SkillID,
			PrincipleIDs: append([]string(nil), finding.EthosIDs...),
			Column:       finding.Column,
			Line:         finding.Line,
		})
	}

	return activations
}

func capturedDiagnosticFiles(items []diagnostics.Diagnostic) []string {
	files := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		file := strings.TrimSpace(item.File)
		if file == "" || seen[file] {
			continue
		}
		files = append(files, file)
		seen[file] = true
	}

	return files
}

func firstCapturedArgFile(args []string) string {
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg == "" || strings.HasPrefix(arg, "-") {
			continue
		}
		if arg == "check" || arg == "run" || arg == "lint" {
			continue
		}
		return filepath.ToSlash(arg)
	}

	return ""
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
		ParseStatus:   capturedParseStatus(execution.ExitCode, items, outputExcerpt),
		OutputExcerpt: outputExcerpt,
		Args:          append([]string(nil), request.Args...),
		RunArgs:       append([]string(nil), execution.RunArgs...),
		Sandbox:       execution.Sandbox,
		ExitCode:      execution.ExitCode,
	}
}

func sandboxDenialDiagnostic(evidence sandbox.Evidence) diagnostics.Diagnostic {
	reason := strings.TrimSpace(evidence.Reason)
	if reason == "" {
		reason = "sandbox capability request was denied"
	}

	return diagnostics.Diagnostic{
		Metadata: map[string]any{"sandbox": evidence},
		Advice:   "Use the managed tool path with declared capabilities, or install the required sandbox backend.",
		Code:     "SANDBOX_DENIED",
		Detail:   reason,
		Message:  "Managed tool sandbox execution was denied.",
		PolicyID: "runtime.sandbox_denial",
		Severity: "error",
		SkillID:  "managed-toolchain",
		Tool:     "coding-ethos-sandbox",
		PrincipleIDs: []string{
			"security-by-design",
			"one-path-for-critical-operations",
		},
		Tags: []string{"security", "sandbox", "runtime"},
	}
}

func lintSandboxEvidence(evidence sandbox.Evidence) *lint.SandboxEvidence {
	if evidence.Mode == "" && evidence.Profile == "" && !evidence.Enabled && !evidence.Denied {
		return nil
	}
	if evidence.Mode == sandbox.ModeOff && !evidence.Enabled && !evidence.Denied {
		return nil
	}

	return &lint.SandboxEvidence{
		Mode:                 evidence.Mode,
		Backend:              evidence.Backend,
		BackendPath:          evidence.BackendPath,
		Profile:              evidence.Profile,
		Tool:                 evidence.Tool,
		Command:              append([]string(nil), evidence.Command...),
		Tags:                 append([]string(nil), evidence.Tags...),
		HiddenCredentialDirs: append([]string(nil), evidence.HiddenCredentialDirs...),
		ReadPaths:            append([]string(nil), evidence.ReadPaths...),
		WritePaths:           append([]string(nil), evidence.WritePaths...),
		TimeoutSeconds:       evidence.TimeoutSeconds,
		MemoryMB:             evidence.MemoryMB,
		CPUQuotaPercent:      evidence.CPUQuotaPercent,
		RequiresNetwork:      evidence.RequiresNetwork,
		RequiresGit:          evidence.RequiresGit,
		RequiresEnv:          evidence.RequiresEnv,
		RequiresProcesses:    evidence.RequiresProcesses,
		GitReadOnly:          evidence.GitReadOnly,
		ReadOnlyRoot:         evidence.ReadOnlyRoot,
		NetworkIsolated:      evidence.NetworkIsolated,
		ProcessIsolated:      evidence.ProcessIsolated,
		TimeoutEnforced:      evidence.TimeoutEnforced,
		CgroupRequested:      evidence.CgroupRequested,
		CgroupEnabled:        evidence.CgroupEnabled,
		CgroupPath:           evidence.CgroupPath,
		SeccompProfile:       evidence.SeccompProfile,
		SeccompEnabled:       evidence.SeccompEnabled,
		Enabled:              evidence.Enabled,
		Denied:               evidence.Denied,
		Reason:               evidence.Reason,
	}
}

func sandboxEvidenceFromLint(evidence lint.SandboxEvidence) sandbox.Evidence {
	return sandbox.Evidence{
		Mode:                 evidence.Mode,
		Backend:              evidence.Backend,
		BackendPath:          evidence.BackendPath,
		Profile:              evidence.Profile,
		Tool:                 evidence.Tool,
		Command:              append([]string(nil), evidence.Command...),
		Tags:                 append([]string(nil), evidence.Tags...),
		HiddenCredentialDirs: append([]string(nil), evidence.HiddenCredentialDirs...),
		ReadPaths:            append([]string(nil), evidence.ReadPaths...),
		WritePaths:           append([]string(nil), evidence.WritePaths...),
		TimeoutSeconds:       evidence.TimeoutSeconds,
		MemoryMB:             evidence.MemoryMB,
		CPUQuotaPercent:      evidence.CPUQuotaPercent,
		RequiresNetwork:      evidence.RequiresNetwork,
		RequiresGit:          evidence.RequiresGit,
		RequiresEnv:          evidence.RequiresEnv,
		RequiresProcesses:    evidence.RequiresProcesses,
		GitReadOnly:          evidence.GitReadOnly,
		ReadOnlyRoot:         evidence.ReadOnlyRoot,
		NetworkIsolated:      evidence.NetworkIsolated,
		ProcessIsolated:      evidence.ProcessIsolated,
		TimeoutEnforced:      evidence.TimeoutEnforced,
		CgroupRequested:      evidence.CgroupRequested,
		CgroupEnabled:        evidence.CgroupEnabled,
		CgroupPath:           evidence.CgroupPath,
		SeccompProfile:       evidence.SeccompProfile,
		SeccompEnabled:       evidence.SeccompEnabled,
		Enabled:              evidence.Enabled,
		Denied:               evidence.Denied,
		Reason:               evidence.Reason,
	}
}

func capturedParseStatus(exitCode int, items []diagnostics.Diagnostic, outputExcerpt string) string {
	if len(items) > 0 {
		return "parsed"
	}
	if exitCode == 0 {
		if strings.TrimSpace(outputExcerpt) != "" {
			return "output"
		}

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
