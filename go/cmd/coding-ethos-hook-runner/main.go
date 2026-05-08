// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/hookrunnercli"
)

func main() {
	execguard.Enter("coding-ethos-hook-runner")
	os.Exit(hookrunnercli.Run(os.Args[1:]))
}
