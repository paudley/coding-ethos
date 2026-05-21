// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import "context"

func gitIgnoreMatcherActive(
	ctx context.Context,
	root string,
	allowedPaths map[string]bool,
	allowedDirs map[string]bool,
) bool {
	return gitWorkTreeAvailable(ctx, root) &&
		(len(allowedPaths) > 0 || len(allowedDirs) > 1)
}
