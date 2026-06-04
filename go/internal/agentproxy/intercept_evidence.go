// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agentproxy

// Interception mode values for the opt-in HTTPS-interception gate.
const (
	// InterceptionModeOff disables HTTPS interception entirely.
	InterceptionModeOff = "off"
	// InterceptionModeRequired requests opt-in HTTPS interception.
	InterceptionModeRequired = "required"
)

// InterceptionEvidence records the decision of the opt-in HTTPS-interception
// gate, including which CA backs the decision when interception is enabled.
type InterceptionEvidence struct {
	// Mode is the configured interception mode that produced this decision.
	Mode string `json:"mode"`
	// Reason explains why interception was enabled, disabled, or denied.
	Reason string `json:"reason"`
	// CAFingerprint is the SHA-256 fingerprint of the provisioned CA, if any.
	CAFingerprint string `json:"ca_fingerprint,omitempty"`
	// CACertPath is the path to the provisioned CA certificate, if any.
	CACertPath string `json:"ca_cert_path,omitempty"`
	// Enabled reports whether HTTPS interception is active.
	Enabled bool `json:"enabled"`
	// Denied reports whether interception was explicitly fail-closed denied.
	Denied bool `json:"denied"`
}
