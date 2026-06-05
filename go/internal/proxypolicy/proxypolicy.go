// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package proxypolicy wires the compiled CEL policy bundle to the agentproxy
// outbound enforcement contract. It selects proxy-scoped policies, validates
// their CEL once at construction, and evaluates them against body-free
// structural proxy facts so a denial is fully traceable.
//
// This package is the composition layer between policy, evaluators, celexpr,
// and agentproxy. The agentproxy package never imports it; instead the CLI
// injects an *Evaluator as the agentproxy.ProxyPolicyEvaluator the proxy uses.
package proxypolicy

import (
	"context"
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	// evaluatorKindCEL is the evaluator kind every CEL expression policy uses.
	evaluatorKindCEL = "cel"
	// evaluatorNameCELExpression is the evaluator name of a compiled expression.
	evaluatorNameCELExpression = "cel.expression"
	// optionScope keys the policy scope within an evaluator's options.
	optionScope = "scope"
	// optionProxyDirection keys the proxy direction within evaluator options.
	optionProxyDirection = "proxy_direction"
	// optionWhen keys the CEL boolean expression within evaluator options.
	optionWhen = "when"
	// scopeProxy is the scope value marking a proxy-evaluated policy.
	scopeProxy = "proxy"
	// proxyDirectionOutbound selects policies evaluated for outbound traffic.
	proxyDirectionOutbound = "outbound"
	// proxyDirectionBoth selects policies evaluated in both directions.
	proxyDirectionBoth = "both"
	// decisionBlock is the matched-decision verdict that denies a request.
	decisionBlock = "block"
	// decisionDeny is the alternate matched-decision verdict that denies.
	decisionDeny = "deny"
)

// errEmptyProxyWhen reports a proxy policy whose CEL expression is absent. A
// proxy-scoped policy with no when clause can never decide, so construction
// fails fast rather than letting an empty matcher pass traffic silently.
var errEmptyProxyWhen = apperror.StaticError(
	"proxy policy has an empty when expression",
)

// outboundPolicy is a selected proxy policy paired with its CEL evaluator. The
// evaluator carries the option map EvaluateCELExpression reads (when, scope,
// skill_id, mode); holding both avoids re-deriving the evaluator per request.
type outboundPolicy struct {
	evaluator policy.Evaluator
	policy    policy.Policy
}

// celEvaluator is the CEL evaluation function signature. It is held as a field
// so the context-free CEL foundation is invoked through an indirection rather
// than a direct call from the context-bearing EvaluateOutbound method.
type celEvaluator func(policy.Policy, evaluators.Context) ([]policy.Decision, error)

// Evaluator decides outbound proxy requests against the proxy-scoped policies
// compiled into a bundle. It holds the validated outbound policy set and
// satisfies agentproxy.ProxyPolicyEvaluator.
type Evaluator struct {
	evaluateCEL celEvaluator
	outbound    []outboundPolicy
}

// New selects the outbound proxy-scoped policies from bundle and validates each
// policy's CEL expression once. A policy is outbound when its CEL evaluator is
// scoped "proxy" with proxy_direction "outbound" or "both". Any policy whose
// when fails to compile makes New return an error so the CLI refuses to start
// enforcement on an un-evaluatable policy.
func New(bundle policy.Bundle) (*Evaluator, error) {
	outbound := make([]outboundPolicy, 0, len(bundle.Policies))

	for _, policyDef := range bundle.Policies {
		evaluator, ok := outboundProxyEvaluator(policyDef)
		if !ok {
			continue
		}

		when := stringOption(evaluator.Options, optionWhen)
		if when == "" {
			return nil, fmt.Errorf("%w: %s", errEmptyProxyWhen, policyDef.ID)
		}

		err := celexpr.Validate(policyDef.ID, when)
		if err != nil {
			return nil, fmt.Errorf(
				"validate proxy policy %q CEL expression: %w",
				policyDef.ID,
				err,
			)
		}

		outbound = append(outbound, outboundPolicy{
			policy:    policyDef,
			evaluator: evaluator,
		})
	}

	return &Evaluator{
		evaluateCEL: evaluators.EvaluateCELExpression,
		outbound:    outbound,
	}, nil
}

// EvaluateOutbound evaluates input against every selected outbound policy in
// turn. The first policy returning a block or deny decision wins and produces a
// disallowed ProxyDecision carrying the matched policy identity, remediation,
// and SARIF proxy metadata. When no policy matches, the request is allowed.
func (evaluator *Evaluator) EvaluateOutbound(
	ctx context.Context,
	input agentproxy.ProxyDecisionInput,
) (agentproxy.ProxyDecision, error) {
	cancelErr := ctx.Err()
	if cancelErr != nil {
		return agentproxy.ProxyDecision{}, fmt.Errorf(
			"proxy policy evaluation canceled: %w",
			cancelErr,
		)
	}

	proxyInput := toCelProxyInput(input)

	for _, selected := range evaluator.outbound {
		decisions, err := evaluator.evaluateCEL(
			selected.policy,
			evaluators.Context{
				Proxy:            proxyInput,
				EvaluatorOptions: selected.evaluator.Options,
				Scope:            scopeProxy,
			},
		)
		if err != nil {
			return agentproxy.ProxyDecision{}, fmt.Errorf(
				"evaluate proxy policy %q: %w",
				selected.policy.ID,
				err,
			)
		}

		for _, decision := range decisions {
			if isDenial(decision.Decision) {
				return deniedDecision(selected.policy, decision, input), nil
			}
		}
	}

	return agentproxy.ProxyDecision{Allowed: true}, nil
}

// outboundProxyEvaluator returns policyDef's CEL evaluator when the policy is
// proxy-scoped and applies to outbound traffic. The second result is false for
// any non-proxy policy or proxy policy that only applies inbound.
func outboundProxyEvaluator(policyDef policy.Policy) (policy.Evaluator, bool) {
	for _, evaluator := range policyDef.Evaluators {
		if evaluator.Kind != evaluatorKindCEL ||
			evaluator.Name != evaluatorNameCELExpression {
			continue
		}

		if stringOption(evaluator.Options, optionScope) != scopeProxy {
			continue
		}

		if !outboundDirection(stringOption(evaluator.Options, optionProxyDirection)) {
			continue
		}

		return evaluator, true
	}

	return policy.Evaluator{}, false
}

// outboundDirection reports whether a proxy_direction option selects a policy
// for outbound evaluation. Outbound and both apply; inbound does not.
func outboundDirection(direction string) bool {
	return direction == proxyDirectionOutbound || direction == proxyDirectionBoth
}

// isDenial reports whether a matched CEL decision verdict denies the request.
func isDenial(decision string) bool {
	return decision == decisionBlock || decision == decisionDeny
}

// deniedDecision assembles the body-free disallowed verdict for a matched
// outbound policy. It pulls the remediation and skill from the policy and CEL
// decision and attaches the SARIF proxy metadata keys so the recorded denial
// event is joinable to its triggering request.
func deniedDecision(
	policyDef policy.Policy,
	decision policy.Decision,
	input agentproxy.ProxyDecisionInput,
) agentproxy.ProxyDecision {
	return agentproxy.ProxyDecision{
		Allowed:      false,
		PolicyID:     policyDef.ID,
		Reason:       decision.Message,
		Severity:     decision.Severity,
		Message:      decision.Message,
		Suggestion:   decision.Suggestion,
		PrincipleIDs: append([]string(nil), policyDef.PrincipleIDs...),
		SkillID:      decision.EvidenceSkillID(),
		EvidenceID:   input.EventID,
		Metadata:     proxySARIFMetadata(input),
	}
}

// stringOption reads a string option from an evaluator option map, returning the
// empty string when the key is absent or not a string.
func stringOption(options map[string]any, key string) string {
	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return value
}
