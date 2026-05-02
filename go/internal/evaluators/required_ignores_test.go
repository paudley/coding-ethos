// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateRequiredIgnoresResolvesIgnoredPaths(t *testing.T) {
	t.Parallel()

	repo := newRequiredIgnoreRepo(t)
	writeRequiredIgnoreFile(t, repo, ".code-ethos/cache/\n.coding-ethos/\n")

	decisions, err := EvaluateRequiredIgnores(
		requiredIgnoresPolicy(),
		Context{Cwd: repo},
	)
	if err != nil {
		t.Fatalf("evaluate required ignores: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluateRequiredIgnoresBlocksMissingPaths(t *testing.T) {
	t.Parallel()

	repo := newRequiredIgnoreRepo(t)
	writeRequiredIgnoreFile(t, repo, "*.pyc\n")

	decisions, err := EvaluateRequiredIgnores(
		requiredIgnoresPolicy(),
		Context{Cwd: repo},
	)
	if err != nil {
		t.Fatalf("evaluate required ignores: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected one decision, got %#v", decisions)
	}

	missing, ok := decisions[0].Evidence["missing_ignores"].([]string)
	if !ok {
		t.Fatalf("missing ignores evidence mismatch: %#v", decisions[0].Evidence)
	}

	if len(missing) != 3 || missing[0] != ".code-ethos/cache/" {
		t.Fatalf("missing ignores mismatch: %#v", missing)
	}
}

func TestEvaluateRequiredIgnoresUsesConfiguredPaths(t *testing.T) {
	t.Parallel()

	repo := newRequiredIgnoreRepo(t)
	writeRequiredIgnoreFile(t, repo, ".coding-ethos/\n")

	policyDef := requiredIgnoresPolicy()
	policyDef.Evaluators[0].Options = map[string]any{
		"paths": []string{"build-cache/"},
	}

	decisions, err := EvaluateRequiredIgnores(
		policyDef,
		Context{
			Cwd:              repo,
			EvaluatorOptions: policyDef.Evaluators[0].Options,
		},
	)
	if err != nil {
		t.Fatalf("evaluate required ignores: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("expected one decision, got %#v", decisions)
	}

	missing := decisions[0].Evidence["missing_ignores"].([]string)
	if missing[0] != "build-cache/" {
		t.Fatalf("missing ignores mismatch: %#v", missing)
	}
}

func newRequiredIgnoreRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	cmd := gitCommand(repo, "init")
	cmd.Dir = repo
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	return repo
}

func writeRequiredIgnoreFile(t *testing.T, repo string, content string) {
	t.Helper()

	path := filepath.Join(repo, ".gitignore")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}
}

func requiredIgnoresPolicy() policy.Policy {
	return policy.Policy{
		ID:              "filesystem.required_ignores",
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		DefenseLayers:   policy.DefenseLayers{Enforce: "block"},
		Message:         "required ignores missing",
		PrincipleIDs:    []string{"radical-visibility"},
		Evaluators: []policy.Evaluator{{
			Kind: "git_state",
			Name: "filesystem.required_ignores",
		}},
	}
}
