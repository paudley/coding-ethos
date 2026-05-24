// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/feedback"
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

	feedback.Emit(
		os.Stderr,
		feedback.Error{Message: err.Error()},
		feedback.FormatTOON,
	)
	os.Exit(1)
}
