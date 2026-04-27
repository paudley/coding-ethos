// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks_test

import (
	. "blackcat.ca/coding-ethos/go/internal/hooks"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	statusAllowed = "allowed"
	statusBlocked = "blocked"
)

func TestRunBlocksGitHookBypass(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git commit --no-verify -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].PolicyID != "git.hook_bypass" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestRunAllowsNormalGitCommit(t *testing.T) {
	t.Parallel()

	repo := initHookRepo(t)

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			Cwd:           repo,
			ToolInput: map[string]any{
				"command": "git commit -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusAllowed {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].PolicyID != "git.commit_head_advanced" ||
		result.Decisions[0].Decision != "record" {
		t.Fatalf("decision mismatch: %#v", result.Decisions[0])
	}
}

func TestRunAllowsNormalNonCommitGitCommand(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git status",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusAllowed {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func TestRunBlocksProtectedPathWrite(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": "/usr/bin/got",
				"content":   "binary",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if result.Decisions[0].PolicyID != "filesystem.protected_path" {
		t.Fatalf("policy mismatch: %#v", result.Decisions[0])
	}
}

func TestRunEmitsPostToolHookOutputContext(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PostToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "git commit -m test",
			},
			ToolResponse: map[string]any{
				"stdout":      "ruff...Failed\nmypy...Passed",
				"return_code": 1,
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.HookSpecificOutput == nil {
		t.Fatal("expected hook-specific output")
	}

	if !strings.Contains(
		result.HookSpecificOutput.AdditionalContext,
		"format: toon",
	) ||
		!strings.Contains(result.HookSpecificOutput.AdditionalContext, "ruff...Failed") {
		t.Fatalf("unexpected context: %#v", result.HookSpecificOutput)
	}
}

func TestBlockedAdviceUsesTOONForAgentOutput(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	advice := BlockedAdvice(Result{
		Event:  "PreToolUse",
		Tool:   "Bash",
		Status: statusBlocked,
		Decisions: []policy.Decision{
			{
				PolicyID:   "shell.github_admin",
				Decision:   "block",
				Severity:   "block",
				Message:    "gh --admin bypasses normal review gates.",
				Suggestion: "Use the normal review path.",
				PrincipleIDs: []string{
					"evidence-based-engineering-and-decision-quality",
				},
			},
		},
	})

	for _, expected := range []string{
		"format: toon",
		"event: PreToolUse",
		"policy_id: shell.github_admin",
		"suggestion: Use the normal review path.",
		"ethos_reminder:",
		"axiom: Todo lists prevent partial work from masquerading as completion.",
	} {
		if !strings.Contains(advice, expected) {
			t.Fatalf("missing %q in advice: %s", expected, advice)
		}
	}
}

func TestBlockedAdviceUsesEthosReminderInHumanOutput(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "human")

	advice := BlockedAdvice(Result{
		Event:  "PreToolUse",
		Tool:   "Bash",
		Status: statusBlocked,
		Decisions: []policy.Decision{
			{
				PolicyID:     "git.destructive_command",
				Decision:     "block",
				Severity:     "block",
				Message:      "Destructive git command blocked.",
				PrincipleIDs: []string{"no-rationalized-shortcuts"},
			},
		},
	})

	if !strings.Contains(advice, "ETHOS reminder:") ||
		!strings.Contains(advice, "Laziness only moves the cost downstream.") {
		t.Fatalf("missing human reminder: %s", advice)
	}
}

//nolint:paralleltest // Mutates process env to force agent-facing TOON output.
func TestLegacyHookFixturesStayRunnable(t *testing.T) {
	t.Setenv("CODE_ETHOS_HOOK_OUTPUT_FORMAT", "toon")

	tests := []struct {
		name        string
		path        string
		wantStatus  string
		wantPolicy  string
		wantContext string
	}{
		{
			name:       "git hook bypass",
			path:       "testdata/legacy/pretooluse_git_no_verify.json",
			wantStatus: statusBlocked,
			wantPolicy: "git.hook_bypass",
		},
		{
			name:       "protected path write",
			path:       "testdata/legacy/pretooluse_protected_path_write.json",
			wantStatus: statusBlocked,
			wantPolicy: "filesystem.protected_path",
		},
		{
			name:       "gh admin",
			path:       "testdata/legacy/pretooluse_gh_admin.json",
			wantStatus: statusBlocked,
			wantPolicy: "shell.github_admin",
		},
		{
			name:       "bare except write",
			path:       "testdata/legacy/pretooluse_bare_except_write.json",
			wantStatus: statusBlocked,
			wantPolicy: "python.bare_except",
		},
		{
			name:       "type ignore edit",
			path:       "testdata/legacy/pretooluse_type_ignore_edit.json",
			wantStatus: statusBlocked,
			wantPolicy: "python.unexplained_type_ignore",
		},
		{
			name:        "post tool hook output",
			path:        "testdata/legacy/posttooluse_precommit_failure.json",
			wantStatus:  statusAllowed,
			wantContext: "format: toon",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			event := readLegacyEventFixture(t, test.path)

			result, err := Run(legacyFixtureBundle(), Options{Event: event})
			if err != nil {
				t.Fatalf("run hook: %v", err)
			}

			if result.Status != test.wantStatus {
				t.Fatalf("status = %q, want %q", result.Status, test.wantStatus)
			}

			if test.wantPolicy != "" && !hasDecision(result.Decisions, test.wantPolicy) {
				t.Fatalf("policy mismatch: %#v", result.Decisions)
			}

			if test.wantContext != "" &&
				(result.HookSpecificOutput == nil ||
					!strings.Contains(
						result.HookSpecificOutput.AdditionalContext,
						test.wantContext,
					)) {
				t.Fatalf("missing hook context: %#v", result.HookSpecificOutput)
			}
		})
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

func legacyFixtureBundle() policy.Bundle {
	bundle := policy.ExampleBundle()
	addLegacyPolicy(
		&bundle,
		"shell.github_admin",
		"shell",
		"shell.github_admin",
		"Bash",
	)
	addLegacyPolicy(
		&bundle,
		"python.bare_except",
		"python",
		"python.bare_except",
		"Write",
	)
	addLegacyPolicy(
		&bundle,
		"python.unexplained_type_ignore",
		"python",
		"python.unexplained_type_ignore",
		"Edit",
	)

	return bundle
}

func addLegacyPolicy(
	bundle *policy.Bundle,
	policyID string,
	category string,
	evaluatorName string,
	tool string,
) {
	bundle.Policies[policyID] = policy.Policy{
		ID:              policyID,
		Category:        category,
		Source:          policy.SourceRef{File: "testdata/legacy_hook_inventory.json"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record", "advise"},
		Message:         "legacy fixture policy",
		DefenseLayers:   policy.GitDefenseLayers("block", "", "block", "", ""),
		Evaluators:      []policy.Evaluator{{Kind: category, Name: evaluatorName}},
	}

	if bundle.Dispatch.Hooks["PreToolUse"] == nil {
		bundle.Dispatch.Hooks["PreToolUse"] = map[string][]policy.HookDispatchEntry{}
	}

	bundle.Dispatch.Hooks["PreToolUse"][tool] = append(
		bundle.Dispatch.Hooks["PreToolUse"][tool],
		policy.HookDispatchEntry{PolicyID: policyID, Mode: "block"},
	)
}

func readLegacyEventFixture(t *testing.T, path string) Event {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	var event Event

	err = json.Unmarshal(payload, &event)
	if err != nil {
		t.Fatalf("decode fixture %s: %v", path, err)
	}

	return event
}

func TestRunSkipsPathScopedPolicyWhenPathDoesNotMatch(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": "README.md",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusAllowed {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", result.Decisions)
	}
}

func TestRunFailsFastForMissingEvaluator(t *testing.T) {
	t.Parallel()

	bundle := policy.ExampleBundle()
	bundle.Policies["python.conditional_imports"] = policy.Policy{
		ID:       "python.conditional_imports",
		Category: "python",
		Source: policy.SourceRef{
			File: "config.yaml",
			Path: "python.conditional_imports",
		},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         "Required dependencies should fail immediately.",
		DefenseLayers:   policy.CodeDefenseLayers(),
		Evaluators:      []policy.Evaluator{{Kind: "ast", Name: "python.missing"}},
	}

	_, err := Run(bundle, Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": "src/app.py",
			},
		},
	})
	if err == nil {
		t.Fatal("expected missing evaluator error")
	}

	if !strings.Contains(err.Error(), "unregistered evaluator") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAdvisesPythonWriteViolation(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Write",
			ToolInput: map[string]any{
				"file_path": "src/app.py",
				"content":   "try:\n    import missing\nexcept ImportError:\n    missing = None\n",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != "allowed" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", result.Decisions)
	}

	if result.Decisions[0].Decision != "advise" ||
		result.Decisions[0].Severity != "advise" {
		t.Fatalf("expected advisory decision, got %#v", result.Decisions[0])
	}
}

func TestRunDoesNotTreatQuotedNoVerifyAsBypass(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "printf '%s\\n' '--no-verify'",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != "allowed" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func TestRunBlocksEnvPrefixedNoVerifyBypass(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Event: Event{
			HookEventName: "PreToolUse",
			ToolName:      "Bash",
			ToolInput: map[string]any{
				"command": "FOO=bar git commit --no-verify -m test",
			},
		},
	})
	if err != nil {
		t.Fatalf("run hook: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func TestDecodeEventReadsClaudeLikePayload(t *testing.T) {
	t.Parallel()

	event, err := DecodeEvent(strings.NewReader(`{
		"hook_event_name": "PreToolUse",
		"tool_name": "Bash",
		"tool_input": {"command": "git status"}
	}`))
	if err != nil {
		t.Fatalf("decode event: %v", err)
	}

	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
		t.Fatalf("event mismatch: %#v", event)
	}

	if event.Command() != "git status" {
		t.Fatalf("command mismatch: %q", event.Command())
	}
}

func initHookRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runHookGit(t, repo, "init")
	runHookGit(t, repo, "config", "user.email", "test@example.com")
	runHookGit(t, repo, "config", "user.name", "Test User")
	runHookGit(t, repo, "config", "commit.gpgsign", "false")

	err := os.WriteFile(filepath.Join(repo, "file.txt"), []byte("initial\n"), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	runHookGit(t, repo, "add", "file.txt")
	runHookGit(t, repo, "commit", "-m", "initial")

	return repo
}

func runHookGit(t *testing.T, repo string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = repo

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, output)
	}
}
