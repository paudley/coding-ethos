// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func main() {
	flags := flag.NewFlagSet("coding-ethos-lint", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	filesRaw := flags.String("files", "", "Comma-separated files for --scope files")
	jsonOutput := flags.Bool("json", false, "Emit JSON output")
	scope := scopeFlagSet(flags)
	if err := flags.Parse(os.Args[1:]); err != nil {
		exitErr(err)
	}
	if *bundlePath == "" {
		exitErr(fmt.Errorf("--bundle is required"))
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		exitErr(err)
	}
	if err := bundle.Validate(); err != nil {
		exitErr(fmt.Errorf("invalid policy bundle:\n%s", policy.FormatValidationError(err)))
	}

	result, err := lint.Run(bundle, lint.Options{
		Scope: scope.Value(),
		Files: parseFiles(*filesRaw),
	})
	if err != nil {
		exitErr(err)
	}

	if *jsonOutput {
		if err := lint.EncodeResult(os.Stdout, result); err != nil {
			exitErr(err)
		}
		return
	}
	printHuman(result)
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()
	return policy.DecodeBundle(file)
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

func printHuman(result lint.Result) {
	fmt.Printf("coding-ethos lint scope: %s\n", result.Scope)
	fmt.Printf("status: %s\n", result.Status)
	fmt.Printf("policies: %d\n", len(result.Decisions))
	for _, decision := range result.Decisions {
		fmt.Printf("- %s [%s]: %s\n", decision.PolicyID, decision.Severity, decision.Message)
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
