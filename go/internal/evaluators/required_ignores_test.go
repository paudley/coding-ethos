// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/repoignore"
)

func TestEvaluateRequiredIgnoresCELAllowsIgnoredPaths(t *testing.T) {
	t.Parallel()

	repo := newRequiredIgnoreRepo(t)
	writeRequiredIgnoreFile(t, repo, strings.Join(repoignore.RuntimePaths(), "\n")+"\n")
	policyDef := compiledRepoBundle(t).Policies["repo.required_ignores"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate required ignores: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluateRequiredIgnoresCELBlocksMissingPaths(t *testing.T) {
	t.Parallel()

	repo := newRequiredIgnoreRepo(t)
	writeRequiredIgnoreFile(t, repo, "*.pyc\n")
	policyDef := compiledRepoBundle(t).Policies["repo.required_ignores"]

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate required ignores: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateRequiredIgnoresCELUsesConfiguredPaths(t *testing.T) {
	t.Parallel()

	repo := newRequiredIgnoreRepo(t)
	writeRequiredIgnoreFile(t, repo, ".coding-ethos/cache/\n")
	policyDef := compiledRepoBundle(t).Policies["repo.required_ignores"]

	options := map[string]any{}
	maps.Copy(options, policyDef.Evaluators[0].Options)

	options["required_ignore_paths"] = []string{"build-cache/"}

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		EvaluatorOptions: options,
	})
	if err != nil {
		t.Fatalf("evaluate required ignores: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func newRequiredIgnoreRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	gitPath := requiredIgnoreGitPath(t)

	cmd := exec.CommandContext(context.Background(), gitPath, "init")

	cmd.Dir = repo

	output, inlineErrA := cmd.CombinedOutput()
	if inlineErrA != nil {
		t.Fatalf("git init: %v\n%s", inlineErrA, output)
	}

	return repo
}

func requiredIgnoreGitPath(t *testing.T) string {
	t.Helper()

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	return gitPath
}

func writeRequiredIgnoreFile(t *testing.T, repo, content string) {
	t.Helper()

	path := filepath.Join(repo, ".gitignore")

	err := os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
}
