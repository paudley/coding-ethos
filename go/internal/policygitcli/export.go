// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policygitcli

// Run executes the policy-protected git command family.
func Run(args []string) error {
	return runWithArgs(args)
}
