// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedExitCode = 2

var (
	errBundleRequired = errors.New("--bundle is required")
	errInvalidBundle  = errors.New("invalid policy bundle")
)

func main() {
	os.Exit(runWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func runWithIO(args []string, stdin io.Reader, stdout io.Writer, stderr io.Writer) int {
	flags := flag.NewFlagSet("coding-ethos-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	jsonOutput := flags.Bool("json", false, "Emit JSON result to stdout")

	err := flags.Parse(args)
	if err != nil {
		printErr(stderr, err)
		return 1
	}

	if *bundlePath == "" {
		printErr(stderr, errBundleRequired)
		return 1
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		printErr(stderr, err)
		return 1
	}

	err = bundle.Validate()
	if err != nil {
		printErr(stderr, fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)))
		return 1
	}

	event, err := hooks.DecodeEvent(stdin)
	if err != nil {
		printErr(stderr, err)
		return 1
	}

	startedAt := time.Now()
	result, err := hooks.Run(bundle, hooks.Options{Event: event})
	if err != nil {
		printErr(stderr, err)
		return 1
	}
	result.RuntimeMS = time.Since(startedAt).Milliseconds()

	if err := hooks.WriteAgentHookTraceFromEnv(event, result); err != nil {
		printErr(stderr, err)
		return 1
	}

	if *jsonOutput {
		err = hooks.EncodeResult(stdout, result)
		if err != nil {
			printErr(stderr, err)
			return 1
		}
	}

	if result.Blocked() {
		printBlocked(stderr, result)
		return blockedExitCode
	}

	return 0
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

func printBlocked(writer io.Writer, result hooks.Result) {
	advice := hooks.BlockedAdvice(result)
	if result.Provider != "" {
		advice = hooks.ProviderBlockMessage(result)
	}
	if advice == "" {
		return
	}

	fmt.Fprintln(writer, advice)
}

func exitErr(err error) {
	printErr(os.Stderr, err)
	os.Exit(1)
}

func printErr(writer io.Writer, err error) {
	fmt.Fprintf(writer, "%s\n", err)
}
