// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

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

func TestEvaluateShellBestPracticesRequiresOnlyExistingCommonHelper(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	initializeStagedAdminGitRepo(t, repo)
	scriptsDir := filepath.Join(repo, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o700); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "work.sh")
	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	if err := os.WriteFile(scriptPath, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	context := Context{
		Cwd:   repo,
		Files: []string{scriptPath},
		EvaluatorOptions: map[string]any{
			"require_common_for_prefixes": []any{scriptsDir + string(filepath.Separator)},
		},
	}
	decisions, err := EvaluateShellBestPractices(shellBestPracticesPolicy(), context)
	if err != nil {
		t.Fatalf("evaluate without common helper: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("missing common helper must disable convention: %#v", decisions)
	}

	commonPath := filepath.Join(scriptsDir, "common.sh")
	if err = os.WriteFile(commonPath, content, 0o600); err != nil {
		t.Fatalf("write common helper: %v", err)
	}
	runGit(t, repo, "add", "scripts/common.sh")

	decisions, err = EvaluateShellBestPractices(shellBestPracticesPolicy(), context)
	if err != nil {
		t.Fatalf("evaluate with common helper: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("existing common helper must activate convention: %#v", decisions)
	}
	if got := decisions[0].Diagnostics[0].Message; got !=
		"scripts/ shell files must source the repository common shell helpers" {
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
