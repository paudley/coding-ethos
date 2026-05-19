// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package sandbox

// ValidateNativeRuntime records best-available non-Linux behavior.
func ValidateNativeRuntime() (Evidence, error) {
	return Evidence{
		Backend: BackendNative,
		Enabled: false,
		Reason:  "Linux namespaces are unavailable on this platform",
	}, nil
}
