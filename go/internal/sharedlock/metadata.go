// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package sharedlock validates the narrow external lock-directory capability.
package sharedlock

import (
	"os"
	"path/filepath"
)

// ValidDirectoryMetadata reports whether path and info describe the exact
// direct-child, non-symlink, mode-1777 /var/tmp capability used for shared locks.
func ValidDirectoryMetadata(path string, info os.FileInfo) bool {
	mode := info.Mode()

	return filepath.IsAbs(path) && filepath.Dir(path) == "/var/tmp" &&
		info.IsDir() && mode&os.ModeSymlink == 0 &&
		mode.Perm() == 0o777 && mode&os.ModeSticky != 0
}
