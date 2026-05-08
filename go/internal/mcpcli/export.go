// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package mcpcli

import "io"

// Run executes the MCP server CLI command family.
func Run(args []string, stdin io.Reader, stdout io.Writer) error {
	return runWithIO(args, stdin, stdout)
}
