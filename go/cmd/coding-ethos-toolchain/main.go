// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/toolchaincli"
)

func main() {
	execguard.Enter("coding-ethos-toolchain")
	os.Exit(toolchaincli.Run(os.Args[1:]))
}
