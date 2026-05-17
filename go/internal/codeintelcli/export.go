// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import "context"

// Run executes the code-intelligence CLI command family.
func Run(ctx context.Context, args []string) error {
	return run(ctx, args)
}
