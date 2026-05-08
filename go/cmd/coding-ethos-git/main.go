// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policygitcli"
)

func main() {
	execguard.Enter("coding-ethos-git")

	err := policygitcli.Run(os.Args[1:])
	if err == nil {
		return
	}

	var exitError gitwrap.ExitCodeError
	if errors.As(err, &exitError) {
		os.Exit(exitError.Code)
	}

	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}
