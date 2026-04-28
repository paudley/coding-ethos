// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"testing"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluatePytestGatePassesWhenCommandPasses(t *testing.T) {
	t.Parallel()

	decisions, err := evaluators.EvaluatePytestGate(
		externalPolicy("pytest.gate"),
		evaluators.Context{
			EvaluatorOptions: map[string]any{
				"command": []string{"go", "env", "GOVERSION"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate pytest gate: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateGeneratedConfigFreshnessBlocksWhenCommandFails(t *testing.T) {
	t.Parallel()

	decisions, err := evaluators.EvaluateGeneratedConfigFreshness(
		externalPolicy("generated_config.freshness"),
		evaluators.Context{
			EvaluatorOptions: map[string]any{
				"command": []string{"sh", "-c", "echo stale config >&2; exit 7"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate generated config freshness: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(decisions))
	}

	decision := decisions[0]
	if decision.Decision != "block" || decision.PolicyID != "generated_config.freshness" {
		t.Fatalf("unexpected decision: %#v", decision)
	}

	if decision.Evidence["exit_code"] != 7 {
		t.Fatalf("expected non-zero exit evidence: %#v", decision.Evidence)
	}
}

func TestEvaluateExternalCommandAttachesParsedDiagnostics(t *testing.T) {
	t.Parallel()

	decisions, err := evaluators.EvaluateExternalCommand(
		externalPolicy("python.lint"),
		evaluators.Context{
			EvaluatorOptions: map[string]any{
				"command": []string{
					"sh",
					"-c",
					printRuffDiagnosticCommand + "; exit 1",
				},
				"parser": "ruff",
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate external command: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count = %d, want 1", len(decisions))
	}

	diagnostics := decisions[0].Diagnostics
	if len(diagnostics) != 1 {
		t.Fatalf("diagnostic count = %d, want 1: %#v", len(diagnostics), diagnostics)
	}

	if diagnostics[0].Tool != "ruff" ||
		diagnostics[0].File != "pkg/app.py" ||
		diagnostics[0].Code != "F401" {
		t.Fatalf("unexpected diagnostic: %#v", diagnostics[0])
	}
}

func externalPolicy(policyID string) policy.Policy {
	return policy.Policy{
		ID:              policyID,
		DefaultSeverity: "block",
		Message:         "external command failed",
		SupportedModes:  []string{"block", "record"},
		Evaluators:      []policy.Evaluator{{Kind: "external", Name: policyID}},
	}
}

const printRuffDiagnosticCommand = `printf '%s\n' ` +
	`'[{"filename":"pkg/app.py","code":"F401","message":"unused import",` +
	`"location":{"row":4,"column":8}}]'`
