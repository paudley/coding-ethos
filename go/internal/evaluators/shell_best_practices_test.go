// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestEvaluateShellBestPracticesBlocksMissingStrictMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	initializeStagedAdminGitRepo(t, dir)

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
		Context{Cwd: dir, Files: []string{path}},
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
	initializeStagedAdminGitRepo(t, dir)
	path := filepath.Join(dir, "script.sh")

	content := "#!/usr/bin/env bash\nset -euo pipefail\necho 'unterminated\n"

	inlineErr1 := os.WriteFile(path, []byte(content), 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write test file: %v", inlineErr1)
	}

	decisions, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{Cwd: dir, Files: []string{path}},
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

func TestEvaluateShellBestPracticesRejectsMissingRepositoryContext(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "script.sh")
	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	_, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{Files: []string{path}},
	)
	if err == nil {
		t.Fatal("missing repository context must fail helper validation")
	}
	if !strings.Contains(err.Error(), "repository working directory is required") {
		t.Fatalf("missing repository context error = %q", err)
	}
}

func TestEvaluateShellBestPracticesSkipsRepositoryInspectionForNonShellFiles(
	t *testing.T,
) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	if err := os.WriteFile(path, []byte("package main\n"), 0o600); err != nil {
		t.Fatalf("write non-shell source: %v", err)
	}

	decisions, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{Cwd: dir, Files: []string{path}},
	)
	if err != nil {
		t.Fatalf("non-shell source triggered repository inspection: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("non-shell source decisions = %#v", decisions)
	}
}

func TestEvaluateShellBestPracticesPropagatesGitInspectionFailure(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	scriptsDir := filepath.Join(repo, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o700); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}

	scriptPath := filepath.Join(scriptsDir, "work.sh")
	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	if err := os.WriteFile(scriptPath, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(repo, ".git"),
		[]byte("gitdir: missing\n"),
		0o600,
	); err != nil {
		t.Fatalf("write invalid Git directory marker: %v", err)
	}

	_, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{
			Cwd:   repo,
			Files: []string{scriptPath},
			EvaluatorOptions: map[string]any{
				"require_common_for_prefixes": []any{
					scriptsDir + string(filepath.Separator),
				},
			},
		},
	)
	if err == nil {
		t.Fatal("git inspection failure must fail helper validation")
	}
	if !strings.Contains(err.Error(), "git worktree root") {
		t.Fatalf("git inspection error = %q", err)
	}
}

func TestEvaluateShellBestPracticesIgnoresUnrelatedCommonHelper(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	initializeStagedAdminGitRepo(t, repo)
	scriptsDir := filepath.Join(repo, "scripts")
	fixturesDir := filepath.Join(repo, "fixtures")
	if err := os.MkdirAll(scriptsDir, 0o700); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}
	if err := os.MkdirAll(fixturesDir, 0o700); err != nil {
		t.Fatalf("create fixtures directory: %v", err)
	}

	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	scriptPath := filepath.Join(scriptsDir, "work.sh")
	if err := os.WriteFile(scriptPath, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}
	fixturePath := filepath.Join(fixturesDir, "common.sh")
	if err := os.WriteFile(fixturePath, content, 0o600); err != nil {
		t.Fatalf("write unrelated common helper: %v", err)
	}
	runGit(t, repo, "add", "fixtures/common.sh")

	decisions, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{
			Cwd:   repo,
			Files: []string{scriptPath},
			EvaluatorOptions: map[string]any{
				"require_common_for_prefixes": []any{
					scriptsDir + string(filepath.Separator),
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate with unrelated common helper: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("unrelated common helper must not activate convention: %#v", decisions)
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

// Post-edit lint passes the absolute paths of every changed file, including
// files the edit deleted. A deleted path must be skipped outright: it must not
// fail path normalization, and it must not be reported as an empty file that
// violates every shell convention.
func TestEvaluateShellBestPracticesSkipsDeletedAbsoluteShellPaths(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	initializeStagedAdminGitRepo(t, repo)
	scriptsDir := filepath.Join(repo, "scripts")
	if err := os.MkdirAll(scriptsDir, 0o700); err != nil {
		t.Fatalf("create scripts directory: %v", err)
	}

	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	commonPath := filepath.Join(scriptsDir, "common.sh")
	if err := os.WriteFile(commonPath, content, 0o600); err != nil {
		t.Fatalf("write common helper: %v", err)
	}
	runGit(t, repo, "add", "scripts/common.sh")

	deletedPath := filepath.Join(scriptsDir, "removed.sh")

	decisions, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{
			Cwd:   repo,
			Files: []string{deletedPath},
			EvaluatorOptions: map[string]any{
				"require_common_for_prefixes": []any{
					scriptsDir + string(filepath.Separator),
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("deleted shell path failed evaluation: %v", err)
	}
	if len(decisions) != 0 {
		t.Fatalf("deleted shell path produced decisions: %#v", decisions)
	}
}

func TestEvaluateShellBestPracticesResolvesCommonHelperFromNestedCWD(
	t *testing.T,
) {
	t.Parallel()

	repo := t.TempDir()
	initializeStagedAdminGitRepo(t, repo)
	scriptsDir := filepath.Join(repo, "scripts")
	nestedDir := filepath.Join(repo, "work", "nested")
	for _, directory := range []string{scriptsDir, nestedDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", directory, err)
		}
	}

	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	commonPath := filepath.Join(scriptsDir, "common.sh")
	if err := os.WriteFile(commonPath, content, 0o600); err != nil {
		t.Fatalf("write common helper: %v", err)
	}
	runGit(t, repo, "add", "scripts/common.sh")

	scriptPath := filepath.Join(scriptsDir, "work.sh")
	if err := os.WriteFile(scriptPath, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	for _, test := range []struct {
		name    string
		options map[string]any
	}{
		{
			name: "absolute prefix",
			options: map[string]any{
				"require_common_for_prefixes": []any{
					scriptsDir + string(filepath.Separator),
				},
			},
		},
		{name: "default relative prefix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			decisions, err := EvaluateShellBestPractices(
				shellBestPracticesPolicy(),
				Context{
					Cwd:              nestedDir,
					Files:            []string{scriptPath},
					EvaluatorOptions: test.options,
				},
			)
			if err != nil {
				t.Fatalf(
					"evaluate common helper from nested working directory: %v",
					err,
				)
			}
			if len(decisions) != 1 {
				t.Fatalf("nested working directory decisions = %#v", decisions)
			}
			if got := decisions[0].Diagnostics[0].Message; got !=
				"scripts/ shell files must source the repository common shell helpers" {
				t.Fatalf("diagnostic message = %q", got)
			}
		})
	}
}

func TestEvaluateShellBestPracticesNormalizesSymlinkedAbsolutePrefix(
	t *testing.T,
) {
	t.Parallel()

	fixtureRoot := t.TempDir()
	repo := filepath.Join(fixtureRoot, "repo")
	if err := os.Mkdir(repo, 0o700); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	initializeStagedAdminGitRepo(t, repo)

	scriptsDir := filepath.Join(repo, "scripts")
	nestedDir := filepath.Join(repo, "work", "nested")
	for _, directory := range []string{scriptsDir, nestedDir} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("create fixture directory %s: %v", directory, err)
		}
	}

	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	commonPath := filepath.Join(scriptsDir, "common.sh")
	if err := os.WriteFile(commonPath, content, 0o600); err != nil {
		t.Fatalf("write common helper: %v", err)
	}
	runGit(t, repo, "add", "scripts/common.sh")

	scriptPath := filepath.Join(scriptsDir, "work.sh")
	if err := os.WriteFile(scriptPath, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	repoAlias := filepath.Join(fixtureRoot, "repo-alias")
	if err := os.Symlink(repo, repoAlias); err != nil {
		t.Skipf("create repository symlink: %v", err)
	}

	decisions, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{
			Cwd:   filepath.Join(repoAlias, "work", "nested"),
			Files: []string{scriptPath},
			EvaluatorOptions: map[string]any{
				"require_common_for_prefixes": []any{
					filepath.Join(repoAlias, "scripts") + string(filepath.Separator),
				},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate symlinked common helper prefix: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("symlinked absolute prefix decisions = %#v", decisions)
	}
}

func TestEvaluateShellBestPracticesRejectsAbsolutePrefixOutsideRepository(
	t *testing.T,
) {
	t.Parallel()

	repo := t.TempDir()
	initializeStagedAdminGitRepo(t, repo)
	scriptPath := filepath.Join(repo, "work.sh")
	content := []byte("#!/usr/bin/env bash\nset -euo pipefail\necho ok\n")
	if err := os.WriteFile(scriptPath, content, 0o600); err != nil {
		t.Fatalf("write script: %v", err)
	}

	outsidePrefix := t.TempDir() + string(filepath.Separator)
	_, err := EvaluateShellBestPractices(
		shellBestPracticesPolicy(),
		Context{
			Cwd:   repo,
			Files: []string{scriptPath},
			EvaluatorOptions: map[string]any{
				"require_common_for_prefixes": []any{outsidePrefix},
			},
		},
	)
	if err == nil {
		t.Fatal("absolute prefix outside repository must fail validation")
	}
	if !strings.Contains(err.Error(), "outside the repository") {
		t.Fatalf("outside repository error = %q", err)
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
