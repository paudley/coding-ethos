// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcpcli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/mcp"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

var errBundleRequired = apperror.StaticError("--bundle is required")

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

	inlineErr0 := flags.Parse(args)
	if inlineErr0 != nil {
		return fmt.Errorf("parse flags: %w", inlineErr0)
	}

	if *bundlePath == "" {
		return errBundleRequired
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}

	inlineErr1 := bundle.Validate()
	if inlineErr1 != nil {
		return apperror.Wrapf(
			apperror.StaticError("invalid policy bundle:\n%s"),
			"invalid policy bundle:\n%s",
			policy.FormatValidationError(inlineErr1),
		)
	}

	err = mcp.NewServerWithRuntime(bundle, mcp.Runtime{
		BundlePath:    *bundlePath,
		EthosRoot:     *ethosRoot,
		ConsumerRoot:  *consumerRoot,
		InvocationCwd: *invocationCwd,
	}).Serve(stdin, stdout)
	if err != nil {
		return fmt.Errorf("serve MCP protocol: %w", err)
	}

	return nil
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("decode policy bundle: %w", err)
	}

	return bundle, nil
}
