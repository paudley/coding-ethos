// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/lintcapture"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	configArgCapacity       = 2
	golangciLintAutofixTool = "golangci-lint-autofix"
	golangciLintTool        = "golangci-lint"
	golangciLintRunCommand  = "run"
	ruffCheckCommand        = "check"
	ruffFormatCommand       = "format"
)

var (
	errUnknownCapturedTool        = errors.New("unknown captured lint tool")
	errManagedCaptureRootRequired = errors.New(
		"managed capture requires --ethos-root and --consumer-root",
	)
	errManagedRunnerUnconfigured = errors.New(
		"managed runner is not configured for lint capture",
	)
)

type managedCaptureOptions struct {
	PolicyContext capturePolicyData
	Tool          string
	EthosRoot     string
	ConsumerRoot  string
	InvocationCwd string
	SandboxMode   string
	OutputFormat  string
	Args          []string
}

type managedToolCommand struct {
	Path   string
	Prefix []string
}

func runManagedCapture(options managedCaptureOptions) int {
	if managedCaptureRootsMissing(options) {
		exitErr(errManagedCaptureRootRequired)
	}

	tool, found := toolcatalog.HookOwnedTool(options.Tool)
	if !found {
		exitErr(fmt.Errorf("%w: %s", errUnknownCapturedTool, options.Tool))
	}

	config, err := lintcapture.LoadRuntimeConfig(options.EthosRoot, options.ConsumerRoot)
	if err != nil {
		exitErr(err)
	}

	drift := generatedConfigDrift(options.EthosRoot, options.ConsumerRoot)
	if len(drift) > 0 {
		return printConfigDrift(drift)
	}

	sourceRoots, err := config.LintSourceRoots()
	if err != nil {
		exitBlockedErr(err)
	}

	resolver, err := lintcapture.NewTargetResolver(
		options.ConsumerRoot,
		options.InvocationCwd,
		sourceRoots,
	)
	if err != nil {
		exitBlockedErr(err)
	}

	resolvedArgs, err := resolver.ResolveArgs(options.Args)
	if err != nil {
		exitErr(err)
	}

	toolArgs := resolver.RelativizeArgs(resolvedArgs)
	enforcedArgs := enforceManagedToolArgs(
		tool,
		toolArgs,
		options.ConsumerRoot,
	)

	captureCwd := options.ConsumerRoot
	if isGolangciLintTool(tool.Name) {
		captureCwd, enforcedArgs = normalizeGolangciLintWorktree(
			options.ConsumerRoot,
			enforcedArgs,
		)
	}

	command := managedToolCommandFor(tool, options.EthosRoot)
	if command.Path == "" {
		exitErr(fmt.Errorf("%w: %s", errManagedRunnerUnconfigured, options.Tool))
	}

	return runCapturedToolWithRequest(captureRequest{
		Tool:         options.Tool,
		ToolPath:     command.Path,
		ToolPrefix:   command.Prefix,
		Cwd:          captureCwd,
		TraceRoot:    options.ConsumerRoot,
		Args:         enforcedArgs,
		SandboxMode:  options.SandboxMode,
		Capabilities: sandboxCapabilities(tool, config),
		EvidenceMaps: options.PolicyContext.EvidenceMaps,
		Policies:     options.PolicyContext.Policies,
		Skills:       options.PolicyContext.Skills,
	}, firstCaptureNonEmpty(options.OutputFormat, hookoutput.SelectedFormat()))
}

func managedCaptureRootsMissing(options managedCaptureOptions) bool {
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

func runCapturedToolWithRequest(request captureRequest, outputFormat string) int {
	if strings.TrimSpace(request.ToolPath) == "" {
		exitErr(errCaptureToolPathRequired)
	}

	execution := executeCapturedTool(request)
	result := lint.EnrichResultWithSkills(
		capturedToolResult(request, execution),
		request.Skills,
	)
	logCapturedToolResult(firstCaptureNonEmpty(request.TraceRoot, request.Cwd), result)

	if result.Blocked() || len(result.Diagnostics) > 0 || len(result.Findings) > 0 {
		err := hookoutput.EncodeLintResult(
			captureOutputWriter(request),
			result,
			firstCaptureNonEmpty(outputFormat, hookoutput.SelectedFormat()),
		)
		if err != nil {
			fmt.Fprintf(os.Stderr, "WARN: lint result not rendered: %v\n", err)
		}
	}

	if result.Blocked() && execution.ExitCode == 0 {
		return blockedExitCode
	}

	return execution.ExitCode
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

	return blockedExitCode
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

	python := filepath.Join(ethosRoot, ".venv", "bin", "python")
	if isExecutable(python) {
		return managedToolCommand{
			Path:   python,
			Prefix: []string{"-m", runtime.Command[0]},
		}
	}

	candidate := filepath.Join(ethosRoot, ".venv", "bin", runtime.Command[0])
	if isExecutable(candidate) {
		return managedToolCommand{Path: candidate}
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
		config := tool.ConfigSpec()

		return enforceSubcommandConfigArgs(
			args,
			"lint",
			"--config",
			filepath.Join(consumerRoot, config.RepoConfig),
		)
	case "tombi":
		return enforceFirstCommandArgs(
			args,
			"lint",
			[]string{"--quiet", "--error-on-warnings"},
		)
	case golangciLintTool, golangciLintAutofixTool:
		config := tool.ConfigSpec()

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
	default:
		return enforceCatalogConfigArgs(tool, args, consumerRoot)
	}
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
	return name == golangciLintTool || name == golangciLintAutofixTool
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

	configArgs = append(configArgs, "--config", filepath.Join(consumerRoot, "ruff.toml"))
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
	if len(args) > 0 && args[0] == command {
		return append(append([]string{command}, enforced...), args[1:]...)
	}

	return append(append([]string{command}, enforced...), args...)
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
	args []string,
) (string, []string) {
	if len(args) == 0 || args[0] != "run" {
		return consumerRoot, args
	}

	moduleIndex, moduleDir := firstGoModuleArgument(consumerRoot, args[1:])
	if moduleIndex < 0 {
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
