// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolprotocol"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var errCaptureToolPathRequired = apperror.StaticError(
	"--tool-path is required with --capture-tool",
)

//nolint:tagliatelle // go test -json emits capitalized keys.
type goTestJSONEvent struct {
	Action string `json:"Action"`
}

const (
	capturedConfigurationExitCode = 2
	capturedCommandNotFoundCode   = 127
	capturedSandboxWrapperFailure = 126
	copiedProcessStreamCount      = 2
	capturedDecisionBlock         = "block"
	capturedFindingStatusFail     = "fail"
	capturedFindingStatusPass     = "pass"
	capturedPrivateDirMode        = 0o700
	capturedOutputKey             = "output"
	capturedStatusBlocked         = "blocked"
	capturedStatusResolved        = "resolved"
)

type captureRequest struct {
	Skills             map[string]policy.Skill
	Tool               string
	Parser             string
	Category           string
	ToolPath           string
	Cwd                string
	TraceRoot          string
	SandboxBackendPath string
	DiagnosticKind     string
	Output             io.Writer
	ToolPrefix         []string
	Args               []string
	FileExtensions     []string
	EvidenceMaps       []diagnostics.EvidenceMap
	Policies           []policy.Policy
	Capabilities       sandbox.Capabilities
	CodeIntel          bool
}

type captureExecution struct {
	Sandbox  *lint.SandboxEvidence
	Stdout   string
	Stderr   string
	RunArgs  []string
	Changes  []string
	ExitCode int
}

type formatterSnapshot struct {
	hash  [sha256.Size]byte
	found bool
}

type captureBuffers struct {
	stdout bytes.Buffer
	stderr bytes.Buffer
}

type processResult struct {
	err      error
	stdout   string
	stderr   string
	exitCode int
}

func runCapturedTool(
	tool string,
	toolPath string,
	cwd string,
	traceRoot string,
	args []string,
	policyContext PolicyContext,
) int {
	return runCapturedToolWithCodeIntel(
		tool,
		toolPath,
		cwd,
		traceRoot,
		args,
		policyContext,
		nil,
		"",
		false,
	)
}

func runCapturedToolWithCodeIntel(
	tool string,
	toolPath string,
	cwd string,
	traceRoot string,
	args []string,
	policyContext PolicyContext,
	output io.Writer,
	outputFormat string,
	codeIntel bool,
) int {
	request := captureRequest{
		Tool:         tool,
		Parser:       tool,
		ToolPath:     toolPath,
		Cwd:          cwd,
		TraceRoot:    traceRoot,
		Args:         append([]string(nil), args...),
		Output:       output,
		EvidenceMaps: policyContext.EvidenceMaps,
		Policies:     policyContext.Policies,
		Skills:       policyContext.Skills,
		CodeIntel:    codeIntel,
	}
	if strings.TrimSpace(request.ToolPath) == "" {
		exitErr(errCaptureToolPathRequired)
	}

	return runCapturedToolWithRequest(request, outputFormat)
}

func executeCapturedTool(request captureRequest) captureExecution {
	startedAt := time.Now()

	debuglog.Debug(
		"managed_capture.execute.enter",
		zap.String("tool", request.Tool),
		zap.String("tool_path", request.ToolPath),
		zap.String("cwd", request.Cwd),
	)

	defer func() {
		debuglog.Debug(
			"managed_capture.execute.exit",
			zap.String("tool", request.Tool),
			zap.Duration("elapsed", time.Since(startedAt)),
		)
	}()

	runArgs := capturedToolArgs(request.Tool, request.Args)
	runArgs = append(append([]string(nil), request.ToolPrefix...), runArgs...)
	plan, cacheEnv, planErr := buildCapturedSandboxPlan(request, runArgs)

	defer func() {
		err := plan.Close()
		if err != nil {
			emitManagedCaptureText("warning: sandbox resources not closed: " + err.Error())
		}
	}()

	evidence := lintSandboxEvidence(plan.Evidence)

	if planErr != nil {
		debuglog.Debug(
			"managed_capture.sandbox_plan.error",
			zap.String("tool", request.Tool),
			zap.String("executable", request.ToolPath),
			zap.Error(planErr),
		)

		cleanupSandboxCacheEnv(cacheEnv)

		diagnostic := sandboxDenialDiagnostic(plan.Evidence)

		return captureExecution{
			Stderr:   diagnostic.Message + " " + diagnostic.Detail,
			RunArgs:  runArgs,
			Sandbox:  evidence,
			ExitCode: BlockedExitCode,
		}
	}

	debuglog.Debug(
		"managed_capture.sandbox_plan.built",
		zap.String("tool", request.Tool),
		zap.String("executable", plan.Executable),
		zap.String("backend", plan.Evidence.Backend),
		zap.String("backend_path", plan.Evidence.BackendPath),
		zap.String("cwd", request.Cwd),
		zap.Int("arg_count", len(runArgs)),
		zap.Int("read_path_count", len(plan.Evidence.ReadPaths)),
		zap.Int("write_path_count", len(plan.Evidence.WritePaths)),
		zap.Int("timeout_seconds", plan.Evidence.TimeoutSeconds),
	)

	return runCapturedPlan(request, plan, runArgs, cacheEnv)
}

func buildCapturedSandboxPlan(
	request captureRequest,
	runArgs []string,
) (sandbox.Plan, sandboxCacheEnvironment, error) {
	executable, executableErr := agentShellToolExecutable(request.ToolPath)
	if executableErr != nil {
		return sandbox.Plan{
			Evidence: sandbox.Evidence{
				Denied: true,
				Reason: executableErr.Error(),
			},
		}, sandboxCacheEnvironment{}, executableErr
	}

	cacheEnv, cacheEnvErr := sandboxCacheEnv(context.Background(), request)
	if cacheEnvErr != nil {
		return sandbox.Plan{
			Evidence: sandbox.Evidence{
				Denied: true,
				Reason: cacheEnvErr.Error(),
			},
		}, cacheEnv, cacheEnvErr
	}

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Tool:       request.Tool,
		Executable: executable,
		WrapperPath: firstCaptureNonEmpty(
			request.SandboxBackendPath,
			captureSandboxWrapperPath(),
		),
		Cwd:          request.Cwd,
		RepoRoot:     firstCaptureNonEmpty(request.TraceRoot, request.Cwd),
		Args:         runArgs,
		BackendPath:  request.SandboxBackendPath,
		Capabilities: captureSandboxCapabilities(request),
	})
	if err != nil {
		return plan, cacheEnv, fmt.Errorf("build captured sandbox plan: %w", err)
	}

	if !plan.Evidence.Enabled {
		cleanupSandboxCacheEnv(cacheEnv)

		return plan, sandboxCacheEnvironment{}, nil
	}

	err = prepareManagedWritablePaths(
		firstCaptureNonEmpty(request.TraceRoot, request.Cwd),
		plan.Evidence,
	)
	if err != nil {
		plan.Evidence.Denied = true
		plan.Evidence.Reason = err.Error()

		return plan, cacheEnv, err
	}

	return plan, cacheEnv, nil
}

func captureSandboxCapabilities(request captureRequest) sandbox.Capabilities {
	capabilities := request.Capabilities
	if activeAgentShellSandboxCoversCapture(request) {
		capabilities.RequiresProcesses = true
	}

	return capabilities
}

func runCapturedPlan(
	request captureRequest,
	plan sandbox.Plan,
	runArgs []string,
	cacheEnv sandboxCacheEnvironment,
) captureExecution {
	commandContext, cancel := sandbox.CommandContext(
		context.Background(),
		plan.Evidence.TimeoutSeconds,
	)
	defer cancel()

	cgroup, appliedEvidence, cgroupErr := prepareSandboxCgroup(plan.Evidence)
	if cgroupErr != nil {
		appliedEvidence.Reason = appendEvidenceReason(
			appliedEvidence.Reason,
			cgroupErr.Error(),
		)
	}

	evidence := lintSandboxEvidence(appliedEvidence)

	if cgroup != nil {
		defer func() { _ = cgroup.Close() }()
	}

	startedAt := time.Now()

	debuglog.Debug(
		"managed_capture.process.enter",
		zap.String("tool", request.Tool),
		zap.String("executable", plan.Executable),
		zap.String("cwd", request.Cwd),
		zap.Int("timeout_seconds", appliedEvidence.TimeoutSeconds),
		zap.Bool("cgroup", cgroup != nil),
	)
	result := startCapturedProcess(
		commandContext,
		request,
		plan,
		cacheEnv,
		cgroup,
		appliedEvidence,
	)
	debuglog.Debug(
		"managed_capture.process.exit",
		zap.String("tool", request.Tool),
		zap.Int("exit_code", result.exitCode),
		zap.Duration("elapsed", time.Since(startedAt)),
		zap.Error(result.err),
	)

	if deniedEvidence, denied := capturedSandboxRuntimeDenial(
		appliedEvidence,
		result,
	); denied {
		evidence = lintSandboxEvidence(deniedEvidence)
		diagnostic := sandboxDenialDiagnostic(deniedEvidence)

		return captureExecution{
			Stderr:   diagnostic.Message + " " + diagnostic.Detail,
			RunArgs:  runArgs,
			Sandbox:  evidence,
			ExitCode: BlockedExitCode,
		}
	}

	if errors.Is(commandContext.Err(), context.DeadlineExceeded) {
		appliedEvidence.Denied = true
		appliedEvidence.Reason = "sandboxed tool exceeded timeout"
		evidence = lintSandboxEvidence(appliedEvidence)
	}

	return captureExecution{
		Stdout:   result.stdout,
		Stderr:   capturedExecutionError(result.stderr, result.err),
		RunArgs:  runArgs,
		Sandbox:  evidence,
		ExitCode: result.exitCode,
	}
}

func startCapturedProcess(
	ctx context.Context,
	request captureRequest,
	plan sandbox.Plan,
	cacheEnv sandboxCacheEnvironment,
	cgroup *sandbox.Cgroup,
	evidence sandbox.Evidence,
) processResult {
	processIO, failedResult, ok := openCapturedProcessIO(plan)
	if !ok {
		cleanupSandboxCacheEnv(cacheEnv)

		return failedResult
	}
	defer processIO.closeReaders()

	argv := capturedProcessArgv(plan)
	process, startedAt, startErr := startCapturedOSProcess(
		request,
		plan,
		processIO.files,
		argv,
		cacheEnv,
		cgroup,
		evidence,
	)

	processIO.closeWriters()

	var buffers captureBuffers

	copyDone := copyProcessOutput(
		&buffers,
		processIO.stdoutReader,
		processIO.stderrReader,
	)

	if startErr != nil {
		return failedCapturedProcessStart(
			startedAt,
			argv,
			request,
			copyDone,
			cacheEnv,
			startErr,
		)
	}

	if result, failed := assignCapturedProcessToCgroup(
		ctx,
		process,
		cgroup,
		startedAt,
		argv,
		request,
		cacheEnv,
		copyDone,
		&buffers,
	); failed {
		return result
	}

	return waitForCapturedProcessResult(
		ctx,
		process,
		startedAt,
		argv,
		request,
		copyDone,
		cacheEnv,
		&buffers,
	)
}

func failedProcessStartResult(
	copyDone <-chan error,
	cacheEnv sandboxCacheEnvironment,
	startErr error,
) processResult {
	copyErr := <-copyDone

	cleanupSandboxCacheEnv(cacheEnv)

	return processResult{
		err:      errors.Join(startErr, copyErr),
		exitCode: capturedExitCode(startErr),
	}
}

func failedCgroupAssignmentResult(
	ctx context.Context,
	process *os.Process,
	copyDone <-chan error,
	cacheEnv sandboxCacheEnvironment,
	buffers *captureBuffers,
	assignErr error,
) processResult {
	killErr := killCapturedProcessGroup(process)
	state, waitErr := waitCapturedProcess(ctx, process)
	copyErr := <-copyDone

	cleanupSandboxCacheEnv(cacheEnv)

	return processResult{
		stdout: buffers.stdout.String(),
		stderr: buffers.stderr.String(),
		err: errors.Join(
			assignErr,
			killErr,
			waitErr,
			copyErr,
		),
		exitCode: capturedProcessExitCode(state, assignErr),
	}
}

func capturedProcessFiles(
	stdout *os.File,
	stderr *os.File,
	extraFiles []*os.File,
) []*os.File {
	const standardProcessFileCount = 3

	files := make([]*os.File, 0, len(extraFiles)+standardProcessFileCount)
	files = append(files, os.Stdin, stdout, stderr)
	files = append(files, extraFiles...)

	return files
}

func capturedProcessArgv(plan sandbox.Plan) []string {
	return append([]string{plan.Executable}, plan.Args...)
}

func capturedProcessEnv(
	environ []string,
	cacheEnv sandboxCacheEnvironment,
	tool string,
) []string {
	out := make([]string, 0, len(environ))
	hasPath := false

	for _, item := range environ {
		name, value, found := strings.Cut(item, "=")
		if !found {
			out = append(out, item)

			continue
		}

		if capturedProcessEnvBlocked(name) {
			continue
		}

		if cacheEnv.overrides(name) {
			continue
		}

		if name == capturedProcessPathEnv {
			hasPath = true

			out = append(
				out,
				name+"="+capturedProcessPathWithPrefix(
					value,
					cacheEnv.PathPrefix,
				),
			)

			continue
		}

		out = append(out, item)
	}

	if !hasPath {
		out = append(
			out,
			capturedProcessPathEnv+"="+capturedProcessPathWithPrefix(
				"",
				cacheEnv.PathPrefix,
			),
		)
	}

	out = append(out, cacheEnv.items()...)
	if tool == toolprotocol.ActionlintTool {
		out = append(out, toolprotocol.ActionlintShellcheckEnvironment())
	}

	return out
}

func resolvedGoTestSandboxTempDir(root string) string {
	return filepath.Join(root, sandbox.SandboxTempWritePath, goTestSandboxTempName(root))
}

func goTestSandboxTempDir(root string) string {
	return filepath.Join(root, sandbox.SandboxTempWritePath, goTestSandboxTempName(root))
}

func goTestSandboxTempName(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))

	return fmt.Sprintf("coding-ethos-go-test-%x-%d", sum[:8], os.Getpid())
}

func gpgRuntimeWritePath() string {
	runtimeRoot := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
	if runtimeRoot == "" {
		runtimeRoot = filepath.Join("/run/user", strconv.Itoa(os.Getuid()))
	}

	return filepath.Join(runtimeRoot, "gnupg")
}

func copyProcessOutput(
	buffers *captureBuffers,
	stdout *os.File,
	stderr *os.File,
) <-chan error {
	done := make(chan error, 1)
	errs := make(chan error, copiedProcessStreamCount)

	go func() {
		errs <- copyBuffer(&buffers.stdout, stdout)
	}()

	go func() {
		errs <- copyBuffer(&buffers.stderr, stderr)
	}()

	go func() {
		done <- errors.Join(<-errs, <-errs)
	}()

	return done
}

func copyBuffer(writer io.Writer, reader io.Reader) error {
	_, err := io.Copy(writer, reader)
	if err != nil {
		return fmt.Errorf("copy process output: %w", err)
	}

	return nil
}

func waitCapturedProcess(
	ctx context.Context,
	process *os.Process,
) (*os.ProcessState, error) {
	done := make(chan struct {
		state *os.ProcessState
		err   error
	}, 1)

	go func() {
		state, err := process.Wait()
		done <- struct {
			state *os.ProcessState
			err   error
		}{state: state, err: err}
	}()

	select {
	case result := <-done:
		return result.state, result.err
	case <-ctx.Done():
		killErr := killCapturedProcessGroup(process)
		result := <-done

		if result.err != nil {
			return result.state, result.err
		}

		if killErr != nil {
			return result.state, fmt.Errorf("kill timed-out process: %w", killErr)
		}

		return result.state, fmt.Errorf("process context expired: %w", ctx.Err())
	}
}

func capturedProcessSysProcAttr(
	cgroup *sandbox.Cgroup,
	evidence sandbox.Evidence,
) *syscall.SysProcAttr {
	return sandbox.SysProcAttr(cgroup, evidence)
}

func killCapturedProcessGroup(process *os.Process) error {
	err := syscall.Kill(-process.Pid, syscall.SIGKILL)
	if err == nil {
		return nil
	}

	killErr := process.Kill()
	if killErr != nil {
		return errors.Join(err, killErr)
	}

	return nil
}

func capturedProcessExitCode(state *os.ProcessState, err error) int {
	if state == nil {
		return capturedExitCode(err)
	}

	status, ok := state.Sys().(syscall.WaitStatus)
	if !ok {
		return capturedExitCode(err)
	}

	if status.Exited() {
		return status.ExitStatus()
	}

	if err != nil {
		return capturedExitCode(err)
	}

	return 1
}

func prepareSandboxCgroup(
	evidence sandbox.Evidence,
) (*sandbox.Cgroup, sandbox.Evidence, error) {
	if !evidence.Enabled {
		return nil, evidence, nil
	}

	cgroup, appliedEvidence, err := sandbox.PrepareCgroupLimits(evidence)
	appliedEvidence.Reason = appendEvidenceReason(
		evidence.Reason,
		appliedEvidence.Reason,
	)

	if err != nil {
		return nil, appliedEvidence, fmt.Errorf(
			"prepare sandbox cgroup limits: %w",
			err,
		)
	}

	return cgroup, appliedEvidence, nil
}

func appendEvidenceReason(existing, next string) string {
	existing = strings.TrimSpace(existing)
	next = strings.TrimSpace(next)

	if existing == "" {
		return next
	}

	if next == "" || strings.Contains(existing, next) {
		return existing
	}

	return existing + "; " + next
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

func capturedSandboxRuntimeDenial(
	evidence sandbox.Evidence,
	result processResult,
) (sandbox.Evidence, bool) {
	if !evidence.Enabled {
		return evidence, false
	}

	reason, denied := capturedSandboxRuntimeDenialReason(result)
	if !denied {
		return evidence, false
	}

	evidence.Denied = true
	evidence.Reason = reason

	return evidence, true
}

func capturedSandboxRuntimeDenialReason(result processResult) (string, bool) {
	errText := strings.TrimSpace(capturedExecutionError(result.stderr, result.err))
	lowerText := strings.ToLower(errText)

	if lowerText == "" {
		return "", false
	}

	if result.exitCode == capturedSandboxWrapperFailure &&
		strings.Contains(lowerText, "coding-ethos-sandbox:") {
		return errText, true
	}

	if result.err != nil &&
		(strings.Contains(lowerText, "permission denied") ||
			strings.Contains(lowerText, "no such file or directory") ||
			strings.Contains(lowerText, "operation not permitted")) {
		return errText, true
	}

	return "", false
}

func captureSandboxWrapperPath() string {
	executable, err := os.Executable()
	if err != nil || strings.TrimSpace(executable) == "" {
		return ""
	}

	helperName := "coding-ethos-sandbox"
	if runtime.GOOS == "windows" {
		helperName += ".exe"
	}

	return filepath.Join(filepath.Dir(executable), helperName)
}

func capturedToolResult(
	request captureRequest,
	execution captureExecution,
) lint.Result {
	parser := firstCaptureNonEmpty(request.Parser, request.Tool)
	parsed := capturedExecutionDiagnostics(parser, execution)
	parsed = append(parsed, formatterChangedDiagnostics(request, execution.Changes)...)
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

func capturedExecutionDiagnostics(
	parser string,
	execution captureExecution,
) []diagnostics.Diagnostic {
	if execution.Sandbox != nil && execution.Sandbox.Denied {
		return []diagnostics.Diagnostic{
			sandboxDenialDiagnostic(sandboxEvidenceFromLint(*execution.Sandbox)),
		}
	}

	return diagnostics.Parse(parser, execution.Stdout, execution.Stderr)
}

func capturedFindings(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
	items []diagnostics.Diagnostic,
) []lint.Finding {
	outcome := capturedOutcome(request.Tool, execution.ExitCode, outputExcerpt, items)
	if len(items) == 0 {
		return capturedOutcomeFindings(request, execution, outputExcerpt, outcome)
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

func capturedOutcomeFindings(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
	outcome capturedOutcomeClass,
) []lint.Finding {
	if execution.ExitCode == 0 {
		if request.Category == toolcatalog.CategoryFormat {
			return nil
		}

		if outputExcerpt == "" {
			return nil
		}

		parser := firstCaptureNonEmpty(request.Parser, request.Tool)
		if diagnostics.RecognizesCleanOutput(parser, execution.Stdout, execution.Stderr) {
			return nil
		}

		return []lint.Finding{
			capturedPassingOutputFinding(request, execution, outputExcerpt),
		}
	}

	return []lint.Finding{
		capturedUnparseableFailureFinding(request, execution, outputExcerpt, outcome),
	}
}

func capturedUnparseableFailureFinding(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
	outcome capturedOutcomeClass,
) lint.Finding {
	return lint.Finding{
		RawOutcome: map[string]any{
			"category":        outcome.Category,
			"args":            append([]string(nil), request.Args...),
			"exit_code":       execution.ExitCode,
			"run_args":        append([]string(nil), execution.RunArgs...),
			capturedOutputKey: outputExcerpt,
		},
		CheckID:    "tool." + request.Tool,
		Message:    outcome.Message,
		Severity:   "fatal",
		SourceTool: request.Tool,
		Status:     capturedFindingStatusFail,
		Blocking:   true,
	}
}

func capturedPassingOutputFinding(
	request captureRequest,
	execution captureExecution,
	outputExcerpt string,
) lint.Finding {
	file := firstCapturedArgFile(request.Args)

	return lint.Finding{
		RawOutcome: map[string]any{
			"category":        "tool_output",
			"args":            append([]string(nil), request.Args...),
			"exit_code":       execution.ExitCode,
			"run_args":        append([]string(nil), execution.RunArgs...),
			capturedOutputKey: outputExcerpt,
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
			result.Status = capturedStatusBlocked
			result.Findings = append(
				result.Findings,
				capturedPolicyErrorFinding(policyDef, err),
			)

			continue
		}

		if len(decisions) == 0 {
			continue
		}

		result.Decisions = append(result.Decisions, decisions...)
		for _, decision := range decisions {
			result.Diagnostics = append(result.Diagnostics, decision.Diagnostics...)

			result.Findings = append(
				result.Findings,
				capturedDecisionFindings(decision, context.Files)...)
			if decision.Decision == capturedDecisionBlock ||
				decision.Severity == capturedDecisionBlock {
				result.Status = capturedStatusBlocked
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
		Status:   capturedFindingStatusFail,
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
			return nil, fmt.Errorf("evaluate capture policy %s: %w", policyDef.ID, err)
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
		return capturedFindingStatusFail
	}

	if decision.Decision == severityRecord || decision.Severity == severityRecord {
		return capturedFindingStatusPass
	}

	return decision.Decision
}

func capturedDecisionBlocks(decision policy.Decision) bool {
	return decision.Decision == capturedDecisionBlock ||
		decision.Severity == capturedDecisionBlock
}

func capturedDecisionRawOutcome(decision policy.Decision) map[string]any {
	outcome := map[string]any{}
	maps.Copy(outcome, decision.Evidence)

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

		if arg == ruffCheckCommand || arg == golangciLintRunCommand || arg == "lint" {
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
		Parser:        firstCaptureNonEmpty(request.Parser, request.Tool),
		Category:      request.Category,
		ParseStatus:   capturedParseStatus(execution.ExitCode, items, outputExcerpt),
		OutputExcerpt: outputExcerpt,
		Stdout:        execution.Stdout,
		Stderr:        execution.Stderr,
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
		Advice: "Use the managed tool path with declared capabilities, " +
			"or install the required sandbox backend.",
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
	if evidence.Mode == "" && evidence.Profile == "" && !evidence.Enabled &&
		!evidence.Denied {
		return nil
	}

	copied := cloneSandboxEvidence(evidence)

	return &copied
}

func sandboxEvidenceFromLint(evidence lint.SandboxEvidence) sandbox.Evidence {
	return cloneSandboxEvidence(evidence)
}

func cloneSandboxEvidence(evidence sandbox.Evidence) sandbox.Evidence {
	evidence.Command = append([]string(nil), evidence.Command...)
	evidence.Tags = append([]string(nil), evidence.Tags...)
	evidence.HiddenCredentialDirs = append(
		[]string(nil),
		evidence.HiddenCredentialDirs...,
	)
	evidence.ReadPaths = append([]string(nil), evidence.ReadPaths...)
	evidence.WritePaths = append([]string(nil), evidence.WritePaths...)
	evidence.EnvBindings = append([]string(nil), evidence.EnvBindings...)

	return evidence
}

func capturedParseStatus(
	exitCode int,
	items []diagnostics.Diagnostic,
	outputExcerpt string,
) string {
	if diagnosticsAreFormatterChanges(items) {
		return "changed_files"
	}

	if len(items) > 0 {
		return "parsed"
	}

	if exitCode == 0 {
		if strings.TrimSpace(outputExcerpt) != "" {
			return capturedOutputKey
		}

		return "empty"
	}

	if exitCode == capturedConfigurationExitCode {
		return "tool_config_error"
	}

	return "parse_error"
}

func diagnosticsAreFormatterChanges(items []diagnostics.Diagnostic) bool {
	if len(items) == 0 {
		return false
	}

	for _, item := range items {
		category, ok := item.Metadata["category"].(string)
		if !ok || category != "formatter_changed_file" {
			return false
		}
	}

	return true
}

const maxCapturedOutputExcerpt = 600

const capturedFormatterSummaryFieldCount = 4

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
		if err != nil || rel == "." || rel == "" ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			rel == ".." {
			continue
		}

		out[index].File = filepath.ToSlash(rel)
		out[index].Message = redactCapturedOutputPaths(out[index].Message, absRoot, "")
		out[index].Advice = redactCapturedOutputPaths(out[index].Advice, absRoot, "")
		out[index].Detail = redactCapturedOutputPaths(out[index].Detail, absRoot, "")
	}

	return out
}

func captureFormatterSnapshots(
	request captureRequest,
) map[string]formatterSnapshot {
	if request.DiagnosticKind != toolcatalog.DiagnosticKindFormatterChangedFiles {
		return nil
	}

	files := captureFormatterCandidateFiles(request)

	snapshots := make(map[string]formatterSnapshot, len(files))
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		snapshots[file] = formatterSnapshot{
			hash:  sha256.Sum256(content),
			found: true,
		}
	}

	return snapshots
}

func captureFormatterChanges(
	request captureRequest,
	snapshots map[string]formatterSnapshot,
) []string {
	if len(snapshots) == 0 {
		return nil
	}

	changed := []string{}

	for file, before := range snapshots {
		if !before.found {
			continue
		}

		content, err := os.ReadFile(file)
		if err != nil {
			if os.IsNotExist(err) {
				changed = append(changed, formatterDiagnosticPath(request, file))
			}

			continue
		}

		if sha256.Sum256(content) != before.hash {
			changed = append(changed, formatterDiagnosticPath(request, file))
		}
	}

	sort.Strings(changed)

	return changed
}

func captureFormatterCandidateFiles(request captureRequest) []string {
	files := []string{}
	seen := map[string]bool{}

	for _, arg := range request.Args {
		addFormatterCandidate(&files, seen, request.Cwd, arg)
	}

	if len(files) > 0 {
		return files
	}

	return walkFormatterCandidateFiles(request.Cwd, request.FileExtensions)
}

func addFormatterCandidate(
	files *[]string,
	seen map[string]bool,
	cwd string,
	arg string,
) {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.HasPrefix(arg, "-") || formatterCommandArg(arg) {
		return
	}

	path := arg
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return
	}

	path = filepath.Clean(path)
	if seen[path] {
		return
	}

	seen[path] = true
	*files = append(*files, path)
}

func formatterCommandArg(arg string) bool {
	switch arg {
	case "check", "format", "fmt", "run", "lint":
		return true
	default:
		return false
	}
}

func walkFormatterCandidateFiles(cwd string, extensions []string) []string {
	if len(extensions) == 0 {
		return nil
	}

	files := []string{}

	extensionSet := map[string]bool{}
	for _, extension := range extensions {
		extensionSet[extension] = true
	}

	err := filepath.WalkDir(cwd, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			if shouldSkipFormatterDir(path, entry.Name()) {
				return filepath.SkipDir
			}

			return nil
		}

		if extensionSet[filepath.Ext(path)] {
			files = append(files, filepath.Clean(path))
		}

		return nil
	})
	if err != nil {
		return nil
	}

	sort.Strings(files)

	return files
}

func shouldSkipFormatterDir(path, name string) bool {
	if path == "." || path == "" {
		return false
	}

	switch name {
	case ".git", ".coding-ethos", ".mypy_cache", ".ruff_cache", ".venv",
		"build", "dist", "node_modules", "__pycache__":
		return true
	default:
		return false
	}
}

func formatterChangedDiagnostics(
	request captureRequest,
	files []string,
) []diagnostics.Diagnostic {
	if len(files) == 0 {
		return nil
	}

	items := make([]diagnostics.Diagnostic, 0, len(files))
	for _, file := range files {
		items = append(items, diagnostics.Diagnostic{
			Metadata: formatterChangedMetadata(request),
			Tool:     request.Tool,
			File:     file,
			Line:     1,
			Severity: "warning",
			Code:     "formatted",
			Message:  request.Tool + " changed this file.",
		})
	}

	return items
}

func formatterChangedMetadata(request captureRequest) map[string]any {
	metadata := map[string]any{
		"category": "formatter_changed_file",
	}

	if len(request.Args) > 0 {
		metadata["args"] = append([]string(nil), request.Args...)
	}

	if len(request.ToolPrefix) > 0 {
		metadata["tool_prefix"] = append([]string(nil), request.ToolPrefix...)
	}

	return metadata
}

func formatterDiagnosticPath(request captureRequest, file string) string {
	root := firstCaptureNonEmpty(request.TraceRoot, request.Cwd)
	if root == "" {
		return filepath.ToSlash(file)
	}

	rel, err := filepath.Rel(root, file)
	if err != nil || rel == "." || rel == "" ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
		rel == ".." {
		return filepath.ToSlash(file)
	}

	return filepath.ToSlash(rel)
}

func capturedOutputExcerpt(stdout, stderr, repoRoot, toolRoot string) string {
	output := capturedCombinedOutput(stdout, stderr)
	if output == "" {
		return ""
	}

	output = redactCapturedOutputPaths(output, repoRoot, toolRoot)

	if capturedOutputIsEmptyMachinePayload(output) {
		return ""
	}

	output = strings.Join(strings.Fields(output), " ")
	if len(output) <= maxCapturedOutputExcerpt {
		return output
	}

	return output[:maxCapturedOutputExcerpt] + "..."
}

func capturedCombinedOutput(stdout, stderr string) string {
	stdout = strings.TrimSpace(stdout)
	stderr = strings.TrimSpace(stderr)

	switch {
	case stdout == "" && stderr == "":
		return ""
	case stdout == "":
		return stderr
	case stderr == "":
		return stdout
	case stdout == stderr:
		return stdout
	default:
		return stderr + "\n" + stdout
	}
}

func capturedOutputIsEmptyMachinePayload(output string) bool {
	trimmed := strings.TrimSpace(output)
	compact := strings.ReplaceAll(trimmed, " ", "")

	switch compact {
	case "[]", "{}", "null", "0issues.":
		return true
	default:
	}

	if capturedOutputIsFormatterSummary(trimmed) {
		return true
	}

	if golangciReportHasNoIssues(trimmed) {
		return true
	}

	if goTestReportHasNoFailures(trimmed) {
		return true
	}

	lines := strings.Split(trimmed, "\n")
	if len(lines) > 1 &&
		golangciReportHasNoIssues(lines[0]) &&
		capturedTrailingOutputIsInformational(lines[1:]) {
		return true
	}

	return false
}

func capturedOutputIsFormatterSummary(output string) bool {
	fields := strings.Fields(output)
	if len(fields) != capturedFormatterSummaryFieldCount {
		return false
	}

	if fields[1] != "files" || fields[2] != "left" || fields[3] != "unchanged" {
		return false
	}

	for _, char := range fields[0] {
		if char < '0' || char > '9' {
			return false
		}
	}

	return fields[0] != ""
}

func golangciReportHasNoIssues(output string) bool {
	var payload map[string]json.RawMessage
	if json.Unmarshal([]byte(output), &payload) != nil {
		return false
	}

	issues, ok := payload["Issues"]
	if !ok {
		return false
	}

	var parsed []json.RawMessage
	if json.Unmarshal(issues, &parsed) != nil {
		return false
	}

	return len(parsed) == 0
}

func goTestReportHasNoFailures(output string) bool {
	seen := false

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event goTestJSONEvent
		if json.Unmarshal([]byte(line), &event) != nil || event.Action == "" {
			return false
		}

		seen = true

		if event.Action == capturedFindingStatusFail {
			return false
		}
	}

	return seen
}

func capturedTrailingOutputIsInformational(lines []string) bool {
	for _, line := range lines {
		switch strings.TrimSpace(line) {
		case "", "0 issues.":
		default:
			return false
		}
	}

	return true
}

func redactCapturedOutputPaths(output, repoRoot, toolRoot string) string {
	redacted := output

	replacements := map[string]string{}
	if repoRoot = strings.TrimSpace(repoRoot); repoRoot != "" {
		replacements[repoRoot] = "<repo>"
	}

	if toolRoot = strings.TrimSpace(toolRoot); toolRoot != "" && toolRoot != repoRoot {
		replacements[toolRoot] = "<tool-project>"
	}

	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		replacements[home] = "<home>"
	}

	paths := make([]string, 0, len(replacements))
	for path := range replacements {
		paths = append(paths, path)
	}

	sort.Slice(paths, func(i, j int) bool {
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
	outputExcerpt string,
	items []diagnostics.Diagnostic,
) capturedOutcomeClass {
	if len(items) > 0 {
		return capturedOutcomeClass{
			Category: "lint_findings",
			Message:  tool + " reported diagnostics",
		}
	}

	if exitCode == 0 {
		return capturedOutcomeClass{Category: "success", Message: tool + " passed"}
	}

	switch exitCode {
	case capturedConfigurationExitCode:
		if capturedOutputHasPermissionFailure(outputExcerpt) {
			return capturedOutcomeClass{
				Category: "permission_error",
				Message:  tool + " failed because a target path is not writable",
			}
		}

		return capturedOutcomeClass{
			Category: "configuration_error",
			Message: fmt.Sprintf(
				"%s configuration or usage failed with status %d",
				tool,
				exitCode,
			),
		}
	default:
		return capturedOutcomeClass{
			Category: "tool_error",
			Message: fmt.Sprintf(
				"%s exited with status %d without parseable diagnostics",
				tool,
				exitCode,
			),
		}
	}
}

func capturedOutputHasPermissionFailure(outputExcerpt string) bool {
	lowerOutput := strings.ToLower(outputExcerpt)

	return strings.Contains(lowerOutput, "permission denied") ||
		strings.Contains(lowerOutput, "operation not permitted")
}

func capturedExitCode(err error) int {
	return processstatus.ExitCode(err, capturedCommandNotFoundCode)
}

func capturedStatus(exitCode int) string {
	if exitCode == 0 {
		return capturedStatusResolved
	}

	return capturedStatusBlocked
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
