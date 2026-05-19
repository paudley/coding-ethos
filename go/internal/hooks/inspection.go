// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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
	UpdatedInput       map[string]any
	BlockPolicyID      string
	Reason             string
	RemediationCommand string
	Block              bool
	Rewrite            bool
}

func collectInspectionContext(
	event Event,
	adminApprovedForCWD func(string) bool,
) InspectionContext {
	return CollectInspectionContext(event, adminApprovedForCWD)
}

// CollectInspectionContext annotates a hook event with provider, admin, and
// routing context before policy evaluation.
func CollectInspectionContext(
	event Event,
	adminApprovedForCWD func(string) bool,
) InspectionContext {
	if adminApprovedForCWD == nil {
		adminApprovedForCWD = defaultAdminApprovedForCWD
	}

	adminApproved := adminApprovedForCWD(event.Cwd)

	return InspectionContext{
		Event:              event,
		Provider:           event.Provider(),
		AdminApproved:      adminApproved,
		ReadOnlyInspection: readOnlyInspectionEvent(event, adminApproved),
		SkipNestedHook:     shouldSkipNestedCodexHook(event),
	}
}

type InspectionDecision struct {
	Status   string
	Route    InspectionRoute
	Policies []policy.Decision
}

func decideInspection(
	bundle policy.Bundle,
	ctx InspectionContext,
	policyDecisions []policy.Decision,
	route InspectionRoute,
) InspectionDecision {
	return DecideInspection(bundle, ctx, policyDecisions, route)
}

// DecideInspection merges policy decisions and routing decisions into one hook
// decision, clearing rewrites when a block or unsupported provider applies.
func DecideInspection(
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
			"Hook rewrites require a known agent provider so the updated input "+
				"can be emitted with the correct provider schema.",
		))
	}

	if route.Block && resultStatus(decisions) != statusBlocked {
		decisions = append(
			decisions,
			routeBlockDecision(bundle, route.BlockPolicyID, route.Reason),
		)
	}

	if route.Rewrite &&
		!providerSupportsUpdatedInput(ctx.Provider) &&
		resultStatus(decisions) != statusBlocked {
		decisions = append(
			decisions,
			unsupportedRewriteDecision(bundle, route),
		)
	}

	status := resultStatus(decisions)
	if status == statusBlocked ||
		(route.Rewrite && !providerSupportsUpdatedInput(ctx.Provider)) {
		route = InspectionRoute{}
	}

	return InspectionDecision{
		Route:    route,
		Policies: decisions,
		Status:   status,
	}
}

func unsupportedRewriteDecision(
	bundle policy.Bundle,
	route InspectionRoute,
) policy.Decision {
	policyID := route.BlockPolicyID
	if policyID == "" {
		policyID = providerRewritePolicyID
	}

	decision := routeBlockDecision(
		bundle,
		policyID,
		"Hook rewrite cannot be applied by this agent provider; blocking "+
			"the original command instead of allowing an unmanaged command to run.",
	)
	if route.RemediationCommand != "" {
		decision.Message = sentence(
			decision.Message,
			"Resubmit the command through: "+route.RemediationCommand,
		)
		decision.Suggestion = "Resubmit the command through: " +
			route.RemediationCommand
	}

	return decision
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
