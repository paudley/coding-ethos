// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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
