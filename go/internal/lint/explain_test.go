// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestExplainReportsSelectedScopeChecks(t *testing.T) {
	t.Parallel()

	result, err := Explain(policy.ExampleBundle(), ScopeStaged)
	if err != nil {
		t.Fatalf("Explain() returned error: %v", err)
	}

	if result.Scope != ScopeStaged || result.Selected == 0 {
		t.Fatalf("explain result = %#v", result)
	}

	var found bool
	for _, check := range result.Checks {
		if check.CheckID == "git.hook_bypass" {
			found = true
			if check.Status != "selected" || check.Reason == "" {
				t.Fatalf("check = %#v", check)
			}
		}
	}
	if !found {
		t.Fatalf("missing git.hook_bypass in %#v", result.Checks)
	}

	output := FormatExplainResultHuman(result)
	if !strings.Contains(output, "lint scope: staged") ||
		!strings.Contains(output, "git.hook_bypass") {
		t.Fatalf("human output missing expected details:\n%s", output)
	}
}
