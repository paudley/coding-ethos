// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func main() {
	flags := flag.NewFlagSet("coding-ethos-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	jsonOutput := flags.Bool("json", false, "Emit JSON result to stdout")
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
	event, err := hooks.DecodeEvent(os.Stdin)
	if err != nil {
		exitErr(err)
	}
	result, err := hooks.Run(bundle, hooks.Options{Event: event})
	if err != nil {
		exitErr(err)
	}

	if *jsonOutput {
		if err := hooks.EncodeResult(os.Stdout, result); err != nil {
			exitErr(err)
		}
	}
	if result.Blocked() {
		printBlocked(result)
		os.Exit(2)
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

func printBlocked(result hooks.Result) {
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
