// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooklogcli

import (
	"flag"
	"fmt"
	"io"
	"os"

	"blackcat.ca/coding-ethos/go/internal/hooklog"
)

type exitCoder interface {
	ExitCode() int
}

func run() error {
	return runWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}

func runWithIO(
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
) error {
	flags := flag.NewFlagSet("coding-ethos-hook-log", flag.ExitOnError)
	root := flags.String("root", "", "Repository root for hook logs")
	bundleRoot := flags.String("bundle-root", "", "coding-ethos pre-commit bundle root")
	gitPath := flags.String(
		"git",
		"/usr/bin/git",
		"Git binary used for ignore validation",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	err = hooklog.Run(hooklog.Options{
		Stdin:      stdin,
		Stdout:     stdout,
		Stderr:     stderr,
		GitPath:    *gitPath,
		Root:       *root,
		BundleRoot: *bundleRoot,
		Command:    flags.Args(),
	})
	if err != nil {
		return fmt.Errorf("run hook log command: %w", err)
	}

	return nil
}
