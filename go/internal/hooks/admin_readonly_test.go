// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunAllowsAdminReadOnlyHookImplementationInspection(t *testing.T) {
	restore := stubAdminApprovedForCWD(true)
	defer restore()

	result, err := Run(policy.ExampleBundle(), Options{
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

	if result.Status != statusAllowed {
		t.Fatalf(
			"status mismatch: got %q decisions %#v",
			result.Status,
			result.Decisions,
		)
	}
}

func TestRunAllowsAdminReadOnlyGitDiffWithoutRewrite(t *testing.T) {
	restore := stubAdminApprovedForCWD(true)
	defer restore()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "codex",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `git diff --stat -- go/internal/hooks/json.go .codex/config.toml`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusAllowed {
		t.Fatalf("status mismatch: got %q decisions %#v", result.Status, result.Decisions)
	}

	if result.HookSpecificOutput != nil &&
		len(result.HookSpecificOutput.UpdatedInput) > 0 {
		t.Fatalf(
			"admin read-only inspection must not emit updatedInput: %#v",
			result.HookSpecificOutput,
		)
	}
}

func TestRunAllowsAdminReadOnlyRedirectCapture(t *testing.T) {
	restore := stubAdminApprovedForCWD(true)
	defer restore()

	result, err := Run(policy.ExampleBundle(), Options{
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

	if result.Status != statusAllowed {
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
				parallelToolBatchMarker: true,
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

	if result.Status != statusBlocked ||
		!hasDecision(result.Decisions, parallelToolBatchPolicyID) {
		t.Fatalf("expected parallel batch decision, got %#v", result)
	}
}

func TestRunBlocksAdminMutatingHookImplementationInspection(t *testing.T) {
	restore := stubAdminApprovedForCWD(true)
	defer restore()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `sed -i 's/header/footer/' /workspace/coding-ethos/.claude/settings.json`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusBlocked ||
		!hasDecision(result.Decisions, shellForbiddenStringsPolicyID) {
		t.Fatalf("expected forbidden string decision, got %#v", result)
	}
}

func TestRunBlocksAdminSedLongFormInPlaceMutation(t *testing.T) {
	restore := stubAdminApprovedForCWD(true)
	defer restore()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": `sed --in-place 's/header/footer/' /workspace/coding-ethos/.claude/settings.json`,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusBlocked ||
		!hasDecision(result.Decisions, shellForbiddenStringsPolicyID) {
		t.Fatalf("expected forbidden string decision, got %#v", result)
	}
}

func TestReadOnlyInspectionRejectsDynamicShell(t *testing.T) {
	t.Parallel()

	if isReadOnlyInspectionCommand(`rg "$(cat needle)" /workspace/coding-ethos`) {
		t.Fatal("dynamic inspection command was allowed")
	}
}

func stubAdminApprovedForCWD(approved bool) func() {
	previous := adminApprovedForCWD
	adminApprovedForCWD = func(string) bool {
		return approved
	}

	return func() {
		adminApprovedForCWD = previous
	}
}

func hasDecision(decisions []policy.Decision, policyID string) bool {
	for _, decision := range decisions {
		if decision.PolicyID == policyID {
			return true
		}
	}

	return false
}
