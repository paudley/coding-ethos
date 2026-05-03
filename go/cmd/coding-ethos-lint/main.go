// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

var (
	errBundleRequired       = errors.New("--bundle is required")
	errInvalidBundle        = errors.New("invalid policy bundle")
	errOutputFormatConflict = errors.New("--json and --sarif are mutually exclusive")
	errSARIFUnsupported     = errors.New("--sarif is supported only for lint result output")
)

const blockedExitCode = 2

func main() {
	flags := flag.NewFlagSet("coding-ethos-lint", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	filesRaw := flags.String("files", "", "Comma-separated files for --scope files")
	filesFrom := flags.String("files-from", "", "Newline-separated file list for --scope files")
	forFilesRaw := flags.String(
		"for-files",
		"",
		"Comma-separated files to filter --analyze-log results",
	)
	forFilesFrom := flags.String(
		"for-files-from",
		"",
		"Newline-separated file list to filter --analyze-log results",
	)
	argvRaw := flags.String(
		"argv",
		"",
		"Command argv to evaluate, separated by NUL when possible or spaces",
	)
	command := flags.String("command", "", "Raw shell command to evaluate")
	captureTool := flags.String("capture-tool", "", "Run and log a managed lint tool")
	managedCaptureTool := flags.String(
		"managed-capture-tool",
		"",
		"Run captured lint tool with Go-owned managed resolution",
	)
	ethosRoot := flags.String("ethos-root", "", "coding-ethos checkout root")
	consumerRoot := flags.String("consumer-root", "", "consumer repository root")
	invocationCwd := flags.String("invocation-cwd", "", "original command working directory")
	cwd := flags.String("cwd", "", "Working directory for git-state evaluators")
	traceRoot := flags.String("trace-root", "", "Root directory for persisted lint traces")
	jsonOutput := flags.Bool("json", false, "Emit JSON output")
	sarifOutput := flags.Bool("sarif", false, "Emit SARIF output")
	sarifCategory := flags.String(
		"sarif-category",
		"",
		"GitHub code-scanning SARIF category",
	)
	analyzeLog := flags.Bool(
		"analyze-log",
		false,
		"Analyze persisted .coding-ethos/lint-runs traces",
	)
	explain := flags.Bool("explain", false, "Explain selected lint checks without running them")
	logDir := flags.String(
		"log-dir",
		"",
		"Lint trace directory for --analyze-log",
	)
	replayTrace := flags.String(
		"replay",
		"",
		"Replay a persisted .coding-ethos/lint-runs trace",
	)
	logOutput := flags.Bool(
		"log",
		true,
		"Persist normalized lint result under .coding-ethos/lint-runs",
	)
	listCapturedTools := flags.Bool(
		"list-captured-tools",
		false,
		"Print captured lint tool names, one per line",
	)
	installShims := flags.Bool(
		"install-shims",
		false,
		"Install captured lint tool shims into --tools-bin-dir",
	)
	toolsBinDir := flags.String("tools-bin-dir", "", "Directory for captured lint tool shims")
	runner := flags.String("runner", "", "runner path for captured lint shims")
	toolPath := flags.String("tool-path", "", "Real tool path for --capture-tool")
	sandboxMode := flags.String(
		"sandbox-mode",
		"off",
		"Managed tool sandbox mode: off, auto, or required",
	)
	scope := scopeFlagSet(flags)

	err := flags.Parse(os.Args[1:])
	if err != nil {
		exitErr(err)
	}
	outputFormat, formatErr := lintOutputFormat(*jsonOutput, *sarifOutput)
	if formatErr != nil {
		exitErr(formatErr)
	}
	if strings.TrimSpace(*cwd) == "" {
		workingDir, cwdErr := os.Getwd()
		if cwdErr != nil {
			exitErr(cwdErr)
		}
		*cwd = workingDir
	}

	if *captureTool != "" {
		os.Exit(runCapturedTool(
			*captureTool,
			*toolPath,
			*cwd,
			*traceRoot,
			flags.Args(),
			capturePolicyContext(*bundlePath),
			outputFormat,
		))
	}

	if *managedCaptureTool != "" {
		os.Exit(runManagedCapture(managedCaptureOptions{
			Tool:          *managedCaptureTool,
			EthosRoot:     *ethosRoot,
			ConsumerRoot:  *consumerRoot,
			InvocationCwd: *invocationCwd,
			Args:          flags.Args(),
			SandboxMode:   *sandboxMode,
			OutputFormat:  outputFormat,
			PolicyContext: capturePolicyContext(*bundlePath),
		}))
	}

	if *listCapturedTools {
		printCapturedTools()
		return
	}

	if *installShims {
		if err := installCapturedToolShims(*toolsBinDir, *runner, *ethosRoot); err != nil {
			exitErr(err)
		}
		return
	}

	if *analyzeLog {
		path := *logDir
		if path == "" {
			var pathErr error
			path, pathErr = lint.DefaultTraceDir(*cwd)
			if pathErr != nil {
				exitErr(pathErr)
			}
		}

		forFiles, filesErr := filesFromInputs(*forFilesRaw, *forFilesFrom)
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
		if *sarifOutput {
			exitErr(errSARIFUnsupported)
		}
		format := selectedLintOutputFormat(outputFormat)
		if encodeErr := lint.EncodeAnalysis(os.Stdout, analysis, format); encodeErr != nil {
			exitErr(encodeErr)
		}

		return
	}

	if *replayTrace != "" {
		result, replayErr := lint.ReplayTrace(*replayTrace)
		if replayErr != nil {
			exitErr(replayErr)
		}
		format := selectedLintOutputFormat(outputFormat)
		if encodeErr := encodeLintResult(os.Stdout, result, format, *sarifCategory); encodeErr != nil {
			exitErr(encodeErr)
		}
		if result.Blocked() {
			os.Exit(blockedExitCode)
		}

		return
	}

	if *bundlePath == "" {
		exitErr(errBundleRequired)
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		exitErr(err)
	}

	err = bundle.Validate()
	if err != nil {
		exitErr(
			fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)),
		)
	}

	if *explain {
		files, filesErr := filesFromInputs(*filesRaw, *filesFrom)
		if filesErr != nil {
			exitErr(filesErr)
		}

		explainResult, explainErr := lint.ExplainWithOptions(bundle, lint.ExplainOptions{
			Scope: scope.Value(),
			Files: files,
		})
		if explainErr != nil {
			exitErr(explainErr)
		}
		if *sarifOutput {
			exitErr(errSARIFUnsupported)
		}
		format := selectedLintOutputFormat(outputFormat)
		if encodeErr := lint.EncodeExplainResult(
			os.Stdout,
			explainResult,
			format,
		); encodeErr != nil {
			exitErr(encodeErr)
		}

		return
	}

	files, filesErr := filesFromInputs(*filesRaw, *filesFrom)
	if filesErr != nil {
		exitErr(filesErr)
	}
	if shouldReturnEmptyExplicitFileScope(scope.Value(), files, *filesRaw, *filesFrom) {
		result := lint.Result{
			Scope:  lint.ScopeFiles,
			Files:  []string{},
			Status: "resolved",
		}
		err = encodeLintResult(
			os.Stdout,
			result,
			selectedLintOutputFormat(outputFormat),
			*sarifCategory,
		)
		if err != nil {
			exitErr(err)
		}

		return
	}
	if len(files) == 0 && scope.Value() == lint.ScopeStaged {
		files, err = stagedFiles(*cwd)
		if err != nil {
			exitErr(err)
		}
	}

	result, err := lint.Run(bundle, lint.Options{
		Scope:   scope.Value(),
		Files:   files,
		Argv:    parseArgv(*argvRaw),
		Command: *command,
		Cwd:     *cwd,
	})
	if err != nil {
		exitErr(err)
	}

	if *logOutput {
		if _, logErr := lint.LogResult(*cwd, result); logErr != nil {
			fmt.Fprintf(os.Stderr, "WARN: lint trace not written: %v\n", logErr)
		}
	}

	err = encodeLintResult(
		os.Stdout,
		result,
		selectedLintOutputFormat(outputFormat),
		*sarifCategory,
	)
	if err != nil {
		exitErr(err)
	}

	if result.Blocked() {
		os.Exit(blockedExitCode)
	}
}

func encodeLintResult(
	writer *os.File,
	result lint.Result,
	format string,
	sarifCategory string,
) error {
	if format != hookoutput.FormatSARIF || strings.TrimSpace(sarifCategory) == "" {
		return hookoutput.EncodeLintResult(writer, result, format)
	}

	output, err := hookoutput.FormatLintResultSARIFWithOptions(
		result,
		hookoutput.SARIFOptions{Category: sarifCategory},
	)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(writer, output)

	return err
}

func printCapturedTools() {
	for _, tool := range toolcatalog.CapturedLintTools() {
		fmt.Fprintln(os.Stdout, tool.Name)
	}
}

func lintOutputFormat(jsonOutput bool, sarifOutput bool) (string, error) {
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

type capturePolicyData struct {
	EvidenceMaps []diagnostics.EvidenceMap
	Skills       map[string]policy.Skill
}

func capturePolicyContext(bundlePath string) capturePolicyData {
	if strings.TrimSpace(bundlePath) == "" {
		return capturePolicyData{}
	}

	bundle, err := readBundle(bundlePath)
	if err != nil {
		exitErr(err)
	}

	if err := bundle.Validate(); err != nil {
		exitErr(
			fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)),
		)
	}

	return capturePolicyData{
		EvidenceMaps: bundle.EvidenceMaps,
		Skills:       bundle.Skills,
	}
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

func filesFromInputs(raw string, path string) ([]string, error) {
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
	command := exec.CommandContext(
		context.Background(),
		"git",
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
		"--",
	)
	if strings.TrimSpace(cwd) != "" {
		command.Dir = cwd
	}

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}

	return splitNonEmpty(string(output), "\n"), nil
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

func splitNonEmpty(raw string, separator string) []string {
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
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}

func exitBlockedErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(blockedExitCode)
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
