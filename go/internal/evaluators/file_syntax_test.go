// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateFileSyntaxBlocksInvalidYAML(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path := filepath.Join(dir, "bad.yaml")

	inlineErr0 := os.WriteFile(path, []byte("key: [unterminated\n"), 0o600)
	if inlineErr0 != nil {
		t.Fatalf("write test file: %v", inlineErr0)
	}

	decisions, err := EvaluateFileSyntax(syntaxPolicy(), Context{Files: []string{path}})
	if err != nil {
		t.Fatalf("evaluate syntax: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	if got := decisions[0].Diagnostics[0].Tool; got != "syntax" {
		t.Fatalf("diagnostic tool = %q", got)
	}
}

func TestEvaluateFileSyntaxSkipsDirectories(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateFileSyntax(
		syntaxPolicy(),
		Context{Files: []string{t.TempDir()}},
	)
	if err != nil {
		t.Fatalf("evaluate syntax: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions for directory path, got %#v", decisions)
	}
}

func syntaxPolicy() policy.Policy {
	return policy.Policy{
		ID:              "syntax.file_syntax",
		DefaultSeverity: "block",
		Message:         "syntax failed",
		Suggestion:      "fix syntax",
	}
}
