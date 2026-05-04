// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestCollectInspectionContextAnnotatesAdminReadOnlyOnce(t *testing.T) {
	restore := stubAdminApprovedForCWD(true)
	defer restore()

	ctx := collectInspectionContext(Event{
		Cwd:           "/workspace/coding-ethos",
		HookEventName: "PreToolUse",
		ProviderHint:  "codex",
		ToolName:      "Bash",
		ToolInput: map[string]any{
			"command": `git diff --stat -- go/internal/hooks/json.go`,
		},
	})

	if !ctx.AdminApproved || !ctx.ReadOnlyInspection {
		t.Fatalf("inspection context not annotated: %#v", ctx)
	}
	if ctx.Provider != providerCodex {
		t.Fatalf("provider = %q, want %q", ctx.Provider, providerCodex)
	}
}

func TestCollectInspectionContextDoesNotMarkNormalCommandReadOnly(t *testing.T) {
	restore := stubAdminApprovedForCWD(false)
	defer restore()

	ctx := collectInspectionContext(Event{
		Cwd:           "/workspace/coding-ethos",
		HookEventName: "PreToolUse",
		ProviderHint:  "codex",
		ToolName:      "Bash",
		ToolInput: map[string]any{
			"command": `git diff --stat -- go/internal/hooks/json.go`,
		},
	})

	if ctx.AdminApproved || ctx.ReadOnlyInspection {
		t.Fatalf("normal context incorrectly annotated: %#v", ctx)
	}
}

func TestDecideInspectionClearsRewriteWhenPolicyBlocks(t *testing.T) {
	decision := decideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{Provider: providerClaude},
		[]policy.Decision{{
			PolicyID: "git.hook_bypass",
			Decision: "block",
			Severity: "block",
			Message:  "blocked",
		}},
		InspectionRoute{
			Rewrite:      true,
			UpdatedInput: map[string]any{"command": "rewritten"},
			Reason:       "rewrite",
		},
	)

	if decision.Status != statusBlocked {
		t.Fatalf("status = %q, want blocked", decision.Status)
	}
	if decision.Route.Rewrite || len(decision.Route.UpdatedInput) > 0 {
		t.Fatalf("blocked inspection must clear route rewrite: %#v", decision.Route)
	}
}

func TestDecideInspectionConvertsRouteBlockToPolicyDecision(t *testing.T) {
	decision := decideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{Provider: providerClaude},
		nil,
		InspectionRoute{
			Block:         true,
			BlockPolicyID: "git.wrapper_required",
			Reason:        "blocked route",
		},
	)

	if decision.Status != statusBlocked || len(decision.Policies) != 1 {
		t.Fatalf("decision mismatch: %#v", decision)
	}
	if decision.Policies[0].PolicyID != "git.wrapper_required" {
		t.Fatalf("policy = %#v", decision.Policies[0])
	}
}

func TestDecideInspectionBlocksRewriteForUnknownProvider(t *testing.T) {
	decision := decideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{},
		nil,
		InspectionRoute{
			Rewrite:      true,
			UpdatedInput: map[string]any{"command": "rewritten"},
			Reason:       "rewrite",
		},
	)

	if decision.Status != statusBlocked || len(decision.Policies) != 1 {
		t.Fatalf("decision mismatch: %#v", decision)
	}
	if decision.Policies[0].PolicyID != providerRewritePolicyID {
		t.Fatalf("policy = %#v", decision.Policies[0])
	}
	if decision.Route.Rewrite || len(decision.Route.UpdatedInput) > 0 {
		t.Fatalf("blocked inspection must clear route rewrite: %#v", decision.Route)
	}
}

func TestDecideInspectionClearsRewriteForUnsupportedProvider(t *testing.T) {
	decision := decideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{Provider: providerCodex},
		nil,
		InspectionRoute{
			Rewrite:      true,
			UpdatedInput: map[string]any{"command": "rewritten"},
			Reason:       "rewrite",
		},
	)

	if decision.Status != statusAllowed || len(decision.Policies) != 0 {
		t.Fatalf("decision mismatch: %#v", decision)
	}
	if decision.Route.Rewrite || len(decision.Route.UpdatedInput) > 0 {
		t.Fatalf("unsupported provider inspection must clear route rewrite: %#v", decision.Route)
	}
}

func exampleBundleForInspectionTest() policy.Bundle {
	return policy.ExampleBundle()
}
