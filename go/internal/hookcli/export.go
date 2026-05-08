// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookcli

import "io"

// Run executes the agent hook CLI command family.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithIO(args, stdin, stdout, stderr)
}
