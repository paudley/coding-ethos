// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteSummaryIncludesPoliciesAndPrinciples(t *testing.T) {
	var buffer bytes.Buffer
	if err := WriteSummary(&buffer, ExampleBundle()); err != nil {
		t.Fatalf("write summary: %v", err)
	}
	summary := buffer.String()
	for _, expected := range []string{
		"# Policy Bundle Summary",
		"`no-conditional-imports`: No Conditional Imports",
		"`git.hook_bypass` [git/block]",
		"`python.conditional_imports` [python/block]",
	} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("summary missing %q:\n%s", expected, summary)
		}
	}
}
