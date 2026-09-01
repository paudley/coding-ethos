// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package sandboxexec

import "errors"

var errNativeGitBindProbeUnsupported = errors.New(
	"native git bind probe requires Linux mount namespaces",
)

func applyFilesystemPolicy(options options) error {
	return nil
}

func runNativeGitBindProbe() error {
	return errNativeGitBindProbeUnsupported
}

// ReadOnlyMountInfoForPath is false where Linux mountinfo is unavailable.
func ReadOnlyMountInfoForPath(_, _ string) bool {
	return false
}
