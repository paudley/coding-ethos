// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooklogcli

import (
	"io"
)

// Run executes the hook-log CLI command family.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	return runWithIO(args, stdin, stdout, stderr)
}
