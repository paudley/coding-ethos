// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint

import (
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestRunResolvesFileScopePolicies(t *testing.T) {
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
	_, err := Run(policy.ExampleBundle(), Options{Scope: "invalid"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `unsupported lint scope "invalid"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
