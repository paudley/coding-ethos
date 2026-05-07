// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/hookcli"
)

func main() {
	execguard.Enter("coding-ethos-hook")
	os.Exit(hookcli.Run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
