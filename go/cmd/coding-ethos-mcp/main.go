// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"blackcat.ca/coding-ethos/go/internal/mcp"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

var errBundleRequired = errors.New("--bundle is required")

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}

func run() error {
	return runWithIO(os.Args[1:], os.Stdin, os.Stdout)
}

func runWithIO(args []string, stdin io.Reader, stdout io.Writer) error {
	flags := flag.NewFlagSet("coding-ethos-mcp", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	ethosRoot := flags.String("ethos-root", "", "coding-ethos checkout root")
	consumerRoot := flags.String("consumer-root", "", "consumer repository root")
	invocationCwd := flags.String(
		"invocation-cwd",
		"",
		"original command working directory",
	)
	lintBinary := flags.String("lint-binary", "", "Path to coding-ethos-lint")

	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *bundlePath == "" {
		return errBundleRequired
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}

	if err := bundle.Validate(); err != nil {
		return fmt.Errorf("invalid policy bundle:\n%s", policy.FormatValidationError(err))
	}

	return mcp.NewServerWithRuntime(bundle, mcp.Runtime{
		BundlePath:    *bundlePath,
		EthosRoot:     *ethosRoot,
		ConsumerRoot:  *consumerRoot,
		InvocationCwd: *invocationCwd,
		LintBinary:    *lintBinary,
	}).Serve(stdin, stdout)
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, err
	}

	return bundle, nil
}
