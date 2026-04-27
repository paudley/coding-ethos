// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateProtectedPathBlocksCommandReference(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{Command: "sudo chmod +x /usr/bin/got"},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathBlocksFileTarget(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{Files: []string{"/usr/bin/got"}},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluateProtectedPathUsesConfiguredPaths(t *testing.T) {
	t.Parallel()

	policyDef := protectedPathPolicy()

	decisions, err := EvaluateProtectedPath(
		policyDef,
		Context{
			Files: []string{"/opt/blocked"},
			EvaluatorOptions: map[string]any{
				"paths": []any{"/opt/blocked"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate protected path: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func protectedPathPolicy() policy.Policy {
	return policy.Policy{
		ID:       "filesystem.protected_path",
		Category: "filesystem",
		Source: policy.SourceRef{
			File: "config.yaml",
			Path: "filesystem.protected_path",
		},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Protected paths must not be modified.",
		DefenseLayers:   policy.GitDefenseLayers("block", "", "block", "", ""),
		Evaluators: []policy.Evaluator{{
			Kind: "path",
			Name: "filesystem.protected_path",
		}},
	}
}
