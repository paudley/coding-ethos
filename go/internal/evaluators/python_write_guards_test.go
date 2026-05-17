// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluatePythonBareExcept(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluatePythonBareExcept(
		pythonWritePolicy("python.bare_except"),
		Context{
			Files:   []string{"src/app.py"},
			Content: "try:\n    run()\nexcept:\n    recover()\n",
		},
	)
	if err != nil {
		t.Fatalf("evaluate bare except: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func TestEvaluatePythonUnexplainedTypeIgnore(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluatePythonUnexplainedTypeIgnore(
		pythonWritePolicy("python.unexplained_type_ignore"),
		Context{
			Files:   []string{"src/app.py"},
			Content: "value = dynamic()  # type: ignore\n",
		},
	)
	if err != nil {
		t.Fatalf("evaluate type ignore: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("expected block decision, got %#v", decisions)
	}
}

func pythonWritePolicy(policyID string) policy.Policy {
	return policy.Policy{
		ID:              policyID,
		Category:        "python",
		Source:          policy.SourceRef{File: "config.yaml"},
		DefaultSeverity: blockDecision,
		SupportedModes:  []string{blockDecision, "advise"},
		Message:         "Python write guard failed.",
		DefenseLayers:   policy.CodeDefenseLayers(),
		Evaluators:      []policy.Evaluator{{Kind: "text", Name: policyID}},
	}
}
