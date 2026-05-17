// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/lintcli"
)

func main() {
	execguard.Enter("coding-ethos-lint")
	os.Exit(lintcli.Run(os.Args[1:]))
}
