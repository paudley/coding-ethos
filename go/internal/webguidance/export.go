// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package webguidance

// Error sentinels exposed for command and MCP callers.
var (
	ErrModernWebGuidanceDisabled = errModernWebGuidanceDisabled
	ErrNoCachedModernWebGuidance = errNoCachedModernWebGuidance
	ErrNetworkRefreshDisabled    = errNetworkRefreshDisabled
	ErrModernWebQueryRequired    = errModernWebQueryRequired
	ErrModernWebIDsRequired      = errModernWebIDsRequired
)
