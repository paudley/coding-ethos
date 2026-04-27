// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

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
	cwd := flags.String("cwd", "", "Working directory for git-state evaluators")
	jsonOutput := flags.Bool("json", false, "Emit JSON output")
	scope := scopeFlagSet(flags)

	err := flags.Parse(os.Args[1:])
	if err != nil {
		exitErr(err)
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

	if *jsonOutput {
		err = lint.EncodeResult(os.Stdout, result)
		if err != nil {
			exitErr(err)
		}
	} else {
		printHuman(result)
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

func printHuman(result lint.Result) {
	fmt.Fprintf(os.Stdout, "coding-ethos lint scope: %s\n", result.Scope)
	fmt.Fprintf(os.Stdout, "status: %s\n", result.Status)
	fmt.Fprintf(os.Stdout, "policies: %d\n", len(result.Decisions))

	for _, decision := range result.Decisions {
		fmt.Fprintf(
			os.Stdout,
			"- %s [%s]: %s\n",
			decision.PolicyID,
			decision.Severity,
			decision.Message,
		)
	}
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
	flags.Var(scope, "scope", "Lint scope: files, changed, staged, smoke, full")
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
