// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestGitSafetyEvaluatorsBlockUnsafeCommands(t *testing.T) {
	tests := []struct {
		name      string
		policyID  string
		evaluator EvaluatorFunc
		argv      []string
	}{
		{
			name:      "reset hard",
			policyID:  "git.destructive_command",
			evaluator: EvaluateGitDestructiveCommand,
			argv:      []string{"git", "reset", "--hard"},
		},
		{
			name:      "clean force delete",
			policyID:  "git.destructive_command",
			evaluator: EvaluateGitDestructiveCommand,
			argv:      []string{"git", "clean", "-fd"},
		},
		{
			name:      "checkout theirs",
			policyID:  "git.destructive_command",
			evaluator: EvaluateGitDestructiveCommand,
			argv:      []string{"git", "checkout", "--theirs", "file.txt"},
		},
		{
			name:      "merge theirs",
			policyID:  "git.merge_strategy_shortcut",
			evaluator: EvaluateGitMergeStrategyShortcut,
			argv:      []string{"git", "merge", "-X", "theirs", "feature"},
		},
		{
			name:      "force push main",
			policyID:  "git.force_push_protected_branch",
			evaluator: EvaluateGitForcePushProtectedBranch,
			argv:      []string{"git", "push", "--force", "origin", "main"},
		},
		{
			name:      "checkout main",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateGitCheckoutProtectedBranch,
			argv:      []string{"git", "checkout", "main"},
		},
		{
			name:      "worktree prune",
			policyID:  "git.destructive_worktree",
			evaluator: EvaluateGitDestructiveWorktree,
			argv:      []string{"git", "worktree", "prune"},
		},
		{
			name:      "change dir flag",
			policyID:  "git.change_dir_flag",
			evaluator: EvaluateGitChangeDirFlag,
			argv:      []string{"git", "-C", "/tmp/repo", "status"},
		},
		{
			name:      "stash",
			policyID:  "git.stash_blocked",
			evaluator: EvaluateGitStashBlocked,
			argv:      []string{"git", "stash"},
		},
	}

	bundle := compiledGitSafetyTestBundle()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policyDef := bundle.Policies[test.policyID]
			decisions, err := test.evaluator(policyDef, Context{Argv: test.argv})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if len(decisions) != 1 {
				t.Fatalf("decision count mismatch: got %d", len(decisions))
			}
			if decisions[0].Decision != "block" {
				t.Fatalf("decision mismatch: %#v", decisions[0])
			}
		})
	}
}

func TestGitSafetyEvaluatorsAllowSafeCommands(t *testing.T) {
	tests := []struct {
		name      string
		policyID  string
		evaluator EvaluatorFunc
		argv      []string
	}{
		{
			name:      "soft reset",
			policyID:  "git.destructive_command",
			evaluator: EvaluateGitDestructiveCommand,
			argv:      []string{"git", "reset", "--soft", "HEAD~1"},
		},
		{
			name:      "normal push feature",
			policyID:  "git.force_push_protected_branch",
			evaluator: EvaluateGitForcePushProtectedBranch,
			argv:      []string{"git", "push", "origin", "feature"},
		},
		{
			name:      "checkout feature",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateGitCheckoutProtectedBranch,
			argv:      []string{"git", "checkout", "feature"},
		},
		{
			name:      "worktree list",
			policyID:  "git.destructive_worktree",
			evaluator: EvaluateGitDestructiveWorktree,
			argv:      []string{"git", "worktree", "list"},
		},
	}

	bundle := compiledGitSafetyTestBundle()
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policyDef := bundle.Policies[test.policyID]
			decisions, err := test.evaluator(policyDef, Context{Argv: test.argv})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}
			if len(decisions) != 0 {
				t.Fatalf("expected no decisions, got %#v", decisions)
			}
		})
	}
}

func compiledGitSafetyTestBundle() policy.Bundle {
	bundle := policy.ExampleBundle()
	for _, policyID := range []string{
		"git.destructive_command",
		"git.merge_strategy_shortcut",
		"git.force_push_protected_branch",
		"git.checkout_protected_branch",
		"git.destructive_worktree",
		"git.change_dir_flag",
		"git.stash_blocked",
	} {
		bundle.Policies[policyID] = policy.Policy{
			ID:              policyID,
			Category:        "git",
			Source:          policy.SourceRef{File: "config.yaml", Path: policyID},
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "blocked",
			DefenseLayers:   policy.GitDefenseLayers("block", "wrapper", "block", "", ""),
			Evaluators:      []policy.Evaluator{{Kind: "argv", Name: policyID}},
		}
	}
	return bundle
}
