// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/hooklogcli"
)

type exitCoder interface {
	ExitCode() int
}

func main() {
	execguard.Enter("coding-ethos-hook-log")

	status := 0

	err := hooklogcli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		var exitErr exitCoder
		if errors.As(err, &exitErr) {
			status = exitErr.ExitCode()
		} else {
			status = 1

			fmt.Fprintf(os.Stderr, "%s\n", err)
		}
	}

	os.Exit(status)
}
