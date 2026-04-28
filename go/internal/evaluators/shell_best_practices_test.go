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

func TestEvaluateShellBestPracticesBlocksMissingStrictMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	if err := os.WriteFile(path, []byte("#!/usr/bin/env bash\necho ok\n"), 0o600); err != nil {
		t.Fatalf("write test file: %v", err)
	}

	decisions, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{Files: []string{path}},
	)
	if err != nil {
		t.Fatalf("evaluate shell best practices: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
	if got := decisions[0].Diagnostics[0].Message; got != "missing 'set -euo pipefail'" {
		t.Fatalf("diagnostic message = %q", got)
	}
}

func shellBestPracticesPolicy() policy.Policy {
	return policy.Policy{
		ID:              "shell.best_practices",
		DefaultSeverity: "block",
		Message:         "shell practice failed",
		Suggestion:      "fix shell practice",
	}
}
