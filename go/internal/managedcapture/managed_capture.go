// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/configdata"
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
	goTestTool              = "go-test"
	goVetTool               = "go-vet"
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
}

type managedToolCommand struct {
	Path   string
	Prefix []string
}

func Run(options Options) int {
	if managedCaptureRootsMissing(options) {
		return printManagedCaptureError(errManagedCaptureRootRequired, 1)
	}

	tool, found := toolcatalog.HookOwnedTool(options.Tool)
	if !found {
		return printManagedCaptureError(
			fmt.Errorf("%w: %s", errUnknownCapturedTool, options.Tool),
			1,
		)
	}

	config, err := lintcapture.LoadRuntimeConfig(
		options.EthosRoot,
		options.ConsumerRoot,
	)
	if err != nil {
		return printManagedCaptureError(err, 1)
	}

	enabled, err := managedToolEnabled(tool.Name, options.EthosRoot, options.ConsumerRoot)
	if err != nil {
		return printManagedCaptureError(err, 1)
	}

	if !enabled {
		return 0
	}

	drift := generatedConfigDrift(options.EthosRoot, options.ConsumerRoot)
	if len(drift) > 0 {
		return printConfigDrift(drift)
	}

	request, exitCode, err := managedCaptureRequest(tool, config, options)
	if err != nil {
		return printManagedCaptureError(err, exitCode)
	}

	return runCapturedToolWithRequest(
		request,
		firstCaptureNonEmpty(options.OutputFormat, hookoutput.SelectedFormat()),
	)
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
		Output: options.Output,
		Capabilities: sandboxCapabilitiesForRequest(
			tool,
			config,
			options.ConsumerRoot,
			captureCwd,
			args,
		),
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
	fmt.Fprintln(os.Stderr, err)

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
	writePaths := append([]string(nil), spec.WritePaths...)
	writePaths = append(writePaths, config.SandboxReadWritePaths()...)

	return sandbox.Capabilities{
		Tags:               append([]string(nil), spec.Tags...),
		ReadPaths:          append([]string(nil), spec.ReadPaths...),
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
) sandbox.Capabilities {
	capabilities := sandboxCapabilities(tool, config)
	capabilities.WritePaths = append(
		capabilities.WritePaths,
		toolSandboxWritePaths(tool, consumerRoot, captureCwd, args)...,
	)

	return capabilities
}

func toolSandboxWritePaths(
	tool toolcatalog.Tool,
	consumerRoot string,
	captureCwd string,
	args []string,
) []string {
	if tool.Name == goTestTool {
		paths := append(
			sandboxRelativePath(consumerRoot, captureCwd),
			goTestSandboxTempDir(consumerRoot),
		)

		if runtimePath := gpgRuntimeWritePath(); runtimePath != "" {
			paths = append(paths, runtimePath)
		}

		return paths
	}

	if tool.Category != toolcatalog.CategoryFormat {
		return nil
	}

	fileMatch := tool.FileMatchSpec()
	if !fileMatch.PassFilesAsArgs {
		return sandboxRelativePath(consumerRoot, captureCwd)
	}

	return formatArgWritePaths(args, tool.ConfigSpec().Flags, fileMatch.Extensions)
}

func sandboxRelativePath(root, path string) []string {
	relative, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(relative, "..") {
		return nil
	}

	return []string{filepath.ToSlash(relative)}
}

func formatArgWritePaths(
	args []string,
	configFlags []string,
	fileExtensions []string,
) []string {
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

		if !pathHasExtension(arg, fileExtensions) ||
			slices.Contains(writePaths, arg) {
			continue
		}

		writePaths = append(writePaths, arg)
	}

	return writePaths
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

	logCapturedToolResult(firstCaptureNonEmpty(request.TraceRoot, request.Cwd), result)

	resolvedFormat := firstCaptureNonEmpty(outputFormat, hookoutput.SelectedFormat())
	if shouldRenderCapturedResult(result, resolvedFormat) {
		err := hookoutput.EncodeLintResult(
			captureOutputWriter(request),
			result,
			resolvedFormat,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: lint result not rendered: %v\n", err)
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
	fmt.Fprintln(os.Stdout, "format: toon")
	fmt.Fprintln(os.Stdout, "tool: coding-ethos-config-integrity")
	fmt.Fprintln(os.Stdout, "status: FAIL")
	fmt.Fprintln(os.Stdout, "title: GENERATED TOOL CONFIG DRIFT")
	fmt.Fprintf(
		os.Stdout,
		"message: Hey - lint failed - you modified: %s. "+
			"Restore it before continuing. "+
			"Or run make -C coding-ethos fix-configs.\n",
		joinDriftFiles(drift),
	)
	fmt.Fprintf(os.Stdout, "drifted_configs[%d]{file}:\n", len(drift))

	for _, item := range drift {
		fmt.Fprintf(os.Stdout, "  %s\n", item.File)
	}

	fmt.Fprintln(os.Stdout, "next[1]{action}:")
	fmt.Fprintln(
		os.Stdout,
		"  Run make -C coding-ethos fix-configs, then rerun the lint command.",
	)

	return BlockedExitCode
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
		return enforceFirstCommandArgs(
			args,
			"test",
			[]string{"-json", "-cover", "-buildvcs=false", "-timeout=30s", "-short"},
		)
	default:
		return enforceCatalogConfigArgs(tool, args, consumerRoot)
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
			return defaultModuleDir, append(append([]string(nil), args...), "./...")
		}

		return consumerRoot, args
	}

	normalized := append([]string(nil), args[:moduleIndex+1]...)
	normalized = append(normalized, "./...")
	normalized = append(normalized, args[moduleIndex+2:]...)

	return moduleDir, normalized
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
			return defaultModuleDir, append(append([]string(nil), args...), "./...")
		}

		return consumerRoot, args
	}

	normalized := append([]string(nil), args[:moduleIndex+1]...)
	normalized = append(normalized, "./...")
	normalized = append(normalized, args[moduleIndex+2:]...)

	return moduleDir, normalized
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
