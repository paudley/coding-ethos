// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package githookcli

// Run executes the git hook CLI command family.
func Run(args []string) int {
	return runWithArgs(args)
}
