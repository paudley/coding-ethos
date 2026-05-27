//go:build windows

// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import "blackcat.ca/coding-ethos/go/internal/apperror"

func duckDBRebuildLockPIDStale(_ int) (bool, error) {
	return false, apperror.StaticError("pid liveness is unavailable on Windows")
}
