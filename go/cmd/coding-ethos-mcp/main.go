// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/mcpcli"
)

func main() {
	execguard.Enter("coding-ethos-mcp")

	err := mcpcli.Run(os.Args[1:], os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
