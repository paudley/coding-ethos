// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy_test

import (
	. "blackcat.ca/coding-ethos/go/internal/policy"
	"bytes"
	"strings"
	"testing"
)

func TestExplainPolicyWritesPolicyDetails(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	err := ExplainPolicy(&buffer, ExampleBundle(), "git.hook_bypass")
	if err != nil {
		t.Fatalf("explain policy: %v", err)
	}

	output := buffer.String()
	for _, expected := range []string{
		"# git.hook_bypass",
		"Category: `git`",
		"Principles: `one-path-for-critical-operations`",
		"Hook bypass is forbidden.",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("explanation missing %q:\n%s", expected, output)
		}
	}
}

func TestExplainPolicyRejectsUnknownPolicy(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer

	err := ExplainPolicy(&buffer, ExampleBundle(), "missing.policy")
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), `unknown policy "missing.policy"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}
