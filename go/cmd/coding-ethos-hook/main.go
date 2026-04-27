// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedExitCode = 2

var (
	errBundleRequired = errors.New("--bundle is required")
	errInvalidBundle  = errors.New("invalid policy bundle")
)

func main() {
	flags := flag.NewFlagSet("coding-ethos-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	jsonOutput := flags.Bool("json", false, "Emit JSON result to stdout")

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

	event, err := hooks.DecodeEvent(os.Stdin)
	if err != nil {
		exitErr(err)
	}

	result, err := hooks.Run(bundle, hooks.Options{Event: event})
	if err != nil {
		exitErr(err)
	}

	if *jsonOutput {
		err = hooks.EncodeResult(os.Stdout, result)
		if err != nil {
			exitErr(err)
		}
	}

	if result.Blocked() {
		printBlocked(result)
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

func printBlocked(result hooks.Result) {
	advice := hooks.BlockedAdvice(result)
	if advice == "" {
		return
	}

	fmt.Fprintln(os.Stderr, advice)
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}
