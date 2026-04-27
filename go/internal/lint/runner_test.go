// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	. "blackcat.ca/coding-ethos/go/internal/lint"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunResolvesFileScopePolicies(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Scope: ScopeFiles,
		Files: []string{"src/app.py"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Status != "resolved" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 1 {
		t.Fatalf("decision count mismatch: got %d", len(result.Decisions))
	}

	decision := result.Decisions[0]
	if decision.PolicyID != "python.conditional_imports" {
		t.Fatalf("policy mismatch: got %q", decision.PolicyID)
	}

	if decision.Decision != "record" || decision.Severity != "record" {
		t.Fatalf("decision should be record/record: %#v", decision)
	}
}

func TestRunMapsChangedScopeToFiles(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{Scope: ScopeChanged})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Scope != ScopeChanged {
		t.Fatalf("scope mismatch: got %q", result.Scope)
	}

	if len(result.Decisions) == 0 {
		t.Fatal("expected changed scope to resolve file policies")
	}
}

func TestRunRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	_, err := Run(policy.ExampleBundle(), Options{Scope: "invalid"})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), `unsupported lint scope "invalid"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunUsesRegisteredEvaluator(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Scope: ScopeStaged,
		Argv:  []string{"git", "commit", "--no-verify", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	var found bool

	for _, decision := range result.Decisions {
		if decision.PolicyID == "git.hook_bypass" {
			found = true

			if decision.Decision != "block" {
				t.Fatalf("hook bypass decision mismatch: %#v", decision)
			}
		}
	}

	if !found {
		t.Fatalf("missing git.hook_bypass decision: %#v", result.Decisions)
	}

	if result.Status != "blocked" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func TestRunRejectsPolicyWithNoRegisteredEvaluator(t *testing.T) {
	t.Parallel()

	bundle := policy.Bundle{
		Policies: map[string]policy.Policy{
			"python.missing_evaluator": {
				ID:              "python.missing_evaluator",
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Evaluators:      []policy.Evaluator{{Name: "python.missing_evaluator"}},
				DefenseLayers:   policy.DefenseLayers{Enforce: "block"},
				Message:         "missing evaluator",
				PrincipleIDs:    []string{"static-analysis-is-the-first-line-of-defense"},
			},
		},
		Dispatch: policy.Dispatch{
			Linter: map[string][]string{
				ScopeFiles: []string{"python.missing_evaluator"},
			},
		},
	}

	_, err := Run(bundle, Options{Scope: ScopeFiles, Files: []string{"src/app.py"}})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "lint policy has no registered evaluator") {
		t.Fatalf("unexpected error: %v", err)
	}
}
