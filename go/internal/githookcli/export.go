// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package githookcli

// Run executes the git hook CLI command family.
func Run(args []string) int {
	return runWithArgs(args)
}
