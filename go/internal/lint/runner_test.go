// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	. "blackcat.ca/coding-ethos/go/internal/lint"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/diagnostics"
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

func TestRunExecutesSmokeExternalPolicies(t *testing.T) {
	t.Parallel()

	bundle := policy.Bundle{
		Policies: map[string]policy.Policy{
			"pytest.gate": {
				ID:              "pytest.gate",
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Evaluators: []policy.Evaluator{{
					Kind: "external",
					Name: "pytest.gate",
					Options: map[string]any{
						"command": []string{
							"sh",
							"-c",
							printRuffDiagnosticCommand + "; exit 9",
						},
						"parser": "ruff",
					},
				}},
				DefenseLayers: policy.DefenseLayers{Enforce: "block"},
				Message:       "pytest failed",
				PrincipleIDs:  []string{"testing-as-specification"},
			},
		},
		EvidenceMaps: []diagnostics.EvidenceMap{
			{
				Source:       "ruff",
				Codes:        []string{"F401"},
				PolicyID:     "python.direct_imports",
				PrincipleIDs: []string{"protocol-first-design"},
				Confidence:   "medium",
				Meaning:      "unused import evidence",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Remove the unused import or use the protocol.",
					Steps:   []string{"Remove unused imports."},
					Rerun:   []string{"make pre-commit"},
				},
			},
		},
		Dispatch: policy.Dispatch{
			Linter: map[string][]string{
				ScopeSmoke: []string{"pytest.gate"},
			},
		},
	}

	result, err := Run(bundle, Options{Scope: ScopeSmoke})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Status != "blocked" {
		t.Fatalf("status = %q, want blocked", result.Status)
	}

	if got := result.Decisions[0].Evidence["exit_code"]; got != 9 {
		t.Fatalf("exit evidence = %#v, want 9", got)
	}

	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F401" {
		t.Fatalf("diagnostics = %#v, want parsed F401", result.Diagnostics)
	}

	if result.Diagnostics[0].PolicyID != "python.direct_imports" {
		t.Fatalf("diagnostic policy = %q", result.Diagnostics[0].PolicyID)
	}

	if result.Diagnostics[0].Advice != "Remove the unused import or use the protocol." {
		t.Fatalf("diagnostic advice = %q", result.Diagnostics[0].Advice)
	}
}

const printRuffDiagnosticCommand = `printf '%s\n' ` +
	`'[{"filename":"pkg/app.py","code":"F401","message":"unused import",` +
	`"location":{"row":4,"column":8}}]'`
