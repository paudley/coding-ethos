// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/agenthookscli"
	"blackcat.ca/coding-ethos/go/internal/execguard"
)

func main() {
	execguard.Enter("coding-ethos-agent-hooks")
	os.Exit(agenthookscli.Run(os.Args[1:]))
}
