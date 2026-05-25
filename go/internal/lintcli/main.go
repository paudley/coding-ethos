// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/managedcapture"
	"blackcat.ca/coding-ethos/go/internal/outputsurface"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var (
	errBundleRequired = apperror.StaticError(
		"--bundle is required for direct coding-ethos-lint use; run " +
			"coding-ethos-run lint or ./bin/lint so the active policy bundle is supplied",
	)
	errInvalidBundle        = apperror.StaticError("invalid policy bundle")
	errOutputFormatConflict = apperror.StaticError(
		"--json and --sarif are mutually exclusive",
	)
	errSARIFUnsupported = apperror.StaticError(
		"--sarif is supported only for lint result output",
	)
)

const blockedExitCode = managedcapture.BlockedExitCode

type lintCLIConfig struct {
	stdout             io.Writer
	forFilesRaw        *string
	managedCaptureTool *string
	argvRaw            *string
	bundlePath         *string
	captureTool        *string
	codeIntel          *bool
	command            *string
	consumerRoot       *string
	cwd                *string
	ethosRoot          *string
	explain            *bool
	filesFrom          *string
	filesRaw           *string
	forFilesFrom       *string
	repoConfig         *string
	repoEthos          *string
	traceRoot          *string
	analyzeLog         *bool
	listCapturedTools  *bool
	installShims       *bool
	logDir             *string
	logOutput          *bool
	invocationCwd      *string
	replayTrace        *string
	runner             *string
	sarifCategory      *string
	sarifOutput        *bool
	scope              *scopeFlag
	toolPath           *string
	toolsBinDir        *string
	outputFormat       string
}

func Run(args []string) int {
	return RunWithWriter(args, os.Stdout)
}

func RunWithWriter(args []string, stdout io.Writer) int {
	return runCLIWithWriter(args, stdout)
}

func runCLIWithWriter(args []string, stdout io.Writer) int {
	flags := flag.NewFlagSet("coding-ethos-lint", flag.ExitOnError)
	config := registerLintFlags(flags)
	jsonOutput := flags.Bool("json", false, "Emit JSON output")
	sarifOutput := flags.Bool("sarif", false, "Emit SARIF output")
	config.sarifCategory = flags.String(
		"sarif-category",
		"",
		"GitHub code-scanning SARIF category",
	)
	config.analyzeLog = flags.Bool(
		"analyze-log",
		false,
		"Analyze persisted .coding-ethos/lint-runs traces",
	)
	config.explain = flags.Bool(
		"explain",
		false,
		"Explain selected lint checks without running them",
	)
	config.logDir = flags.String(
		"log-dir",
		"",
		"Lint trace directory for --analyze-log",
	)
	config.replayTrace = flags.String(
		"replay",
		"",
		"Replay a persisted .coding-ethos/lint-runs trace",
	)
	config.logOutput = flags.Bool(
		"log",
		true,
		"Persist normalized lint result under .coding-ethos/lint-runs",
	)
	config.codeIntel = flags.Bool(
		"code-intel",
		false,
		"Ingest the current lint trace and refresh changed-file code intelligence",
	)
	config.listCapturedTools = flags.Bool(
		"list-captured-tools",
		false,
		"Print captured lint tool names, one per line",
	)
	config.installShims = flags.Bool(
		"install-shims",
		false,
		"Install captured lint tool shims into --tools-bin-dir",
	)
	config.toolsBinDir = flags.String(
		"tools-bin-dir",
		"",
		"Directory for captured lint tool shims",
	)
	config.runner = flags.String("runner", "", "runner path for captured lint shims")
	config.toolPath = flags.String("tool-path", "", "Real tool path for --capture-tool")
	config.scope = scopeFlagSet(flags)

	err := flags.Parse(args)
	if err != nil {
		exitErr(err)
	}

	outputFormat, formatErr := lintOutputFormat(*jsonOutput, *sarifOutput)
	if formatErr != nil {
		exitErr(formatErr)
	}

	config.sarifOutput = sarifOutput
	config.outputFormat = outputFormat
	config.stdout = stdout

	return runParsedCLI(config, flags.Args())
}

func registerLintFlags(flags *flag.FlagSet) lintCLIConfig {
	return lintCLIConfig{
		argvRaw: flags.String(
			"argv",
			"",
			"Command argv to evaluate, separated by NUL when possible or spaces",
		),
		bundlePath: flags.String("bundle", "", "Path to policy-bundle.json"),
		captureTool: flags.String(
			"capture-tool",
			"",
			"Run and log a managed lint tool",
		),
		command:      flags.String("command", "", "Raw shell command to evaluate"),
		consumerRoot: flags.String("consumer-root", "", "consumer repository root"),
		cwd: flags.String(
			"cwd",
			"",
			"Working directory for git-state evaluators",
		),
		ethosRoot: flags.String("ethos-root", "", "coding-ethos checkout root"),
		repoConfig: flags.String(
			"repo-config",
			"",
			"consumer repo config path used to compile the bundle",
		),
		repoEthos: flags.String(
			"repo-ethos",
			"",
			"consumer repo ethos overlay used to compile the bundle",
		),
		filesFrom: registerFilesFromFlag(flags),
		filesRaw: flags.String(
			"files",
			"",
			"Comma-separated files for --scope files",
		),
		forFilesFrom:  registerForFilesFromFlag(flags),
		forFilesRaw:   registerForFilesFlag(flags),
		invocationCwd: registerInvocationCwdFlag(flags),
		managedCaptureTool: flags.String(
			"managed-capture-tool",
			"",
			"Run captured lint tool with Go-owned managed resolution",
		),
		traceRoot: flags.String(
			"trace-root",
			"",
			"Root directory for persisted lint traces",
		),
	}
}

func registerFilesFromFlag(flags *flag.FlagSet) *string {
	return flags.String(
		"files-from",
		"",
		"Newline-separated file list for --scope files",
	)
}

func registerForFilesFlag(flags *flag.FlagSet) *string {
	return flags.String(
		"for-files",
		"",
		"Comma-separated files to filter --analyze-log results",
	)
}

func registerForFilesFromFlag(flags *flag.FlagSet) *string {
	return flags.String(
		"for-files-from",
		"",
		"Newline-separated file list to filter --analyze-log results",
	)
}

func registerInvocationCwdFlag(flags *flag.FlagSet) *string {
	return flags.String(
		"invocation-cwd",
		"",
		"original command working directory",
	)
}

func runParsedCLI(config lintCLIConfig, args []string) int {
	ensureLintCWD(config.cwd)

	code, handled := runCaptureMode(config, args)
	if handled {
		return code
	}

	if runUtilityMode(config) {
		return 0
	}

	code, handled = runTraceMode(config)
	if handled {
		return code
	}

	bundle := validatedLintBundle(*config.bundlePath)
	if *config.explain {
		return runExplainMode(config, bundle)
	}

	return runLintMode(config, bundle)
}

func ensureLintCWD(cwd *string) {
	if strings.TrimSpace(*cwd) != "" {
		return
	}

	workingDir, err := os.Getwd()
	if err != nil {
		exitErr(err)
	}

	*cwd = workingDir
}

func runCaptureMode(config lintCLIConfig, args []string) (int, bool) {
	if *config.captureTool != "" {
		return managedcapture.Capture(managedcapture.CaptureOptions{
			Tool:          *config.captureTool,
			ToolPath:      *config.toolPath,
			Cwd:           *config.cwd,
			TraceRoot:     *config.traceRoot,
			Args:          args,
			OutputFormat:  config.outputFormat,
			PolicyContext: capturePolicyContext(*config.bundlePath),
			CodeIntel:     *config.codeIntel,
		}), true
	}

	if *config.managedCaptureTool == "" {
		return 0, false
	}

	return managedcapture.Run(managedcapture.Options{
		Tool:          *config.managedCaptureTool,
		EthosRoot:     *config.ethosRoot,
		ConsumerRoot:  *config.consumerRoot,
		InvocationCwd: *config.invocationCwd,
		Args:          args,
		OutputFormat:  config.outputFormat,
		PolicyContext: capturePolicyContext(*config.bundlePath),
		CodeIntel:     *config.codeIntel,
	}), true
}

func runUtilityMode(config lintCLIConfig) bool {
	if *config.listCapturedTools {
		printCapturedTools(config.stdout)

		return true
	}

	if !*config.installShims {
		return false
	}

	err := installCapturedToolShims(
		*config.toolsBinDir,
		*config.runner,
		*config.ethosRoot,
	)
	if err != nil {
		exitErr(err)
	}

	return true
}

func runTraceMode(config lintCLIConfig) (int, bool) {
	if *config.analyzeLog {
		return runAnalyzeLogMode(config), true
	}

	if *config.replayTrace != "" {
		return runReplayMode(config), true
	}

	return 0, false
}

func runAnalyzeLogMode(config lintCLIConfig) int {
	path := resolvedLogDir(*config.logDir, *config.cwd)

	forFiles, filesErr := filesFromInputs(*config.forFilesRaw, *config.forFilesFrom)
	if filesErr != nil {
		exitErr(filesErr)
	}

	analysis, analyzeErr := lint.AnalyzeTracesWithOptions(
		path,
		lint.AnalysisOptions{Files: forFiles},
	)
	if analyzeErr != nil {
		exitErr(analyzeErr)
	}

	if *config.sarifOutput {
		exitErr(errSARIFUnsupported)
	}

	format := selectedLintOutputFormat(config.outputFormat)

	encodeErr := lint.EncodeAnalysis(config.stdout, analysis, format)
	if encodeErr != nil {
		exitErr(encodeErr)
	}

	return 0
}

func resolvedLogDir(logDir, cwd string) string {
	if logDir != "" {
		return logDir
	}

	path, err := lint.DefaultTraceDir(cwd)
	if err != nil {
		exitErr(err)
	}

	return path
}

func runReplayMode(config lintCLIConfig) int {
	result, replayErr := lint.ReplayTrace(*config.replayTrace)
	if replayErr != nil {
		exitErr(replayErr)
	}

	format := selectedLintOutputFormat(config.outputFormat)

	encodeErr := encodeLintResult(
		config.stdout,
		result,
		format,
		*config.sarifCategory,
	)
	if encodeErr != nil {
		exitErr(encodeErr)
	}

	if result.Blocked() {
		return blockedExitCode
	}

	return 0
}

func validatedLintBundle(bundlePath string) policy.Bundle {
	if bundlePath == "" {
		exitErr(errBundleRequired)
	}

	bundle, err := readBundle(bundlePath)
	if err != nil {
		exitErr(err)
	}

	err = bundle.Validate()
	if err != nil {
		exitErr(
			fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)),
		)
	}

	return bundle
}

func runExplainMode(config lintCLIConfig, bundle policy.Bundle) int {
	files, filesErr := filesFromInputs(*config.filesRaw, *config.filesFrom)
	if filesErr != nil {
		exitErr(filesErr)
	}

	explainResult, explainErr := lint.ExplainWithOptions(bundle, lint.ExplainOptions{
		Scope: config.scope.Value(),
		Files: files,
	})
	if explainErr != nil {
		exitErr(explainErr)
	}

	if *config.sarifOutput {
		exitErr(errSARIFUnsupported)
	}

	format := selectedLintOutputFormat(config.outputFormat)

	encodeErr := lint.EncodeExplainResult(config.stdout, explainResult, format)
	if encodeErr != nil {
		exitErr(encodeErr)
	}

	return 0
}

func runLintMode(config lintCLIConfig, bundle policy.Bundle) int {
	files, emptyExplicitScope := resolveLintFiles(config)
	if emptyExplicitScope {
		encodeEmptyExplicitFileScope(config)

		return 0
	}

	result, err := lint.Run(bundle, lint.Options{
		Scope:   config.scope.Value(),
		Files:   files,
		Argv:    parseArgv(*config.argvRaw),
		Command: *config.command,
		Cwd:     *config.cwd,
	})
	if err != nil {
		exitErr(err)
	}

	if result.Blocked() {
		lint.EnsureTraceID(&result)
	}

	if *config.logOutput {
		tracePath := logLintResult(*config.cwd, result)
		if *config.codeIntel && tracePath != "" {
			refreshLintCodeIntel(*config.cwd, tracePath, config.scope.Value(), files)
		}
	}

	err = encodeLintResult(
		config.stdout,
		result,
		selectedLintOutputFormat(config.outputFormat),
		*config.sarifCategory,
	)
	if err != nil {
		exitErr(err)
	}

	if result.Blocked() {
		return blockedExitCode
	}

	return 0
}

func resolveLintFiles(config lintCLIConfig) ([]string, bool) {
	files, filesErr := filesFromInputs(*config.filesRaw, *config.filesFrom)
	if filesErr != nil {
		exitErr(filesErr)
	}

	if shouldReturnEmptyExplicitFileScope(
		config.scope.Value(),
		files,
		*config.filesRaw,
		*config.filesFrom,
	) {
		return files, true
	}

	if len(files) == 0 && config.scope.Value() == lint.ScopeStaged {
		var err error

		files, err = stagedFiles(*config.cwd)
		if err != nil {
			exitErr(err)
		}
	}

	if len(files) == 0 && config.scope.Value() == lint.ScopeChanged &&
		*config.codeIntel {
		var err error

		files, err = changedFiles(*config.cwd)
		if err != nil {
			writeLintCLIText(
				"warning: changed files not resolved for code-intel: " + err.Error(),
			)
		}
	}

	return files, false
}

func encodeEmptyExplicitFileScope(config lintCLIConfig) {
	result := lint.Result{
		Scope:  lint.ScopeFiles,
		Files:  []string{},
		Status: "resolved",
	}

	err := encodeLintResult(
		config.stdout,
		result,
		selectedLintOutputFormat(config.outputFormat),
		*config.sarifCategory,
	)
	if err != nil {
		exitErr(err)
	}
}

func logLintResult(cwd string, result lint.Result) string {
	tracePath, logErr := lint.LogResult(cwd, result)
	if logErr != nil {
		writeLintCLIText("warning: lint trace not written: " + logErr.Error())

		return ""
	}

	err := outputsurface.AutoPruneSurface(
		context.Background(),
		cwd,
		"lint_traces",
		false,
	)
	if err != nil {
		writeLintCLIText("warning: lint trace auto-prune failed: " + err.Error())
	}

	return tracePath
}

func refreshLintCodeIntel(root, tracePath, scope string, files []string) {
	ctx := context.Background()

	err := codeintel.IngestLintTraceFile(ctx, root, tracePath)
	if err != nil {
		writeLintCLIText("warning: lint trace not ingested into code-intel: " + err.Error())

		return
	}

	paths := lintCodeIntelPaths(scope, files)
	if len(paths) == 0 {
		return
	}

	_, err = codeintel.RefreshLintFiles(ctx, root, paths)
	if err != nil {
		writeLintCLIText("warning: lint code-intel refresh failed: " + err.Error())
	}
}

func lintCodeIntelPaths(scope string, files []string) []string {
	if scope == lint.ScopeFull {
		return []string{"."}
	}

	if scope != lint.ScopeFiles &&
		scope != lint.ScopeStaged &&
		scope != lint.ScopeChanged {
		return nil
	}

	return append([]string(nil), files...)
}

func encodeLintResult(
	writer io.Writer,
	result lint.Result,
	format string,
	sarifCategory string,
) error {
	if format != hookoutput.FormatSARIF || strings.TrimSpace(sarifCategory) == "" {
		err := hookoutput.EncodeLintResult(writer, result, format)
		if err != nil {
			return fmt.Errorf("encode lint result: %w", err)
		}

		return nil
	}

	output, err := hookoutput.FormatLintResultSARIFWithOptions(
		result,
		hookoutput.SARIFOptions{Category: sarifCategory},
	)
	if err != nil {
		return fmt.Errorf("format SARIF lint result: %w", err)
	}

	_, err = fmt.Fprintln(writer, output)
	if err != nil {
		return fmt.Errorf("write SARIF lint result: %w", err)
	}

	return nil
}

func printCapturedTools(writer io.Writer) {
	for _, tool := range toolcatalog.CapturedLintTools() {
		fmt.Fprintln(writer, tool.Name)
	}
}

func lintOutputFormat(jsonOutput, sarifOutput bool) (string, error) {
	if jsonOutput && sarifOutput {
		return "", errOutputFormatConflict
	}

	if jsonOutput {
		return hookoutput.FormatJSON, nil
	}

	if sarifOutput {
		return hookoutput.FormatSARIF, nil
	}

	return "", nil
}

func selectedLintOutputFormat(format string) string {
	if format != "" {
		return format
	}

	return hookoutput.SelectedFormat()
}

func shouldReturnEmptyExplicitFileScope(
	scope string,
	files []string,
	filesRaw string,
	filesFrom string,
) bool {
	return scope == lint.ScopeFiles &&
		len(files) == 0 &&
		(strings.TrimSpace(filesRaw) != "" || strings.TrimSpace(filesFrom) != "")
}

type capturePolicyData = managedcapture.PolicyContext

func capturePolicyContext(bundlePath string) capturePolicyData {
	if strings.TrimSpace(bundlePath) == "" {
		return capturePolicyData{}
	}

	bundle, err := readBundle(bundlePath)
	if err != nil {
		exitErr(err)
	}

	err = bundle.Validate()
	if err != nil {
		exitErr(
			fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)),
		)
	}

	return capturePolicyData{
		EvidenceMaps: bundle.EvidenceMaps,
		Policies:     capturePolicies(bundle.Policies),
		Skills:       bundle.Skills,
	}
}

func capturePolicies(policies map[string]policy.Policy) []policy.Policy {
	items := make([]policy.Policy, 0, len(policies))
	for _, policyDef := range policies {
		if len(policyDef.AppliesTo.Tools) == 0 {
			continue
		}

		for _, evaluator := range policyDef.Evaluators {
			if evaluator.Name == "cel.expression" {
				items = append(items, policyDef)

				break
			}
		}
	}

	return items
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}

	return bundle, nil
}

func parseFiles(raw string) []string {
	if raw == "" {
		return nil
	}

	parts := strings.Split(raw, ",")

	files := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}

	return files
}

func filesFromInputs(raw, path string) ([]string, error) {
	files := parseFiles(raw)
	if strings.TrimSpace(path) == "" {
		return files, nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read file list %s: %w", path, err)
	}

	return append(files, parseFileListLines(string(content))...), nil
}

func parseFileListLines(raw string) []string {
	lines := strings.Split(raw, "\n")

	files := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}

	return files
}

func stagedFiles(cwd string) ([]string, error) {
	output, err := stagedGitOutput(cwd)
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}

	return splitNonEmpty(string(output), "\n"), nil
}

func changedFiles(cwd string) ([]string, error) {
	output, err := changedGitOutput(cwd)
	if err != nil {
		return nil, fmt.Errorf("list changed files: %w", err)
	}

	return splitNonEmpty(string(output), "\n"), nil
}

func stagedGitOutput(cwd string) ([]byte, error) {
	output, err := evaluators.GitCommand(
		cwd,
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
		"--",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("run git staged-file query: %w", err)
	}

	return output, nil
}

func changedGitOutput(cwd string) ([]byte, error) {
	output, err := evaluators.GitCommand(
		cwd,
		"diff",
		"--name-only",
		"--diff-filter=ACMR",
		"--",
	).Output()
	if err != nil {
		return nil, fmt.Errorf("run git changed-file query: %w", err)
	}

	return output, nil
}

func parseArgv(raw string) []string {
	if raw == "" {
		return nil
	}

	if strings.Contains(raw, "\x00") {
		return splitNonEmpty(raw, "\x00")
	}

	return strings.Fields(raw)
}

func splitNonEmpty(raw, separator string) []string {
	parts := strings.Split(raw, separator)

	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			items = append(items, part)
		}
	}

	return items
}

func exitErr(err error) {
	feedback.Emit(
		os.Stderr,
		feedback.Error{Message: err.Error()},
		feedback.FormatTOON,
	)
	os.Exit(1)
}

func writeLintCLIText(text string) {
	feedback.Emit(os.Stderr, feedback.Text{Text: text}, feedback.FormatTOON)
}

type scopeFlag struct {
	value string
}

func scopeFlagSet(flags *flag.FlagSet) *scopeFlag {
	scope := &scopeFlag{value: lint.ScopeFiles}
	flags.Var(
		scope,
		"scope",
		"Lint scope: files, changed, staged, smoke, full, cutover, commit-msg",
	)
	flags.BoolFunc("changed", "Use changed-file lint scope", func(string) error {
		scope.value = lint.ScopeChanged

		return nil
	})
	flags.BoolFunc("staged", "Use staged lint scope", func(string) error {
		scope.value = lint.ScopeStaged

		return nil
	})
	flags.BoolFunc("smoke", "Use smoke lint scope", func(string) error {
		scope.value = lint.ScopeSmoke

		return nil
	})
	flags.BoolFunc("full", "Use full lint scope", func(string) error {
		scope.value = lint.ScopeFull

		return nil
	})

	return scope
}

func (flag *scopeFlag) String() string {
	return flag.value
}

func (flag *scopeFlag) Set(value string) error {
	flag.value = value

	return nil
}

func (flag *scopeFlag) Value() string {
	return flag.value
}
