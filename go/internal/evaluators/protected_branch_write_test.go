// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"maps"
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
)

func TestEvaluateProtectedBranchWriteBlocksMainWrite(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := compiledRepoBundle(t).Policies["filesystem.protected_branch_write"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Tool:             "Write",
		Cwd:              repo,
		Files:            []string{"src/app.py"},
		EvaluatorOptions: policyDef.Evaluators[0].Options,
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
	policyDef := compiledRepoBundle(t).Policies["filesystem.protected_branch_write"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Tool:             "Write",
		Cwd:              repo,
		Files:            []string{"docs/plans/next.md"},
		EvaluatorOptions: policyDef.Evaluators[0].Options,
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
	policyDef := compiledRepoBundle(t).Policies["filesystem.protected_branch_write"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Tool:             "Bash",
		Cwd:              repo,
		Command:          "git commit -m noop",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate protected branch write: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluateProtectedBranchWriteAllowsReadOnlySed(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := compiledRepoBundle(t).Policies["filesystem.protected_branch_write"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Tool:             "Bash",
		Cwd:              repo,
		Command:          "sed -n '1,120p' repo_config.yaml",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate protected branch write: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluateProtectedBranchWriteBlocksInPlaceSed(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := compiledRepoBundle(t).Policies["filesystem.protected_branch_write"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Tool:             "Bash",
		Cwd:              repo,
		Command:          "sed -i 's/old/new/' app.py",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate protected branch write: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedBranchWriteUsesConfiguredBranches(t *testing.T) {
	t.Parallel()

	repo := initProtectedBranchRepo(t)
	policyDef := compiledRepoBundle(t).Policies["filesystem.protected_branch_write"]

	options := map[string]any{}
	maps.Copy(options, policyDef.Evaluators[0].Options)

	options["protected_branches"] = []any{"release"}

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Tool:             "Write",
		Cwd:              repo,
		Files:            []string{"src/app.py"},
		EvaluatorOptions: options,
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
