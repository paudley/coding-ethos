// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "blackcat.ca/coding-ethos/go/internal/policy"

const providerRewritePolicyID = "hook.provider_required"

type InspectionContext struct {
	Event              Event
	Provider           string
	AdminApproved      bool
	ReadOnlyInspection bool
	SkipNestedHook     bool
}

type InspectionRoute struct {
	UpdatedInput  map[string]any
	BlockPolicyID string
	Reason        string
	Block         bool
	Rewrite       bool
}

func collectInspectionContext(event Event) InspectionContext {
	adminApproved := adminApprovedForEvent(event)

	return InspectionContext{
		Event:              event,
		Provider:           event.Provider(),
		AdminApproved:      adminApproved,
		ReadOnlyInspection: readOnlyInspectionEvent(event, adminApproved),
		SkipNestedHook:     shouldSkipNestedCodexHook(event),
	}
}

type InspectionDecision struct {
	Route    InspectionRoute
	Policies []policy.Decision
	Status   string
}

func decideInspection(
	bundle policy.Bundle,
	ctx InspectionContext,
	policyDecisions []policy.Decision,
	route InspectionRoute,
) InspectionDecision {
	decisions := append([]policy.Decision(nil), policyDecisions...)
	if route.Rewrite && ctx.Provider == "" && resultStatus(decisions) != statusBlocked {
		decisions = append(decisions, routeBlockDecision(
			bundle,
			providerRewritePolicyID,
			"Hook rewrites require a known agent provider so the updated input can be emitted with the correct provider schema.",
		))
	}
	if route.Block && resultStatus(decisions) != statusBlocked {
		decisions = append(decisions, routeBlockDecision(bundle, route.BlockPolicyID, route.Reason))
	}

	status := resultStatus(decisions)
	if status == statusBlocked || (route.Rewrite && !providerSupportsUpdatedInput(ctx.Provider)) {
		route = InspectionRoute{}
	}

	return InspectionDecision{
		Route:    route,
		Policies: decisions,
		Status:   status,
	}
}

func providerSupportsUpdatedInput(provider string) bool {
	switch provider {
	case providerClaude, providerGemini:
		return true
	default:
		return false
	}
}

func (ctx InspectionContext) allowedResult() Result {
	return Result{
		Event:    ctx.Event.HookEventName,
		Provider: ctx.Provider,
		Tool:     ctx.Event.ToolName,
		Status:   statusAllowed,
	}
}
