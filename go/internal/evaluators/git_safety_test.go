// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const decisionBlock = "block"

type gitSafetyCase struct {
	name          string
	policyID      string
	evaluator     EvaluatorFunc
	argv          []string
	adminApproved bool
}

func TestGitSafetyEvaluatorsBlockUnsafeCommands(t *testing.T) {
	t.Parallel()

	tests := unsafeGitSafetyCases()
	bundle := compiledGitSafetyTestBundle(t)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			policyDef := bundle.Policies[test.policyID]

			decisions, err := test.evaluator(policyDef, Context{
				Argv:             test.argv,
				AdminApproved:    test.adminApproved,
				EvaluatorOptions: policyDef.Evaluators[0].Options,
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}

			if len(decisions) != 1 {
				t.Fatalf("decision count mismatch: got %d", len(decisions))
			}

			if decisions[0].Decision != decisionBlock {
				t.Fatalf("decision mismatch: %#v", decisions[0])
			}
		})
	}
}

func unsafeGitSafetyCases() []gitSafetyCase {
	cases := unsafeDestructiveGitCases()
	cases = append(cases, unsafeProtectedBranchCases()...)
	cases = append(cases, unsafeSubmoduleAndAttributionCases()...)

	return cases
}

func unsafeDestructiveGitCases() []gitSafetyCase {
	return []gitSafetyCase{
		{
			name:      "reset hard",
			policyID:  "git.destructive_command",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "reset", "--hard"},
		},
		{
			name:      "clean force delete",
			policyID:  "git.destructive_command",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "clean", "-fd"},
		},
		{
			name:      "checkout theirs",
			policyID:  "git.destructive_command",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "checkout", "--theirs", "file.txt"},
		},
		{
			name:      "merge theirs",
			policyID:  "git.merge_strategy_shortcut",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "merge", "-X", "theirs", "feature"},
		},
		{
			name:      "worktree prune",
			policyID:  "git.destructive_worktree",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "worktree", "prune"},
		},
		{
			name:      "worktree force remove",
			policyID:  "git.destructive_worktree",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "worktree", "remove", "-f", "../repo-old"},
		},
	}
}

func unsafeProtectedBranchCases() []gitSafetyCase {
	return []gitSafetyCase{
		{
			name:      "force push main",
			policyID:  "git.force_push_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "push", "--force", "origin", "main"},
		},
		{
			name:      "checkout main",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "checkout", "main"},
		},
		{
			name:      "checkout creates protected branch",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "checkout", "-b", "main", "origin/feature"},
		},
		{
			name:      "checkout creates branch from protected local base",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "checkout", "-b", "feature", "main"},
		},
		{
			name:      "switch creates branch from protected local base",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "switch", "-c", "feature", "main"},
		},
	}
}

func unsafeSubmoduleAndAttributionCases() []gitSafetyCase {
	return []gitSafetyCase{
		{
			name:      "protected submodule init",
			policyID:  "git.protected_submodule_update",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "submodule", "update", "--init", "coding-ethos"},
		},
		{
			name:      "protected submodule recorded sha checkout",
			policyID:  "git.protected_submodule_update",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "submodule", "update", "coding-ethos"},
		},
		{
			name:      "protected submodule implicit all recorded sha checkout",
			policyID:  "git.protected_submodule_update",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "submodule", "update"},
		},
		{
			name:      "change dir flag",
			policyID:  "git.change_dir_flag",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "-C", "/tmp/repo", "status"},
		},
		{
			name:      "stash",
			policyID:  "git.stash_blocked",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "stash"},
		},
		{
			name:      "commit attribution message",
			policyID:  "git.commit_attribution",
			evaluator: EvaluateGitCommitAttribution,
			argv: []string{
				"git",
				"commit",
				"-m",
				"feat: test\n\nCo-authored-by: Claude",
			},
		},
	}
}

func TestGitCommitAttributionBlocksMessageFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")

	err := os.WriteFile(
		messagePath,
		[]byte("feat: test\n\nGenerated by OpenAI\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	policyDef := compiledGitSafetyTestBundle(t).Policies["git.commit_attribution"]

	decisions, err := EvaluateGitCommitAttribution(policyDef, Context{
		Argv: []string{"git", "commit", "-F", "COMMIT_EDITMSG"},
		Cwd:  repo,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != decisionBlock {
		t.Fatalf("expected block decision, got %#v", decisions)
	}

	if decisions[0].Evidence["example"] == "" {
		t.Fatalf("missing commitlint example evidence: %#v", decisions[0].Evidence)
	}
}

func TestGitCommitLintBlocksBadCommitMessageFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")

	err := os.WriteFile(messagePath, []byte("bad header\n"), 0o600)
	if err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	policyDef := compiledGitSafetyTestBundle(t).Policies["git.commitlint"]

	decisions, err := EvaluateGitCommitLint(policyDef, Context{
		Cwd:   repo,
		Files: []string{"COMMIT_EDITMSG"},
		Scope: "commit-msg",
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != decisionBlock {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestGitCommitLintMatchesGitMessageSourceSemantics(t *testing.T) {
	t.Parallel()

	policyDef := compiledGitSafetyTestBundle(t).Policies["git.commitlint"]

	for _, test := range commitMessageSourceCases() {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			decisions, err := EvaluateGitCommitLint(policyDef, Context{
				Argv:  test.argv,
				Stdin: []byte(test.stdin),
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}

			assertCommitMessageDecision(t, test.blocked, decisions)
		})
	}
}

type commitMessageSourceCase struct {
	name    string
	stdin   string
	argv    []string
	blocked bool
}

func commitMessageSourceCases() []commitMessageSourceCase {
	return []commitMessageSourceCase{
		{
			name: "multiple message flags become one message with blank paragraph separator",
			argv: []string{
				"git",
				"commit",
				"-m",
				"fix(wrapper): support repeated messages",
				"-m",
				"Body paragraph.",
			},
		},
		{
			name: "compact message flag",
			argv: []string{
				"git",
				"commit",
				"-mfix(wrapper): support compact message flag",
			},
		},
		{
			name: "long message flag with equals",
			argv: []string{
				"git",
				"commit",
				"--message=fix(wrapper): support long message flag",
			},
		},
		{
			name: "stdin file flag",
			argv: []string{
				"git",
				"commit",
				"-F",
				"-",
			},
			stdin: "fix(wrapper): support stdin message file\n\nBody paragraph.\n",
		},
		{
			name: "compact stdin file flag",
			argv: []string{
				"git",
				"commit",
				"-F-",
			},
			stdin: "fix(wrapper): support compact stdin message file\n\nBody paragraph.\n",
		},
		{
			name: "long stdin file flag with equals",
			argv: []string{
				"git",
				"commit",
				"--file=-",
			},
			stdin: "fix(wrapper): support long stdin message file\n\nBody paragraph.\n",
		},
		{
			name: "bad stdin header blocks",
			argv: []string{
				"git",
				"commit",
				"-F",
				"-",
			},
			stdin:   "bad header\n\nBody paragraph.\n",
			blocked: true,
		},
	}
}

func assertCommitMessageDecision(
	t *testing.T,
	blocked bool,
	decisions []policy.Decision,
) {
	t.Helper()

	if blocked && len(decisions) != 1 {
		t.Fatalf("expected block decision, got %#v", decisions)
	}

	if !blocked && len(decisions) != 0 {
		t.Fatalf("expected allow decision, got %#v", decisions)
	}
}

func TestGitCommitLintAllowsHeredocCommandSubstitutionMessage(t *testing.T) {
	t.Parallel()

	argv, err := shellparse.Fields(
		"git commit -m \"$(cat <<'EOF'\n" +
			"refactor(sql): wire domain values into parameterized SQL builders\n" +
			"\n" +
			"Complete the contract updates.\n" +
			"EOF\n" +
			")\"",
	)
	if err != nil {
		t.Fatalf("parse command: %v", err)
	}

	policyDef := compiledGitSafetyTestBundle(t).Policies["git.commitlint"]

	decisions, err := EvaluateGitCommitLint(policyDef, Context{
		Argv:             argv,
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected allow decision, got %#v", decisions)
	}
}

func TestGitCommitLintNormalizesHeredocMessageFile(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")
	message := "$(cat <<'HEREDOC'\n" +
		"fix(commitlint): accept heredoc message files\n" +
		"\n" +
		"Preserve multi-line commit bodies from command substitution text.\n" +
		"HEREDOC\n" +
		")"

	err := os.WriteFile(messagePath, []byte(message), 0o600)
	if err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	policyDef := compiledGitSafetyTestBundle(t).Policies["git.commitlint"]

	decisions, err := EvaluateGitCommitLint(policyDef, Context{
		Cwd:              repo,
		Files:            []string{"COMMIT_EDITMSG"},
		Scope:            "commit-msg",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected allow decision, got %#v", decisions)
	}
}

func TestGitSafetyEvaluatorsAllowSafeCommands(t *testing.T) {
	t.Parallel()

	tests := safeGitSafetyCases()
	bundle := compiledGitSafetyTestBundle(t)

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			policyDef := bundle.Policies[test.policyID]

			decisions, err := test.evaluator(policyDef, Context{
				Argv:             test.argv,
				AdminApproved:    test.adminApproved,
				EvaluatorOptions: policyDef.Evaluators[0].Options,
			})
			if err != nil {
				t.Fatalf("evaluate: %v", err)
			}

			if len(decisions) != 0 {
				t.Fatalf("expected no decisions, got %#v", decisions)
			}
		})
	}
}

func safeGitSafetyCases() []gitSafetyCase {
	cases := safeBasicGitCases()
	cases = append(cases, safeCommitGitCases()...)

	return cases
}

func safeBasicGitCases() []gitSafetyCase {
	cases := safeBranchGitCases()
	cases = append(cases, safeWorktreeAndSubmoduleGitCases()...)

	return cases
}

func safeBranchGitCases() []gitSafetyCase {
	return []gitSafetyCase{
		{
			name:      "soft reset",
			policyID:  "git.destructive_command",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "reset", "--soft", "HEAD~1"},
		},
		{
			name:      "normal push feature",
			policyID:  "git.force_push_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "push", "origin", "feature"},
		},
		{
			name:      "checkout feature",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "checkout", "feature"},
		},
		{
			name:          "admin approved checkout main",
			policyID:      "git.checkout_protected_branch",
			evaluator:     EvaluateCELExpression,
			argv:          []string{"git", "checkout", "main"},
			adminApproved: true,
		},
		{
			name:      "checkout new branch from origin main",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "checkout", "-b", "feature", "origin/main"},
		},
		{
			name:      "switch new branch from origin main",
			policyID:  "git.checkout_protected_branch",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "switch", "-c", "feature", "origin/main"},
		},
	}
}

func safeWorktreeAndSubmoduleGitCases() []gitSafetyCase {
	return []gitSafetyCase{
		{
			name:      "worktree list",
			policyID:  "git.destructive_worktree",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "worktree", "list"},
		},
		{
			name:      "worktree remove without force",
			policyID:  "git.destructive_worktree",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "worktree", "remove", "../repo-old"},
		},
		{
			name:      "protected submodule remote upgrade",
			policyID:  "git.protected_submodule_update",
			evaluator: EvaluateCELExpression,
			argv: []string{
				"git",
				"submodule",
				"update",
				"--remote",
				"coding-ethos",
			},
		},
		{
			name:      "unprotected submodule recorded sha checkout",
			policyID:  "git.protected_submodule_update",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "submodule", "update", "vendor/other"},
		},
		{
			name:      "git status without change dir",
			policyID:  "git.change_dir_flag",
			evaluator: EvaluateCELExpression,
			argv:      []string{"git", "status"},
		},
		{
			name:      "non-git command mentioning change dir flag",
			policyID:  "git.change_dir_flag",
			evaluator: EvaluateCELExpression,
			argv:      []string{"echo", "git", "-C", "/tmp/repo", "status"},
		},
	}
}

func safeCommitGitCases() []gitSafetyCase {
	return []gitSafetyCase{
		{
			name:      "normal commit message",
			policyID:  "git.commit_attribution",
			evaluator: EvaluateGitCommitAttribution,
			argv:      []string{"git", "commit", "-m", "feat: test"},
		},
		{
			name:      "commit heredoc message",
			policyID:  "git.commitlint",
			evaluator: EvaluateGitCommitLint,
			argv: []string{
				"git",
				"commit",
				"-m",
				"$(cat <<'EOF'\nfix(enrichment): resolve runtime bugs\n\nbody\nEOF\n)",
			},
		},
		{
			name:      "commit heredoc message without cat spacing",
			policyID:  "git.commitlint",
			evaluator: EvaluateGitCommitLint,
			argv: []string{
				"git",
				"commit",
				"-m",
				"$(cat<<'EOF'\nfix(enrichment): resolve runtime bugs\n\nbody\nEOF\n)",
			},
		},
		{
			name:      "commit heredoc message with extra cat spacing",
			policyID:  "git.commitlint",
			evaluator: EvaluateGitCommitLint,
			argv: []string{
				"git",
				"commit",
				"-m",
				"$(cat    <<'EOF'\nfix(enrichment): resolve runtime bugs\n\nbody\nEOF\n)",
			},
		},
	}
}

func compiledGitSafetyTestBundle(tb testing.TB) policy.Bundle {
	tb.Helper()

	return compiledRepoBundle(tb)
}
