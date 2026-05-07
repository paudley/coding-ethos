// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateShellBestPracticesBlocksMissingStrictMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	path := filepath.Join(dir, "script.sh")

	inlineErr0 := os.WriteFile(
		path,
		[]byte("#!/usr/bin/env bash\necho ok\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write test file: %v", inlineErr0)
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

func TestEvaluateShellBestPracticesBlocksInvalidShellSyntaxWithLocation(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")

	content := "#!/usr/bin/env bash\nset -euo pipefail\necho 'unterminated\n"

	inlineErr1 := os.WriteFile(path, []byte(content), 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write test file: %v", inlineErr1)
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

	var syntaxDiagnostic diagnostics.Diagnostic

	for _, diagnostic := range decisions[0].Diagnostics {
		if diagnostic.Message == "shell script has invalid shell syntax" {
			syntaxDiagnostic = diagnostic

			break
		}
	}

	if syntaxDiagnostic.Message == "" ||
		syntaxDiagnostic.Line == 0 ||
		syntaxDiagnostic.Column == 0 {
		t.Fatalf("missing syntax diagnostic location: %#v", decisions[0].Diagnostics)
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
