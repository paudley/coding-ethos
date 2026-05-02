// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"bytes"
	"strings"
	"testing"
)

func TestWriteSummaryIncludesPoliciesAndPrinciples(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	err := WriteSummary(&buffer, ExampleBundle())
	if err != nil {
		t.Fatalf("write summary: %v", err)
	}

	summary := buffer.String()
	for _, expected := range []string{
		"# Policy Bundle Summary",
		"`no-conditional-imports`: No Conditional Imports",
		"`git.hook_bypass` [expression/block]",
		"`python.conditional_imports` [python/block]",
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("summary missing %q:\n%s", expected, summary)
		}
	}
}
