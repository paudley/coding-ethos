// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/sandboxexec"
)

func main() {
	execguard.Enter("coding-ethos-sandbox")
	os.Exit(sandboxexec.Run(os.Args[1:]))
}
