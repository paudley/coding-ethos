// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	hookStatusAllowed = "allowed"
	hookStatusBlocked = "blocked"
)

func TestRunAllowsAdminReadOnlyHookImplementationInspection(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `rg -n "header must match" /workspace/coding-ethos --include="*.go"`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusAllowed {
		t.Fatalf(
			"status mismatch: got %q decisions %#v",
			result.Status,
			result.Decisions,
		)
	}
}

func TestRunBlocksAdminReadOnlyRawGitDiffForCodex(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "codex",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `git diff --stat -- README.md`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusBlocked {
		t.Fatalf(
			"status mismatch: got %q decisions %#v",
			result.Status,
			result.Decisions,
		)
	}

	if !hasDecision(result.Decisions, "git.wrapper_required") {
		t.Fatalf("missing wrapper-required decision: %#v", result.Decisions)
	}
}

func TestRunRoutesCodingEthosGitRewriteInsideAdminCheckout(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "coding-ethos",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"agent_shell_rewrite": true,
				"command":             "git status",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusAllowed ||
		result.HookSpecificOutput == nil ||
		result.HookSpecificOutput.UpdatedInput["command"] == "git status" {
		t.Fatalf("expected coding-ethos rewrite inside admin checkout: %#v", result)
	}
}

func TestRunBlocksAdminReadOnlyRawGitRedirectCaptureForCodex(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "codex",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `git status --short 2>&1`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusBlocked {
		t.Fatalf(
			"status mismatch: got %q decisions %#v",
			result.Status,
			result.Decisions,
		)
	}

	if !hasDecision(result.Decisions, "git.wrapper_required") {
		t.Fatalf("missing wrapper-required decision: %#v", result.Decisions)
	}
}

func TestRunBlocksAdminFileToolEmulation(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "claude",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `cat README.md`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusBlocked {
		t.Fatalf("status mismatch: got %q decisions %#v", result.Status, result.Decisions)
	}
}

func TestRunBlocksMultiActionParallelBatch(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "codex",
			ToolName:      "multi_tool_use.parallel",
			ToolInput: map[string]any{
				"__coding_ethos_parallel_batch": true,
				"tool_uses": []any{
					map[string]any{"recipient_name": "functions.exec_command"},
					map[string]any{"recipient_name": "functions.exec_command"},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusBlocked ||
		!hasAdminDecision(result.Decisions, "hook.parallel_tool_batch_unsupported") {
		t.Fatalf("expected parallel batch decision, got %#v", result)
	}
}

func TestRunBlocksAdminMutatingHookImplementationInspection(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `sed -i 's/header/footer/' ` +
					`/workspace/coding-ethos/.claude/settings.json`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusBlocked ||
		!hasAdminDecision(result.Decisions, "shell.forbidden_strings") {
		t.Fatalf("expected forbidden string decision, got %#v", result)
	}
}

func TestRunBlocksAdminSedLongFormInPlaceMutation(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		AdminApproved: stubAdminApprovedForCWD(true),
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `sed --in-place 's/header/footer/' ` +
					`/workspace/coding-ethos/.claude/settings.json`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != hookStatusBlocked ||
		!hasAdminDecision(result.Decisions, "shell.forbidden_strings") {
		t.Fatalf("expected forbidden string decision, got %#v", result)
	}
}

func TestReadOnlyInspectionRejectsDynamicShell(t *testing.T) {
	t.Parallel()

	if ReadOnlyInspectionCommand(`rg "$(cat needle)" /workspace/coding-ethos`) {
		t.Fatal("dynamic inspection command was allowed")
	}
}

func stubAdminApprovedForCWD(approved bool) func(string) bool {
	return func(string) bool {
		return approved
	}
}

func hasAdminDecision(decisions []policy.Decision, policyID string) bool {
	for _, decision := range decisions {
		if decision.PolicyID == policyID {
			return true
		}
	}

	return false
}
