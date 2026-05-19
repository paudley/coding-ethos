// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !linux

package sandbox

const (
	nativeNamespaceUnsupportedReason = "Linux namespaces are unavailable on this platform"
	nestedProcessPolicyReason        = "Linux process sandboxing is unavailable on this platform"
)

// ValidateNativeRuntime records best-available non-Linux behavior.
func ValidateNativeRuntime() (Evidence, error) {
	return Evidence{
		Backend: BackendNative,
		Enabled: false,
		Reason:  "Linux namespaces are unavailable on this platform",
	}, nil
}

func nativeNestedProcessPolicyRestricted() bool {
	return false
}
