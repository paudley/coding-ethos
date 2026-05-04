// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "blackcat.ca/coding-ethos/go/internal/policy"

func newDenialTrackingID(event Event, decisions []policy.Decision) string {
	return hookTraceID(event, Result{
		Event:     event.HookEventName,
		Provider:  event.Provider(),
		Status:    statusBlocked,
		Tool:      event.ToolName,
		Decisions: decisions,
	})
}
