// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

var (
	errBundleRequired = errors.New("--bundle is required")
	errInvalidBundle  = errors.New("invalid policy bundle")
)

const blockedExitCode = 2

func main() {
	flags := flag.NewFlagSet("coding-ethos-lint", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	filesRaw := flags.String("files", "", "Comma-separated files for --scope files")
	argvRaw := flags.String(
		"argv",
		"",
		"Command argv to evaluate, separated by NUL when possible or spaces",
	)
	command := flags.String("command", "", "Raw shell command to evaluate")
	captureTool := flags.String("capture-tool", "", "Run and log a managed lint tool")
	cwd := flags.String("cwd", "", "Working directory for git-state evaluators")
	jsonOutput := flags.Bool("json", false, "Emit JSON output")
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
	logOutput := flags.Bool(
		"log",
		true,
		"Persist normalized lint result under .coding-ethos/lint-runs",
	)
	toolPath := flags.String("tool-path", "", "Real tool path for --capture-tool")
	scope := scopeFlagSet(flags)

	err := flags.Parse(os.Args[1:])
	if err != nil {
		exitErr(err)
	}

	if *captureTool != "" {
		os.Exit(runCapturedTool(*captureTool, *toolPath, *cwd, flags.Args()))
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

		analysis, analyzeErr := lint.AnalyzeTraces(path)
		if analyzeErr != nil {
			exitErr(analyzeErr)
		}
		if encodeErr := lint.EncodeAnalysis(os.Stdout, analysis, *jsonOutput); encodeErr != nil {
			exitErr(encodeErr)
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
		explainResult, explainErr := lint.Explain(bundle, scope.Value())
		if explainErr != nil {
			exitErr(explainErr)
		}
		if encodeErr := lint.EncodeExplainResult(
			os.Stdout,
			explainResult,
			*jsonOutput,
		); encodeErr != nil {
			exitErr(encodeErr)
		}

		return
	}

	result, err := lint.Run(bundle, lint.Options{
		Scope:   scope.Value(),
		Files:   parseFiles(*filesRaw),
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

	if *jsonOutput {
		err = hookoutput.EncodeLintResult(os.Stdout, result, hookoutput.FormatJSON)
		if err != nil {
			exitErr(err)
		}
	} else {
		err = hookoutput.EncodeLintResult(os.Stdout, result, hookoutput.FormatHuman)
		if err != nil {
			exitErr(err)
		}
	}

	if result.Blocked() {
		os.Exit(blockedExitCode)
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
