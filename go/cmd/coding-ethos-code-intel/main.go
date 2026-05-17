// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"

	"blackcat.ca/coding-ethos/go/internal/codeintelcli"
	"blackcat.ca/coding-ethos/go/internal/execguard"
)

func main() {
	execguard.Enter("coding-ethos-code-intel")

	err := codeintelcli.Run(context.Background(), os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}
}
