// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func main() {
	flags := flag.NewFlagSet("coding-ethos-git", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	realGit := flags.String("real-git", "git", "Real git executable")
	checkOnly := flags.Bool("check-only", false, "Check policy without executing git")
	jsonOutput := flags.Bool("json", false, "Emit JSON result")
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

	argv := flags.Args()
	cwd, err := os.Getwd()
	if err != nil {
		exitErr(fmt.Errorf("get cwd: %w", err))
	}
	result, err := gitwrap.Check(bundle, gitwrap.Options{Argv: argv, Cwd: cwd})
	if err != nil {
		exitErr(err)
	}
	if *jsonOutput {
		if err := gitwrap.EncodeResult(os.Stdout, result); err != nil {
			exitErr(err)
		}
	}
	if result.Blocked() {
		printBlocked(result)
		os.Exit(2)
	}
	if *checkOnly {
		if !*jsonOutput {
			fmt.Fprintln(os.Stdout, "git policy check allowed")
		}
		return
	}

	if err := gitwrap.Execute(*realGit, argv); err != nil {
		var exitError gitwrap.ExitCodeError
		if errors.As(err, &exitError) {
			os.Exit(exitError.Code)
		}
		exitErr(err)
	}
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()
	return policy.DecodeBundle(file)
}

func printBlocked(result gitwrap.Result) {
	for _, decision := range result.Decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			fmt.Fprintf(os.Stderr, "[coding-ethos:%s] %s\n", decision.PolicyID, decision.Message)
			if decision.Suggestion != "" {
				fmt.Fprintf(os.Stderr, "Suggestion: %s\n", decision.Suggestion)
			}
		}
	}
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}
