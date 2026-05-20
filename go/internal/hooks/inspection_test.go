// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestCollectInspectionContextAnnotatesAdminReadOnlyOnce(t *testing.T) {
	t.Parallel()

	ctx := CollectInspectionContext(Event{
		Cwd:           "/workspace/coding-ethos",
		HookEventName: "PreToolUse",
		ProviderHint:  "codex",
		ToolName:      "Bash",
		ToolInput: map[string]any{
			"command": `git diff --stat -- go/internal/hooks/json.go`,
		},
	}, stubAdminApprovedForCWD(true))

	if !ctx.AdminApproved || !ctx.ReadOnlyInspection {
		t.Fatalf("inspection context not annotated: %#v", ctx)
	}

	if ctx.Provider != "codex" {
		t.Fatalf("provider = %q, want %q", ctx.Provider, "codex")
	}
}

func TestCollectInspectionContextDoesNotMarkNormalCommandReadOnly(t *testing.T) {
	t.Parallel()

	ctx := CollectInspectionContext(Event{
		Cwd:           "/workspace/coding-ethos",
		HookEventName: "PreToolUse",
		ProviderHint:  "codex",
		ToolName:      "Bash",
		ToolInput: map[string]any{
			"command": `git diff --stat -- go/internal/hooks/json.go`,
		},
	}, stubAdminApprovedForCWD(false))

	if ctx.AdminApproved || ctx.ReadOnlyInspection {
		t.Fatalf("normal context incorrectly annotated: %#v", ctx)
	}
}

func TestDecideInspectionClearsRewriteWhenPolicyBlocks(t *testing.T) {
	t.Parallel()

	decision := DecideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{Provider: "claude"},
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

	if decision.Status != hookStatusBlocked {
		t.Fatalf("status = %q, want blocked", decision.Status)
	}

	if decision.Route.Rewrite || len(decision.Route.UpdatedInput) > 0 {
		t.Fatalf("blocked inspection must clear route rewrite: %#v", decision.Route)
	}
}

func TestDecideInspectionConvertsRouteBlockToPolicyDecision(t *testing.T) {
	t.Parallel()

	decision := DecideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{Provider: "claude"},
		nil,
		InspectionRoute{
			Block:         true,
			BlockPolicyID: "git.wrapper_required",
			Reason:        "blocked route",
		},
	)

	if decision.Status != hookStatusBlocked || len(decision.Policies) != 1 {
		t.Fatalf("decision mismatch: %#v", decision)
	}

	if decision.Policies[0].PolicyID != "git.wrapper_required" {
		t.Fatalf("policy = %#v", decision.Policies[0])
	}
}

func TestDecideInspectionBlocksRewriteForUnknownProvider(t *testing.T) {
	t.Parallel()

	decision := DecideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{},
		nil,
		InspectionRoute{
			Rewrite:      true,
			UpdatedInput: map[string]any{"command": "rewritten"},
			Reason:       "rewrite",
		},
	)

	if decision.Status != hookStatusBlocked || len(decision.Policies) != 1 {
		t.Fatalf("decision mismatch: %#v", decision)
	}

	if decision.Policies[0].PolicyID != "hook.provider_required" {
		t.Fatalf("policy = %#v", decision.Policies[0])
	}

	if decision.Route.Rewrite || len(decision.Route.UpdatedInput) > 0 {
		t.Fatalf("blocked inspection must clear route rewrite: %#v", decision.Route)
	}
}

func TestDecideInspectionBlocksRewriteForUnsupportedProvider(t *testing.T) {
	t.Parallel()

	decision := DecideInspection(
		exampleBundleForInspectionTest(),
		InspectionContext{Provider: "codex"},
		nil,
		InspectionRoute{
			Rewrite:      true,
			UpdatedInput: map[string]any{"command": "rewritten"},
			Reason:       "rewrite",
		},
	)

	if decision.Status != hookStatusBlocked || len(decision.Policies) != 1 {
		t.Fatalf("decision mismatch: %#v", decision)
	}

	if decision.Policies[0].PolicyID != "hook.provider_required" {
		t.Fatalf("policy = %#v", decision.Policies[0])
	}

	if decision.Route.Rewrite || len(decision.Route.UpdatedInput) > 0 {
		t.Fatalf(
			"blocked unsupported provider inspection must clear route rewrite: %#v",
			decision.Route,
		)
	}
}

func exampleBundleForInspectionTest() policy.Bundle {
	return policy.ExampleBundle()
}
