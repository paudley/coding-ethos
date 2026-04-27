// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateProtectedBranchWriteBlocksMainWrite(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := protectedBranchWritePolicy()

	decisions, err := EvaluateProtectedBranchWrite(policyDef, Context{
		Tool:  "Write",
		Cwd:   repo,
		Files: []string{"src/app.py"},
	})
	if err != nil {
		t.Fatalf("evaluate protected branch write: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedBranchWriteAllowsPlanFile(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := protectedBranchWritePolicy()

	decisions, err := EvaluateProtectedBranchWrite(policyDef, Context{
		Tool:  "Write",
		Cwd:   repo,
		Files: []string{"docs/plans/next.md"},
	})
	if err != nil {
		t.Fatalf("evaluate protected branch write: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluateProtectedBranchWriteAllowsCommitVerification(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := protectedBranchWritePolicy()

	decisions, err := EvaluateProtectedBranchWrite(policyDef, Context{
		Tool:    "Bash",
		Cwd:     repo,
		Command: "git commit -m noop",
	})
	if err != nil {
		t.Fatalf("evaluate protected branch write: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func initProtectedBranchRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGit(t, repo, "init", "--initial-branch", "main")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")

	err := os.MkdirAll(filepath.Join(repo, "src"), 0o755)
	if err != nil {
		t.Fatalf("mkdir src: %v", err)
	}

	return repo
}

func protectedBranchWritePolicy() policy.Policy {
	return policy.Policy{
		ID:              "filesystem.protected_branch_write",
		Category:        "filesystem",
		Source:          policy.SourceRef{File: "config.yaml"},
		DefaultSeverity: blockDecision,
		SupportedModes:  []string{blockDecision, "record"},
		Message:         "Protected branch writes are forbidden.",
		DefenseLayers:   policy.GitDefenseLayers(blockDecision, "", blockDecision, "", ""),
		Evaluators: []policy.Evaluator{{
			Kind: "git_state",
			Name: "filesystem.protected_branch_write",
		}},
	}
}
