// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package proxypolicy_test

import (
	"context"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/proxypolicy"
)

const outboundExfiltrationWhen = `proxy.direction == "outbound" && proxy.has_dlp_facts &&
	proxy.dlp_facts.exists(f, f.type in ["secret", "credential_file", "protected_path"])`

// outboundExfiltrationPolicy returns a proxy-scoped outbound policy mirroring the
// compiled seed policy so tests exercise real CEL evaluation.
func outboundExfiltrationPolicy(when string) policy.Policy {
	return policy.Policy{
		ID:              "proxy.outbound_exfiltration",
		Category:        "proxy",
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Outbound provider request carries secret content and was blocked.",
		Suggestion:      "Remove the secret from the request.",
		PrincipleIDs:    []string{"security-by-design"},
		Evaluators: []policy.Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"scope":           "proxy",
				"proxy_direction": "outbound",
				"mode":            "block",
				"skill_id":        "security-by-design",
				"when":            when,
			},
		}},
	}
}

// bundleWith wraps a single policy in a bundle for construction tests.
func bundleWith(policyDef policy.Policy) policy.Bundle {
	return policy.Bundle{
		Policies: map[string]policy.Policy{policyDef.ID: policyDef},
	}
}

func TestEvaluateOutboundDeniesSecretDLPFacts(t *testing.T) {
	t.Parallel()

	evaluator, err := proxypolicy.New(bundleWith(
		outboundExfiltrationPolicy(outboundExfiltrationWhen),
	))
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	decision, err := evaluator.EvaluateOutbound(
		context.Background(),
		agentproxy.ProxyDecisionInput{
			Direction: agentproxy.DirectionOutbound,
			Kind:      agentproxy.EventProviderCall,
			Provider:  "anthropic",
			EventID:   "event-1",
			SessionID: "session-1",
			DLPFacts: []agentproxy.DLPFact{{
				Type:       "secret",
				Reason:     "openai_api_key_prefix",
				Confidence: "high",
			}},
		},
	)
	if err != nil {
		t.Fatalf("evaluate outbound: %v", err)
	}

	if decision.Allowed {
		t.Fatal("expected secret-bearing outbound request to be denied")
	}

	if decision.PolicyID != "proxy.outbound_exfiltration" {
		t.Fatalf("policy id = %q", decision.PolicyID)
	}

	if decision.EvidenceID != "event-1" {
		t.Fatalf("evidence id = %q", decision.EvidenceID)
	}

	if decision.Metadata["proxy_event_id"] != "event-1" {
		t.Fatalf("proxy_event_id metadata = %q", decision.Metadata["proxy_event_id"])
	}

	if decision.Metadata["proxy_direction"] != "outbound" {
		t.Fatalf("proxy_direction metadata = %q", decision.Metadata["proxy_direction"])
	}
}

func TestEvaluateOutboundAllowsWithoutDLPFacts(t *testing.T) {
	t.Parallel()

	evaluator, err := proxypolicy.New(bundleWith(
		outboundExfiltrationPolicy(outboundExfiltrationWhen),
	))
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	decision, err := evaluator.EvaluateOutbound(
		context.Background(),
		agentproxy.ProxyDecisionInput{
			Direction: agentproxy.DirectionOutbound,
			Kind:      agentproxy.EventProviderCall,
			Provider:  "anthropic",
			EventID:   "event-2",
			SessionID: "session-2",
		},
	)
	if err != nil {
		t.Fatalf("evaluate outbound: %v", err)
	}

	if !decision.Allowed {
		t.Fatalf("expected clean outbound request to be allowed, got %#v", decision)
	}
}

func TestEvaluateOutboundSelectsBothDirectionAndCarriesTrace(t *testing.T) {
	t.Parallel()

	policyDef := outboundExfiltrationPolicy(outboundExfiltrationWhen)
	policyDef.Evaluators[0].Options["proxy_direction"] = "both"

	evaluator, err := proxypolicy.New(bundleWith(policyDef))
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	decision, err := evaluator.EvaluateOutbound(
		context.Background(),
		agentproxy.ProxyDecisionInput{
			Direction:   agentproxy.DirectionOutbound,
			Kind:        agentproxy.EventProviderCall,
			Provider:    "anthropic",
			PayloadKind: agentproxy.PayloadPrompt,
			EventID:     "event-3",
			SessionID:   "session-3",
			Metadata: map[string]string{
				"proxy_trace_id":    "trace-3",
				"proxy_tracking_id": "tracking-3",
			},
			DLPFacts: []agentproxy.DLPFact{{Type: "credential_file", Path: ".env"}},
		},
	)
	if err != nil {
		t.Fatalf("evaluate outbound: %v", err)
	}

	if decision.Allowed {
		t.Fatal("expected both-direction policy to deny outbound secret")
	}

	if decision.Metadata["proxy_trace_id"] != "trace-3" {
		t.Fatalf("proxy_trace_id = %q", decision.Metadata["proxy_trace_id"])
	}

	if decision.Metadata["proxy_tracking_id"] != "tracking-3" {
		t.Fatalf("proxy_tracking_id = %q", decision.Metadata["proxy_tracking_id"])
	}

	if decision.Metadata["proxy_payload_kind"] != "prompt" {
		t.Fatalf("proxy_payload_kind = %q", decision.Metadata["proxy_payload_kind"])
	}
}

func TestNewSkipsNonProxyAndInboundPolicies(t *testing.T) {
	t.Parallel()

	inbound := outboundExfiltrationPolicy(outboundExfiltrationWhen)
	inbound.ID = "proxy.inbound_only"
	inbound.Evaluators[0].Options["proxy_direction"] = "inbound"

	nonProxy := policy.Policy{
		ID:       "shell.example",
		Category: "expression",
		Evaluators: []policy.Evaluator{{
			Kind:    "cel",
			Name:    "cel.expression",
			Options: map[string]any{"scope": "command", "when": "true"},
		}},
	}

	evaluator, err := proxypolicy.New(policy.Bundle{
		Policies: map[string]policy.Policy{
			inbound.ID:  inbound,
			nonProxy.ID: nonProxy,
		},
	})
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	decision, err := evaluator.EvaluateOutbound(
		context.Background(),
		agentproxy.ProxyDecisionInput{
			Direction: agentproxy.DirectionOutbound,
			EventID:   "event-4",
			DLPFacts:  []agentproxy.DLPFact{{Type: "secret"}},
		},
	)
	if err != nil {
		t.Fatalf("evaluate outbound: %v", err)
	}

	if !decision.Allowed {
		t.Fatal(
			"expected no outbound policy to match inbound-only and command-scoped policies",
		)
	}
}

func TestEvaluateOutboundDenyOrderIsDeterministic(t *testing.T) {
	t.Parallel()

	// Two policies both match a secret-bearing request. The lowest-sorted policy
	// ID must win every run regardless of the bundle map's iteration order.
	first := outboundExfiltrationPolicy(outboundExfiltrationWhen)
	first.ID = "proxy.aaa_first"

	second := outboundExfiltrationPolicy(outboundExfiltrationWhen)
	second.ID = "proxy.zzz_second"

	for range 20 {
		evaluator, err := proxypolicy.New(policy.Bundle{
			Policies: map[string]policy.Policy{
				first.ID:  first,
				second.ID: second,
			},
		})
		if err != nil {
			t.Fatalf("new evaluator: %v", err)
		}

		decision, err := evaluator.EvaluateOutbound(
			context.Background(),
			agentproxy.ProxyDecisionInput{
				Direction: agentproxy.DirectionOutbound,
				Kind:      agentproxy.EventProviderCall,
				EventID:   "event-order",
				DLPFacts:  []agentproxy.DLPFact{{Type: "secret"}},
			},
		)
		if err != nil {
			t.Fatalf("evaluate outbound: %v", err)
		}

		if decision.PolicyID != "proxy.aaa_first" {
			t.Fatalf("nondeterministic winning policy id = %q", decision.PolicyID)
		}
	}
}

func TestEvaluateOutboundStopsOnCanceledContext(t *testing.T) {
	t.Parallel()

	evaluator, err := proxypolicy.New(bundleWith(
		outboundExfiltrationPolicy(outboundExfiltrationWhen),
	))
	if err != nil {
		t.Fatalf("new evaluator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = evaluator.EvaluateOutbound(ctx, agentproxy.ProxyDecisionInput{
		Direction: agentproxy.DirectionOutbound,
		EventID:   "event-cancel",
		DLPFacts:  []agentproxy.DLPFact{{Type: "secret"}},
	})
	if err == nil {
		t.Fatal("expected canceled context to abort evaluation")
	}
}

func TestNewFailsFastOnEmptyWhen(t *testing.T) {
	t.Parallel()

	_, err := proxypolicy.New(bundleWith(outboundExfiltrationPolicy("")))
	if err == nil {
		t.Fatal("expected New to fail on an empty when expression")
	}
}

func TestNewFailsFastOnUncompilablePolicy(t *testing.T) {
	t.Parallel()

	_, err := proxypolicy.New(bundleWith(
		outboundExfiltrationPolicy("proxy.no_such_field && ("),
	))
	if err == nil {
		t.Fatal("expected New to fail on an uncompilable proxy policy")
	}
}
