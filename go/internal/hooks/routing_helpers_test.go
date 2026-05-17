// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks_test

import (
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestGitWrapperRoutingRewritesOrdinaryGit(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	result := runGeminiBashHook(t, "git status --short && echo ok")

	if result.Status != hookStatusAllowed || result.HookSpecificOutput == nil {
		t.Fatalf("git route result = %#v", result)
	}

	command, ok := result.HookSpecificOutput.UpdatedInput["command"].(string)
	if !ok || !strings.Contains(
		command,
		"'/repo/bin/coding-ethos-run' policy-git 'status' '--short'",
	) {
		t.Fatalf("rewritten command = %#v", result.HookSpecificOutput.UpdatedInput)
	}
}

func TestGitWrapperRoutingBlocksEvasiveShell(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"bash -c 'git status'",
		"python -c 'import subprocess; subprocess.run([\"git\", \"status\"])'",
		"command git status",
	} {
		result := runGeminiBashHook(t, command)
		if result.Status != hookStatusBlocked ||
			!hasDecision(result.Decisions, "git.wrapper_required") {
			t.Fatalf("command %q result = %#v", command, result)
		}
	}
}

func TestLintToolRoutingRewritesOrdinaryLintTool(t *testing.T) {
	t.Setenv("CODING_ETHOS_RUN_GO_HOOK", "/repo/bin/coding-ethos-run")

	result := runGeminiBashHook(t, "python -m ruff check pkg 2>&1")

	if result.Status != hookStatusAllowed || result.HookSpecificOutput == nil {
		t.Fatalf("lint route result = %#v", result)
	}

	command, ok := result.HookSpecificOutput.UpdatedInput["command"].(string)
	if !ok || !strings.Contains(
		command,
		"'/repo/bin/coding-ethos-run' policy-tool ruff 'check' 'pkg' 2>&1",
	) {
		t.Fatalf("rewritten command = %#v", result.HookSpecificOutput.UpdatedInput)
	}
}

func TestLintToolRoutingBlocksEvasiveShell(t *testing.T) {
	t.Parallel()

	for _, command := range []string{
		"bash -c 'ruff check pkg'",
		"python -c 'import subprocess; subprocess.run([\"ruff\", \"check\"])'",
		"PATH=/tmp:$PATH ruff check pkg",
		"eval 'mypy pkg'",
	} {
		result := runGeminiBashHook(t, command)
		if result.Status != hookStatusBlocked ||
			!hasAnyDecision(
				result.Decisions,
				"git.wrapper_required",
				"tool.lint_capture_required",
				"tool.ruff_capture_required",
				"tool.mypy_capture_required",
			) {
			t.Fatalf("command %q result = %#v", command, result)
		}
	}
}

func runGeminiBashHook(t *testing.T, command string) Result {
	t.Helper()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			Cwd:           "/workspace/coding-ethos",
			HookEventName: "PreToolUse",
			ProviderHint:  "gemini",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": command,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	return result
}

func hasAnyDecision(decisions []policy.Decision, policyIDs ...string) bool {
	for _, policyID := range policyIDs {
		if hasDecision(decisions, policyID) {
			return true
		}
	}

	return false
}
