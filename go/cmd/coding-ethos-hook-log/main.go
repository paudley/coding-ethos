// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/hooklog"
)

type exitCoder interface {
	ExitCode() int
}

func main() {
	status := 0
	if err := run(); err != nil {
		if exitErr, ok := err.(exitCoder); ok {
			status = exitErr.ExitCode()
		} else {
			status = 1
			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
	}
	os.Exit(status)
}

func run() error {
	flags := flag.NewFlagSet("coding-ethos-hook-log", flag.ExitOnError)
	root := flags.String("root", "", "Repository root for hook logs")
	bundleRoot := flags.String("bundle-root", "", "coding-ethos pre-commit bundle root")
	gitPath := flags.String("git", "/usr/bin/git", "Git binary used for ignore validation")

	if err := flags.Parse(os.Args[1:]); err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	return hooklog.Run(hooklog.Options{
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		GitPath:    *gitPath,
		Root:       *root,
		BundleRoot: *bundleRoot,
		Command:    flags.Args(),
	})
}
