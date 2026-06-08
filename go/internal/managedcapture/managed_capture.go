// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
	"blackcat.ca/coding-ethos/go/lintcapture"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	configArgCapacity       = 2
	golangciLintAutofixTool = "golangci-lint-autofix"
	golangciLintFormatTool  = "golangci-lint-format"
	golangciLintTool        = "golangci-lint"
	golangciLintFmtCommand  = "fmt"
	golangciLintRunCommand  = "run"
	goFileExtension         = ".go"
	goTestTool              = "go-test"
	goVetTool               = "go-vet"
	maxFormatterWritePaths  = 4096
	ruffCheckCommand        = "check"
	ruffFormatCommand       = "format"
	severityRecord          = "record"
)

var (
	errUnknownCapturedTool        = apperror.StaticError("unknown captured lint tool")
	errManagedCaptureRootRequired = apperror.StaticError(
		"managed capture requires --ethos-root and --consumer-root",
	)
	errManagedRunnerUnconfigured = apperror.StaticError(
		"managed runner is not configured for lint capture",
	)
	errFormatterTargetLimit = apperror.StaticError(
		"formatter directory target exceeds language file limit",
	)
	errFormatterTargetNotWritable = apperror.StaticError(
		"formatter target is not writable",
	)
)

type Options struct {
	PolicyContext PolicyContext
	Tool          string
	EthosRoot     string
	ConsumerRoot  string
	InvocationCwd string
	OutputFormat  string
	Output        io.Writer
	Args          []string
	CodeIntel     bool
}

type managedToolCommand struct {
	Path   string
	Prefix []string
}

func Run(options Options) int {
	startedAt := time.Now()

	debuglog.Debug(
		"managed_capture.run.enter",
		zap.String("tool", options.Tool),
		zap.String("ethos_root", options.EthosRoot),
		zap.String("consumer_root", options.ConsumerRoot),
		zap.String("invocation_cwd", options.InvocationCwd),
		zap.Strings("args", options.Args),
	)

	finish := func(exitCode int) int {
		debuglog.Debug(
			"managed_capture.run.exit",
			zap.String("tool", options.Tool),
			zap.Int("exit_code", exitCode),
			zap.Duration("elapsed", time.Since(startedAt)),
		)

		return exitCode
	}

	if managedCaptureRootsMissing(options) {
		return finish(printManagedCaptureError(errManagedCaptureRootRequired, 1))
	}

	tool, found := toolcatalog.HookOwnedTool(options.Tool)
	if !found {
		return finish(printManagedCaptureError(
			fmt.Errorf("%w: %s", errUnknownCapturedTool, options.Tool),
			1,
		))
	}

	config, err := lintcapture.LoadRuntimeConfig(
		options.EthosRoot,
		options.ConsumerRoot,
	)
	if err != nil {
		return finish(printManagedCaptureError(err, 1))
	}

	enabled, err := managedToolEnabled(tool.Name, options.EthosRoot, options.ConsumerRoot)
	if err != nil {
		return finish(printManagedCaptureError(err, 1))
	}

	if !enabled {
		return finish(0)
	}

	drift := generatedConfigDrift(options.EthosRoot, options.ConsumerRoot)
	if len(drift) > 0 {
		return finish(printConfigDrift(drift))
	}

	request, exitCode, err := managedCaptureRequest(tool, config, options)
	if err != nil {
		return finish(printManagedCaptureError(err, exitCode))
	}

	return finish(runCapturedToolWithRequest(
		request,
		firstCaptureNonEmpty(options.OutputFormat, hookoutput.SelectedFormat()),
	))
}

func managedCaptureRequest(
	tool toolcatalog.Tool,
	config lintcapture.RuntimeConfig,
	options Options,
) (captureRequest, int, error) {
	captureCwd, args, exitCode, err := managedCaptureExecutionContext(
		tool,
		config,
		options,
	)
	if err != nil {
		return captureRequest{}, exitCode, err
	}

	command := managedToolCommandFor(tool, options.EthosRoot)

	if command.Path == "" {
		return captureRequest{},
			1,
			fmt.Errorf("%w: %s", errManagedRunnerUnconfigured, options.Tool)
	}

	capabilities, capabilitiesErr := sandboxCapabilitiesForRequest(
		tool,
		config,
		options.ConsumerRoot,
		captureCwd,
		args,
	)
	if capabilitiesErr != nil {
		return captureRequest{}, BlockedExitCode, capabilitiesErr
	}

	debuglog.Debug(
		"managed_capture.request.built",
		zap.String("tool", options.Tool),
		zap.String("tool_path", command.Path),
		zap.Strings("tool_prefix", command.Prefix),
		zap.String("cwd", captureCwd),
		zap.Int("arg_count", len(args)),
		zap.Strings("file_extensions", tool.FileMatchSpec().Extensions),
		zap.Bool("sandbox_profiled", capabilities.SandboxProfile != ""),
	)

	return captureRequest{
		Tool:       options.Tool,
		Parser:     firstCaptureNonEmpty(tool.Parser, options.Tool),
		Category:   tool.Category,
		ToolPath:   command.Path,
		ToolPrefix: command.Prefix,
		Cwd:        captureCwd,
		TraceRoot:  options.ConsumerRoot,
		Args:       args,
		SandboxBackendPath: filepath.Join(
			options.EthosRoot,
			"bin",
			"coding-ethos-sandbox",
		),
		DiagnosticKind: tool.DiagnosticKind,
		FileExtensions: append(
			[]string(nil),
			tool.FileMatchSpec().Extensions...,
		),
		Output:       options.Output,
		Capabilities: capabilities,
		CodeIntel:    options.CodeIntel,
		EvidenceMaps: options.PolicyContext.EvidenceMaps,
		Policies:     options.PolicyContext.Policies,
		Skills:       options.PolicyContext.Skills,
	}, 0, nil
}

func managedToolEnabled(toolName, ethosRoot, consumerRoot string) (bool, error) {
	config, err := toolconfigs.LoadMergedConfig(ethosRoot, consumerRoot, "")
	if err != nil {
		return false, fmt.Errorf("load generated tool config: %w", err)
	}

	return toolEnabled(config, toolName), nil
}

func toolEnabled(config map[string]any, toolName string) bool {
	toolKey := strings.ReplaceAll(toolName, "-", "_")

	value, ok := configdata.GetPath(
		config,
		"tooling."+toolKey+".enabled",
		true,
	).(bool)
	if !ok {
		return true
	}

	return value
}

func managedCaptureExecutionContext(
	tool toolcatalog.Tool,
	config lintcapture.RuntimeConfig,
	options Options,
) (string, []string, int, error) {
	sourceRoots, err := config.LintSourceRoots()
	if err != nil {
		return "", nil, BlockedExitCode, fmt.Errorf("resolve lint source roots: %w", err)
	}

	resolver, err := lintcapture.NewTargetResolver(
		options.ConsumerRoot,
		options.InvocationCwd,
		sourceRoots,
	)
	if err != nil {
		return "", nil, BlockedExitCode, fmt.Errorf("create target resolver: %w", err)
	}

	resolvedArgs, err := resolver.ResolveArgs(options.Args)
	if err != nil {
		return "", nil, 1, fmt.Errorf("resolve managed capture args: %w", err)
	}

	enforcedArgs := enforceManagedToolArgs(
		tool,
		resolver.RelativizeArgs(resolvedArgs),
		options.ConsumerRoot,
	)

	cwd, args := normalizedManagedCaptureContext(
		tool,
		options.ConsumerRoot,
		options.InvocationCwd,
		enforcedArgs,
	)

	return cwd, args, 0, nil
}

func printManagedCaptureError(err error, exitCode int) int {
	emitManagedCaptureError(err)

	return exitCode
}

func normalizedManagedCaptureContext(
	tool toolcatalog.Tool,
	consumerRoot string,
	invocationCwd string,
	args []string,
) (string, []string) {
	if isGolangciLintTool(tool.Name) {
		return normalizeGolangciLintWorktree(
			consumerRoot,
			invocationCwd,
			args,
		)
	}

	if isGoTool(tool.Name) {
		return normalizeGoToolWorktree(consumerRoot, invocationCwd, args)
	}

	return consumerRoot, args
}

func managedCaptureRootsMissing(options Options) bool {
	return strings.TrimSpace(options.EthosRoot) == "" ||
		strings.TrimSpace(options.ConsumerRoot) == ""
}

func sandboxCapabilities(
	tool toolcatalog.Tool,
	config lintcapture.RuntimeConfig,
) sandbox.Capabilities {
	spec := tool.CapabilitySpec()
	if config.SandboxToolRequiresNetwork(tool.Name) {
		spec.RequiresNetwork = true
		spec.Tags = capabilityTagsRequire(spec.Tags, "network", "no-network")
	}

	if spec.RequiresNetwork && spec.RequiresProcesses {
		spec.SandboxProfile = ""
		spec.SeccompProfile = ""
		spec.MemoryMB = 0
		spec.CPUQuotaPercent = 0
	}

	writePaths := append([]string(nil), spec.WritePaths...)
	writePaths = append(writePaths, config.SandboxReadWritePaths()...)
	readPaths := append([]string(nil), spec.ReadPaths...)
	readPaths = append(readPaths, writePaths...)

	return sandbox.Capabilities{
		Tags:               append([]string(nil), spec.Tags...),
		ReadPaths:          readPaths,
		WritePaths:         writePaths,
		SandboxProfile:     spec.SandboxProfile,
		TimeoutSeconds:     spec.TimeoutSeconds,
		MemoryMB:           spec.MemoryMB,
		CPUQuotaPercent:    spec.CPUQuotaPercent,
		RequiresNetwork:    spec.RequiresNetwork,
		RequiresGit:        spec.RequiresGit,
		RequiresEnv:        spec.RequiresEnv,
		RequiresProcesses:  spec.RequiresProcesses,
		SeccompProfile:     spec.SeccompProfile,
		SeccompProfilePath: spec.SeccompProfilePath,
	}
}

func sandboxCapabilitiesForRequest(
	tool toolcatalog.Tool,
	config lintcapture.RuntimeConfig,
	consumerRoot string,
	captureCwd string,
	args []string,
) (sandbox.Capabilities, error) {
	capabilities := sandboxCapabilities(tool, config)
	if tool.Name == goTestTool {
		capabilities.RequiresGit = true
		capabilities.Tags = capabilityTagsRequire(capabilities.Tags, "git", "no-git")
	}

	writePaths, err := toolSandboxWritePaths(tool, consumerRoot, captureCwd, args)
	if err != nil {
		return sandbox.Capabilities{}, err
	}

	capabilities.WritePaths = append(
		capabilities.WritePaths,
		writePaths...,
	)
	capabilities.ReadPaths = append(capabilities.ReadPaths, writePaths...)

	return capabilities, nil
}

func capabilityTagsRequire(tags []string, requiredTag, deniedTag string) []string {
	updated := make([]string, 0, len(tags)+1)
	hasRequired := false

	for _, tag := range tags {
		if tag == requiredTag {
			hasRequired = true

			updated = append(updated, tag)

			continue
		}

		if tag == deniedTag {
			continue
		}

		updated = append(updated, tag)
	}

	if !hasRequired {
		updated = append(updated, requiredTag)
	}

	return updated
}

func toolSandboxWritePaths(
	tool toolcatalog.Tool,
	consumerRoot string,
	captureCwd string,
	args []string,
) ([]string, error) {
	if tool.Name == goTestTool {
		paths := append(
			sandboxRelativePath(consumerRoot, captureCwd),
			goTestSandboxTempDir(consumerRoot),
		)

		if runtimePath := gpgRuntimeWritePath(); runtimePath != "" {
			_, err := os.Stat(runtimePath)
			if err == nil {
				paths = append(paths, runtimePath)
			}
		}

		return paths, nil
	}

	if tool.Category != toolcatalog.CategoryFormat {
		return nil, nil
	}

	fileMatch := tool.FileMatchSpec()
	if !fileMatch.PassFilesAsArgs {
		return sandboxRelativePath(consumerRoot, captureCwd), nil
	}

	return formatArgWritePaths(
		captureCwd,
		args,
		tool.ConfigSpec().Flags,
		fileMatch.Extensions,
	)
}

func sandboxRelativePath(root, path string) []string {
	relative, err := filepath.Rel(root, path)
	if err != nil ||
		relative == ".." ||
		strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return nil
	}

	return []string{filepath.ToSlash(relative)}
}

func formatArgWritePaths(
	cwd string,
	args []string,
	configFlags []string,
	fileExtensions []string,
) ([]string, error) {
	writePaths := []string{}
	skipNext := false

	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		if configFlagConsumesValue(arg, configFlags) {
			skipNext = true

			continue
		}

		if strings.HasPrefix(arg, "-") || formatterCommandArg(arg) {
			continue
		}

		targets, err := formatterWritableTargets(cwd, arg, fileExtensions)
		if err != nil {
			return nil, err
		}

		if len(targets) == 0 {
			continue
		}

		for _, target := range targets {
			if slices.Contains(writePaths, target) {
				continue
			}

			writePaths = append(writePaths, target)
		}
	}

	return writePaths, nil
}

func formatterWritableTargets(cwd, path string, extensions []string) ([]string, error) {
	statPath := path
	if !filepath.IsAbs(statPath) {
		statPath = filepath.Join(cwd, path)
	}

	if pathHasExtension(path, extensions) {
		return formatterFileWriteTargets(cwd, path, statPath, extensions)
	}

	info, statErr := os.Stat(statPath)
	if os.IsNotExist(statErr) {
		return nil, nil
	}

	if statErr != nil {
		return nil, fmt.Errorf("stat formatter target %s: %w", statPath, statErr)
	}

	if !info.IsDir() {
		return nil, nil
	}

	return formatterDirectoryWriteTargets(cwd, statPath, extensions)
}

func formatterFileWriteTargets(
	cwd, path, statPath string,
	extensions []string,
) ([]string, error) {
	info, statErr := os.Stat(statPath)
	if os.IsNotExist(statErr) {
		return []string{path}, nil
	}

	if statErr != nil {
		return nil, fmt.Errorf("stat formatter target %s: %w", statPath, statErr)
	}

	if info.IsDir() {
		return formatterDirectoryWriteTargets(cwd, statPath, extensions)
	}

	err := requireFormatterTargetWritable(path, statPath)
	if err != nil {
		return nil, err
	}

	return []string{path}, nil
}

func formatterDirectoryWriteTargets(
	cwd string,
	dir string,
	extensions []string,
) ([]string, error) {
	targets := []string{}
	visitor := func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() || !pathHasExtension(path, extensions) {
			return nil
		}

		target := formatterDirectoryWriteTarget(cwd, path)

		err = requireFormatterTargetWritable(target, path)
		if err != nil {
			return err
		}

		targets = append(targets, target)
		if len(targets) > maxFormatterWritePaths {
			return fmt.Errorf(
				"%w: %s exceeds %d language files",
				errFormatterTargetLimit,
				dir,
				maxFormatterWritePaths,
			)
		}

		return nil
	}

	walkErr := filepath.WalkDir(dir, visitor)
	if walkErr != nil {
		return nil, fmt.Errorf(
			"walk formatter directory target %s: %w",
			dir,
			walkErr,
		)
	}

	return targets, nil
}

func requireFormatterTargetWritable(displayPath, absolutePath string) error {
	file, err := os.OpenFile(absolutePath, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		if os.IsPermission(err) || errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w: %s: %w", errFormatterTargetNotWritable, displayPath, err)
		}

		return fmt.Errorf("open formatter target %s: %w", absolutePath, err)
	}

	closeErr := file.Close()
	if closeErr != nil {
		return fmt.Errorf("close formatter target %s: %w", absolutePath, closeErr)
	}

	return nil
}

func formatterDirectoryWriteTarget(cwd, path string) string {
	relative, err := filepath.Rel(cwd, path)
	if err != nil {
		return path
	}

	return filepath.ToSlash(relative)
}

func configFlagConsumesValue(arg string, configFlags []string) bool {
	return slices.Contains(configFlags, arg)
}

func pathHasExtension(path string, extensions []string) bool {
	if len(extensions) == 0 {
		return true
	}

	return slices.Contains(extensions, filepath.Ext(path))
}

func runCapturedToolWithRequest(request captureRequest, outputFormat string) int {
	if strings.TrimSpace(request.ToolPath) == "" {
		exitErr(errCaptureToolPathRequired)
	}

	snapshots := captureFormatterSnapshots(request)
	execution := executeCapturedTool(request)
	execution.Changes = captureFormatterChanges(request, snapshots)
	result := lint.EnrichResultWithSkills(
		capturedToolResult(request, execution),
		request.Skills,
	)

	if capturedResultBlocks(result) {
		result.Status = capturedStatusBlocked
	}

	traceRoot := firstCaptureNonEmpty(request.TraceRoot, request.Cwd)
	tracePath := logCapturedToolResult(traceRoot, result)

	if request.CodeIntel && tracePath != "" {
		refreshCapturedToolCodeIntel(traceRoot, tracePath, execution.Changes)
	}

	resolvedFormat := firstCaptureNonEmpty(outputFormat, hookoutput.SelectedFormat())
	if shouldRenderCapturedResult(result, resolvedFormat) {
		err := hookoutput.EncodeLintResult(
			captureOutputWriter(request),
			result,
			resolvedFormat,
		)
		if err != nil {
			emitManagedCaptureText("warning: lint result not rendered: " + err.Error())
		}
	}

	if capturedResultBlocks(result) && execution.ExitCode == 0 {
		return BlockedExitCode
	}

	return execution.ExitCode
}

func shouldRenderCapturedResult(result lint.Result, outputFormat string) bool {
	if result.Blocked() || capturedResultBlocks(result) {
		return true
	}

	if outputFormat == hookoutput.FormatJSON || outputFormat == hookoutput.FormatSARIF {
		return len(result.Diagnostics) > 0 || len(result.Findings) > 0
	}

	if capturedFormatToolSucceeded(result) {
		return false
	}

	return capturedFindingsNeedRendering(result) ||
		capturedDiagnosticsNeedRendering(result)
}

func capturedFormatToolSucceeded(result lint.Result) bool {
	return result.Capture != nil &&
		result.Capture.Category == toolcatalog.CategoryFormat
}

func capturedFindingsNeedRendering(result lint.Result) bool {
	for _, finding := range result.Findings {
		if finding.Severity != severityRecord || finding.Status == "warn" {
			return true
		}
	}

	return false
}

func capturedDiagnosticsNeedRendering(result lint.Result) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity != severityRecord {
			return true
		}
	}

	return false
}

func capturedResultBlocks(result lint.Result) bool {
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Severity == "block" || diagnostic.Severity == "error" {
			return true
		}
	}

	for _, finding := range result.Findings {
		if finding.Blocking ||
			finding.Status == capturedFindingStatusFail ||
			finding.Severity == "block" ||
			finding.Severity == "error" {
			return true
		}
	}

	return false
}

func captureOutputWriter(request captureRequest) io.Writer {
	if request.Output != nil {
		return request.Output
	}

	return os.Stdout
}

func generatedConfigDrift(ethosRoot, repoRoot string) []lintcapture.ConfigDrift {
	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethosRoot, repoRoot)
	if err != nil {
		return []lintcapture.ConfigDrift{{File: lintcapture.ToolConfigHashManifest}}
	}

	return drift
}

func printConfigDrift(drift []lintcapture.ConfigDrift) int {
	rows := make([][]string, 0, len(drift))
	for _, item := range drift {
		rows = append(rows, []string{item.File})
	}

	feedback.Emit(
		os.Stdout,
		feedback.Message{
			Scalars: []feedback.Scalar{
				feedback.S("tool", "coding-ethos-config-integrity"),
				feedback.S("status", "FAIL"),
				feedback.S("title", "GENERATED TOOL CONFIG DRIFT"),
				feedback.S(
					"message",
					"lint failed after generated config drift: "+joinDriftFiles(drift),
				),
				feedback.S(
					"next",
					"Run make -C coding-ethos fix-configs, then rerun the lint command.",
				),
			},
			Tables: []feedback.Table{
				feedback.T("drifted_configs", []string{"file"}, rows),
			},
		},
		feedback.FormatTOON,
	)

	return BlockedExitCode
}

func emitManagedCaptureError(err error) {
	feedback.Emit(
		os.Stderr,
		feedback.Error{Message: err.Error()},
		feedback.FormatTOON,
	)
}

func emitManagedCaptureText(text string) {
	feedback.Emit(os.Stderr, feedback.Text{Text: text}, feedback.FormatTOON)
}

func joinDriftFiles(drift []lintcapture.ConfigDrift) string {
	files := make([]string, 0, len(drift))
	for _, item := range drift {
		files = append(files, item.File)
	}

	return strings.Join(files, "; ")
}

func managedToolCommandFor(tool toolcatalog.Tool, ethosRoot string) managedToolCommand {
	if managed := tool.ManagedExecutablePath(ethosRoot); managed != "" {
		if isExecutable(managed) {
			return managedToolCommand{Path: managed}
		}

		if command := managedGoToolFallback(tool); command.Path != "" {
			return command
		}

		if command := managedGoRuntimeCommand(tool); command.Path != "" {
			return command
		}

		return managedToolCommand{}
	}

	if command := managedPythonToolCommand(tool, ethosRoot); command.Path != "" {
		return command
	}

	uvBin := strings.TrimSpace(os.Getenv("UV"))
	if uvBin == "" {
		var err error

		uvBin, err = exec.LookPath("uv")
		if err != nil {
			return managedToolCommand{}
		}
	}

	resolved, err := exec.LookPath(uvBin)
	if err != nil {
		return managedToolCommand{}
	}

	uvBin = resolved

	runtime := tool.RuntimeSpec()
	if len(runtime.Command) == 0 {
		return managedToolCommand{}
	}

	return managedToolCommand{
		Path: uvBin,
		Prefix: []string{
			"run",
			"--quiet",
			"--project",
			filepath.Join(ethosRoot, "pre-commit", "hooks"),
			runtime.Command[0],
		},
	}
}

func managedPythonToolCommand(
	tool toolcatalog.Tool,
	ethosRoot string,
) managedToolCommand {
	runtime := tool.RuntimeSpec()
	if runtime.Runtime != toolcatalog.RuntimePython &&
		runtime.Runtime != toolcatalog.RuntimeUV {
		return managedToolCommand{}
	}

	if len(runtime.Command) == 0 {
		return managedToolCommand{}
	}

	candidate := filepath.Join(ethosRoot, ".venv", "bin", runtime.Command[0])
	if isExecutable(candidate) {
		return managedToolCommand{Path: candidate}
	}

	python := filepath.Join(ethosRoot, ".venv", "bin", "python")
	if isExecutable(python) {
		return managedToolCommand{
			Path:   python,
			Prefix: []string{"-m", runtime.Command[0]},
		}
	}

	return managedToolCommand{}
}

func managedGoToolFallback(tool toolcatalog.Tool) managedToolCommand {
	if tool.Name != "actionlint" {
		return managedToolCommand{}
	}

	goBin, err := exec.LookPath("go")
	if err != nil {
		return managedToolCommand{}
	}

	return managedToolCommand{
		Path: goBin,
		Prefix: []string{
			"run",
			"github.com/rhysd/actionlint/cmd/actionlint@v1.7.7",
		},
	}
}

func managedGoRuntimeCommand(tool toolcatalog.Tool) managedToolCommand {
	runtime := tool.RuntimeSpec()
	if len(runtime.Command) == 0 ||
		(runtime.Command[0] != "go" && runtime.Command[0] != "gofmt") {
		return managedToolCommand{}
	}

	path, err := exec.LookPath(runtime.Command[0])
	if err != nil {
		return managedToolCommand{}
	}

	return managedToolCommand{Path: path}
}

func enforceManagedToolArgs(
	tool toolcatalog.Tool,
	args []string,
	consumerRoot string,
) []string {
	if captureArgsInformational(args) {
		return append([]string(nil), args...)
	}

	switch tool.Name {
	case "ruff":
		return enforceRuffArgs(args, consumerRoot)
	case "mypy":
		return enforceMypyArgs(args, consumerRoot)
	case "dotenv-linter":
		return enforceDotenvLinterArgs(args)
	case "sqlfluff":
		return enforceSQLFluffArgs(tool, args, consumerRoot)
	case "tombi":
		return enforceFirstCommandArgs(
			args,
			"lint",
			[]string{"--quiet", "--error-on-warnings"},
		)
	case golangciLintTool, golangciLintAutofixTool, golangciLintFormatTool:
		return enforceGolangciLintArgs(tool, args, consumerRoot)
	case goVetTool:
		return enforceFirstCommandArgs(args, "vet", nil)
	case goTestTool:
		return enforceGoTestArgs(args)
	default:
		return enforceCatalogConfigArgs(tool, args, consumerRoot)
	}
}

func enforceGoTestArgs(args []string) []string {
	enforced := []string{
		"-json",
		"-cover",
		"-p=1",
		"-buildvcs=false",
		"-count=1",
		"-timeout=30s",
		"-short",
	}

	return enforceFirstCommandArgs(args, "test", enforced)
}

func goTestArgsHavePackage(args []string) bool {
	for index := 0; index < len(args); index++ {
		arg := strings.TrimSpace(args[index])
		if arg == "" {
			continue
		}

		if arg == "--" {
			return index+1 < len(args)
		}

		if strings.HasPrefix(arg, "-") {
			if goTestFlagConsumesValue(arg) {
				index++
			}

			continue
		}

		return true
	}

	return false
}

func goTestFlagConsumesValue(arg string) bool {
	name, hasValue := strings.CutPrefix(arg, "-")
	if !hasValue || strings.Contains(name, "=") {
		return false
	}

	switch name {
	case "args", "benchtime", "blockprofile", "blockprofilerate",
		"covermode", "coverpkg", "coverprofile", "cpu", "cpuprofile",
		"exec", "gcflags", "ldflags", "list", "memprofile",
		"memprofilerate", "mod", "modfile", "mutexprofile",
		"mutexprofilefraction", "overlay", "p", "parallel", "run",
		"shuffle", "skip", "tags", "timeout", "toolexec", "trace", "vet":
		return true
	default:
		return false
	}
}

func enforceSQLFluffArgs(
	tool toolcatalog.Tool,
	args []string,
	consumerRoot string,
) []string {
	config := tool.ConfigSpec()

	return enforceSubcommandConfigArgs(
		args,
		"lint",
		"--config",
		filepath.Join(consumerRoot, config.RepoConfig),
	)
}

func enforceGolangciLintArgs(
	tool toolcatalog.Tool,
	args []string,
	consumerRoot string,
) []string {
	config := tool.ConfigSpec()

	if tool.Name == golangciLintFormatTool {
		return enforceSubcommandConfigArgs(
			args,
			golangciLintFmtCommand,
			"--config",
			filepath.Join(consumerRoot, config.RepoConfig),
		)
	}

	enforced := enforceSubcommandConfigArgs(
		args,
		golangciLintRunCommand,
		"--config",
		filepath.Join(consumerRoot, config.RepoConfig),
	)
	if tool.Name == golangciLintAutofixTool {
		return ensureGolangciLintFix(enforced)
	}

	return enforced
}

func ensureGolangciLintFix(args []string) []string {
	if slices.Contains(args, "--fix") {
		return args
	}

	if len(args) == 0 || args[0] != golangciLintRunCommand {
		return append([]string{golangciLintRunCommand, "--fix"}, args...)
	}

	return append([]string{golangciLintRunCommand, "--fix"}, args[1:]...)
}

func isGolangciLintTool(name string) bool {
	return name == golangciLintTool ||
		name == golangciLintAutofixTool ||
		name == golangciLintFormatTool
}

func isGoTool(name string) bool {
	return name == goVetTool || name == goTestTool
}

func captureArgsInformational(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--help", "-h", "help", "--version", "-V", "version":
			return true
		default:
		}
	}

	return false
}

func enforceRuffArgs(args []string, consumerRoot string) []string {
	configArgs := make([]string, 0, configArgCapacity+len(args))

	configArgs = append(
		configArgs,
		"--config",
		filepath.Join(consumerRoot, "ruff.toml"),
	)
	if len(args) > 0 && args[0] == ruffCheckCommand {
		return append(append([]string{ruffCheckCommand}, configArgs...), args[1:]...)
	}

	if len(args) > 0 && args[0] == ruffFormatCommand {
		return append(append([]string{ruffFormatCommand}, configArgs...), args[1:]...)
	}

	return append(configArgs, args...)
}

func enforceMypyArgs(args []string, consumerRoot string) []string {
	enforced := []string{"--config-file", filepath.Join(consumerRoot, "mypy.ini")}
	if python := consumerPythonExecutable(consumerRoot); python != "" {
		enforced = append(enforced, "--python-executable", python)
	}

	return append(enforced, args...)
}

func enforceDotenvLinterArgs(args []string) []string {
	trimmed := args
	if len(trimmed) > 0 && trimmed[0] == "check" {
		trimmed = trimmed[1:]
	}

	return append([]string{"--plain", "--quiet", "check"}, trimmed...)
}

func enforceFirstCommandArgs(
	args []string,
	command string,
	enforced []string,
) []string {
	trimmed := stripExactArgs(args, enforced...)
	if len(args) > 0 && args[0] == command {
		trimmed = stripExactArgs(args[1:], enforced...)

		return append(append([]string{command}, enforced...), trimmed...)
	}

	return append(append([]string{command}, enforced...), trimmed...)
}

func stripExactArgs(args []string, values ...string) []string {
	if len(values) == 0 {
		return append([]string(nil), args...)
	}

	stripped := []string{}

	for _, arg := range args {
		if stringInCaptureSet(arg, values) {
			continue
		}

		stripped = append(stripped, arg)
	}

	return stripped
}

func stringInCaptureSet(value string, candidates []string) bool {
	return slices.Contains(candidates, value)
}

func enforceSubcommandConfigArgs(
	args []string,
	command string,
	configFlag string,
	configPath string,
) []string {
	configArgs := []string{configFlag, configPath}
	if len(args) > 0 && args[0] == command {
		return append(append([]string{command}, configArgs...), args[1:]...)
	}

	return append(append([]string{command}, configArgs...), args...)
}

func normalizeGolangciLintWorktree(
	consumerRoot string,
	invocationCwd string,
	args []string,
) (string, []string) {
	if len(args) == 0 ||
		(args[0] != golangciLintRunCommand &&
			args[0] != golangciLintFmtCommand) {
		return consumerRoot, args
	}

	moduleIndex, moduleDir := firstGoModuleArgument(consumerRoot, args[1:])
	if moduleIndex < 0 {
		defaultModuleDir := defaultNestedGoModuleDir(
			consumerRoot,
			invocationCwd,
		)
		if defaultModuleDir != "" {
			return defaultModuleDir, normalizeGolangciLintTargets(
				defaultNestedGoModuleArgs(
					consumerRoot,
					defaultModuleDir,
					args,
				),
			)
		}

		return consumerRoot, args
	}

	normalized := append([]string(nil), args[:moduleIndex+1]...)
	normalized = append(normalized, "./...")
	normalized = append(normalized, args[moduleIndex+2:]...)

	return moduleDir, normalizeGolangciLintTargets(normalized)
}

func normalizeGolangciLintTargets(args []string) []string {
	if len(args) == 0 || args[0] != golangciLintRunCommand {
		return args
	}

	normalized := make([]string, 0, len(args))
	seenDirs := map[string]struct{}{}

	for _, arg := range args {
		if filepath.Ext(arg) != goFileExtension {
			normalized = append(normalized, arg)

			continue
		}

		dir := filepath.ToSlash(filepath.Dir(filepath.FromSlash(arg)))
		if dir == "." {
			dir = "./..."
		} else if !filepath.IsAbs(dir) && !strings.HasPrefix(dir, ".") {
			dir = "./" + dir
		}

		if _, seen := seenDirs[dir]; seen {
			continue
		}

		seenDirs[dir] = struct{}{}
		normalized = append(normalized, dir)
	}

	return normalized
}

func normalizeGoToolWorktree(
	consumerRoot string,
	invocationCwd string,
	args []string,
) (string, []string) {
	if len(args) == 0 || (args[0] != "vet" && args[0] != "test") {
		return consumerRoot, args
	}

	moduleIndex, moduleDir := firstGoModuleArgument(consumerRoot, args[1:])
	if moduleIndex < 0 {
		defaultModuleDir := defaultNestedGoModuleDir(
			consumerRoot,
			invocationCwd,
		)
		if defaultModuleDir != "" {
			return defaultModuleDir, defaultNestedGoModuleArgs(
				consumerRoot,
				defaultModuleDir,
				args,
			)
		}

		if args[0] == "test" && !goTestArgsHavePackage(args[1:]) {
			return consumerRoot, append(append([]string(nil), args...), "./...")
		}

		return consumerRoot, args
	}

	normalized := append([]string(nil), args[:moduleIndex+1]...)
	normalized = append(normalized, "./...")
	normalized = append(normalized, args[moduleIndex+2:]...)

	return moduleDir, normalized
}

func defaultNestedGoModuleArgs(
	consumerRoot string,
	moduleDir string,
	args []string,
) []string {
	normalized := make([]string, 0, len(args)+1)
	transformedTarget := false

	for _, arg := range args {
		moduleArg, transformed := nestedGoModuleArg(
			consumerRoot,
			moduleDir,
			arg,
		)
		normalized = append(normalized, moduleArg)
		transformedTarget = transformedTarget || transformed
	}

	if !transformedTarget {
		normalized = append(normalized, "./...")
	}

	return normalized
}

func nestedGoModuleArg(
	consumerRoot string,
	moduleDir string,
	arg string,
) (string, bool) {
	if strings.TrimSpace(arg) == "" || strings.HasPrefix(arg, "-") {
		return arg, false
	}

	candidate := arg
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(consumerRoot, filepath.FromSlash(arg))
	}

	relative, err := filepath.Rel(moduleDir, candidate)
	if err != nil {
		return arg, false
	}

	if relative == "." {
		return "./...", true
	}

	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return arg, false
	}

	return filepath.ToSlash(relative), true
}

func firstGoModuleArgument(consumerRoot string, args []string) (int, string) {
	for index, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}

		candidate := filepath.Join(consumerRoot, filepath.FromSlash(arg))
		if regularFileExists(filepath.Join(candidate, "go.mod")) {
			return index, candidate
		}
	}

	return -1, ""
}

func defaultNestedGoModuleDir(roots ...string) string {
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}

		candidate := filepath.Join(root, "go")
		if regularFileExists(filepath.Join(candidate, "go.mod")) {
			return candidate
		}
	}

	return ""
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.Mode().IsRegular()
}

func enforceCatalogConfigArgs(
	tool toolcatalog.Tool,
	args []string,
	consumerRoot string,
) []string {
	enforced := append([]string(nil), args...)

	config := tool.ConfigSpec()
	if config.RepoConfig != "" && len(config.Flags) > 0 {
		enforced = append(
			[]string{config.Flags[0], filepath.Join(consumerRoot, config.RepoConfig)},
			enforced...)
	}

	if len(config.PostArgs) > 0 {
		enforced = append(enforced, config.PostArgs...)
	}

	return enforced
}

func consumerPythonExecutable(consumerRoot string) string {
	for _, name := range []string{"python", "python3"} {
		candidate := filepath.Join(consumerRoot, ".venv", "bin", name)
		if isExecutable(candidate) {
			return candidate
		}
	}

	return ""
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return false
	}

	return info.Mode()&0o111 != 0
}
