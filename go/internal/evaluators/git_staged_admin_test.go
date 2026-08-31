// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const recordDecision = "record"

func TestBlockedAdminFilesFindsBasenamesAndDirs(t *testing.T) {
	t.Parallel()

	blocked := BlockedAdminFiles(
		[]string{
			"pyproject.toml",
			"src/app.py",
			"pre-commit/hooks/run.sh",
			"docs/notes.md",
		},
		nil,
	)
	if len(blocked) != 2 {
		t.Fatalf("blocked count mismatch: %#v", blocked)
	}

	if blocked[0] != "pyproject.toml" || blocked[1] != "pre-commit/hooks/run.sh" {
		t.Fatalf("blocked files mismatch: %#v", blocked)
	}
}

func TestBlockedAdminFilesUsesConfiguredPatterns(t *testing.T) {
	t.Parallel()

	blocked := BlockedAdminFiles(
		[]string{"custom.lock", "ops/config.yml", "pyproject.toml"},
		map[string]any{
			"basenames": []any{"custom.lock"},
			"dirs":      []any{"ops"},
		},
	)
	if len(blocked) != 2 {
		t.Fatalf("blocked count mismatch: %#v", blocked)
	}

	if blocked[0] != "custom.lock" || blocked[1] != "ops/config.yml" {
		t.Fatalf("blocked files mismatch: %#v", blocked)
	}
}

func TestEvaluateGitStagedAdminFilesBlocksWithoutAdminApproval(t *testing.T) {
	t.Parallel()

	repo := stagedAdminRepo(t)

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "admin change"},
			Cwd:  repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}

	for _, want := range []string{
		"Human/admin handoff",
		"Administrative staged files: bin/coding-ethos-run.",
		"Agent action: stop trying to commit these files.",
		"git commit -m 'admin change'.",
		"Note: --admin-approved is only valid inside the coding-ethos repo admin wrapper.",
	} {
		if !strings.Contains(decisions[0].Suggestion, want) {
			t.Fatalf("suggestion missing %q: %q", want, decisions[0].Suggestion)
		}
	}

	assertStringSliceEvidence(
		t,
		decisions[0].Evidence,
		"files",
		[]string{"bin/coding-ethos-run"},
	)
	assertStringSliceEvidence(
		t,
		decisions[0].Evidence,
		"staged_files",
		[]string{"bin/coding-ethos-run"},
	)

	if len(decisions[0].Diagnostics) != 1 {
		t.Fatalf("diagnostic count mismatch: %#v", decisions[0].Diagnostics)
	}

	diagnostic := decisions[0].Diagnostics[0]
	if diagnostic.File != "bin/coding-ethos-run" ||
		diagnostic.PolicyID != "git.staged_admin_files" ||
		diagnostic.Severity != blockDecision {
		t.Fatalf("diagnostic mismatch: %#v", diagnostic)
	}
}

func TestEvaluateGitStagedAdminFilesOmitsWrapperWarningInsideCodingEthos(t *testing.T) {
	t.Parallel()

	repo := stagedAdminRepo(t)
	writeCodingEthosMarkers(t, repo)

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "admin change"},
			Cwd:  repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if strings.Contains(decisions[0].Suggestion, "--admin-approved is only valid") {
		t.Fatalf("suggestion should omit wrapper warning: %q", decisions[0].Suggestion)
	}
}

func TestEvaluateGitStagedAdminFilesRecordsWithAdminApproval(t *testing.T) {
	t.Parallel()

	repo := stagedAdminRepo(t)

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			AdminApproved: true,
			Argv:          []string{"git", "commit", "-m", "admin change"},
			Cwd:           repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].Decision != recordDecision ||
		decisions[0].Severity != recordDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}

	assertStringSliceEvidence(
		t,
		decisions[0].Evidence,
		"files",
		[]string{"bin/coding-ethos-run"},
	)
	if len(decisions[0].Diagnostics) != 1 ||
		decisions[0].Diagnostics[0].Severity != recordDecision {
		t.Fatalf("diagnostic mismatch: %#v", decisions[0].Diagnostics)
	}
}

func TestEvaluateGitStagedAdminFilesRecordsExactGeneratedConfig(t *testing.T) {
	t.Parallel()

	ethosRoot, repo := syncedGeneratedConfigRepo(t)
	initializeStagedAdminGitRepo(t, repo)
	runGit(t, repo, "add", ".pylintrc")

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "generated config"},
			Cwd:  repo,
			EvaluatorOptions: map[string]any{
				"ethos_root": ethosRoot,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged generated admin file: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != recordDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}
	assertStringSliceEvidence(
		t,
		decisions[0].Evidence,
		"verified_generated_files",
		[]string{".pylintrc"},
	)
}

func TestEvaluateGitStagedAdminFilesBlocksDivergentStagedGeneratedConfig(
	t *testing.T,
) {
	t.Parallel()

	ethosRoot, repo := syncedGeneratedConfigRepo(t)
	initializeStagedAdminGitRepo(t, repo)
	pylintPath := filepath.Join(repo, ".pylintrc")
	expected, err := os.ReadFile(pylintPath)
	if err != nil {
		t.Fatalf("read generated Pylint config: %v", err)
	}
	if err = os.WriteFile(
		pylintPath,
		[]byte("[MAIN]\nignore-patterns=.*\n"),
		0o600,
	); err != nil {
		t.Fatalf("write divergent Pylint config: %v", err)
	}
	runGit(t, repo, "add", ".pylintrc")
	if err = os.WriteFile(pylintPath, expected, 0o600); err != nil {
		t.Fatalf("restore working-tree Pylint config: %v", err)
	}

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "divergent config"},
			Cwd:  repo,
			EvaluatorOptions: map[string]any{
				"ethos_root": ethosRoot,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate divergent staged generated admin file: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}
}

func TestEvaluateGitStagedAdminFilesRecordsAdminFileInheritedFromMergeParent(
	t *testing.T,
) {
	t.Parallel()

	repo := stagedAdminMergeRepo(t)

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "merge main"},
			Cwd:  repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].Decision != recordDecision ||
		decisions[0].Severity != recordDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}
	if decisions[0].Message != "Administrative staged files match the current merge parent." {
		t.Fatalf("message mismatch: %q", decisions[0].Message)
	}
	assertStringSliceEvidence(
		t,
		decisions[0].Evidence,
		"merge_parent_files",
		[]string{".pre-commit-config.yaml"},
	)
	if decisions[0].Evidence["merge_parent"] == "" {
		t.Fatalf("missing merge parent evidence: %#v", decisions[0].Evidence)
	}
	if len(decisions[0].Diagnostics) != 1 ||
		decisions[0].Diagnostics[0].Severity != recordDecision {
		t.Fatalf("diagnostic mismatch: %#v", decisions[0].Diagnostics)
	}
}

func TestEvaluateGitStagedAdminFilesBlocksAdminFileEditedDuringMerge(t *testing.T) {
	t.Parallel()

	repo := stagedAdminMergeRepo(t)
	adminPath := filepath.Join(repo, ".pre-commit-config.yaml")
	if err := os.WriteFile(adminPath, []byte("agent edit\n"), 0o600); err != nil {
		t.Fatalf("edit admin file: %v", err)
	}
	runGit(t, repo, "add", ".pre-commit-config.yaml")

	decisions, err := EvaluateGitStagedAdminFiles(
		stagedAdminPolicy(),
		Context{
			Argv: []string{"git", "commit", "-m", "merge main"},
			Cwd:  repo,
		},
	)
	if err != nil {
		t.Fatalf("evaluate staged admin: %v", err)
	}

	if len(decisions) != 1 || decisions[0].Decision != blockDecision {
		t.Fatalf("decision mismatch: %#v", decisions)
	}
	assertStringSliceEvidence(
		t,
		decisions[0].Evidence,
		"files",
		[]string{".pre-commit-config.yaml"},
	)
	if _, ok := decisions[0].Evidence["merge_parent_files"]; ok {
		t.Fatalf("edited file must not be merge-parent evidence: %#v", decisions[0].Evidence)
	}
}

func assertStringSliceEvidence(
	t *testing.T,
	evidence map[string]any,
	key string,
	want []string,
) {
	t.Helper()

	got, ok := evidence[key].([]string)
	if !ok {
		t.Fatalf("evidence %s has type %T: %#v", key, evidence[key], evidence[key])
	}

	if len(got) != len(want) {
		t.Fatalf("evidence %s length mismatch: got %#v want %#v", key, got, want)
	}

	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("evidence %s mismatch: got %#v want %#v", key, got, want)
		}
	}
}

func writeCodingEthosMarkers(t *testing.T, repo string) {
	t.Helper()

	for _, path := range []string{
		"coding_ethos.yml",
		"config.yaml",
		"go/cmd/coding-ethos-run",
	} {
		fullPath := filepath.Join(repo, path)

		err := os.MkdirAll(filepath.Dir(fullPath), 0o755)
		if err != nil {
			t.Fatalf("create marker parent: %v", err)
		}

		err = os.WriteFile(fullPath, []byte("marker\n"), 0o600)
		if err != nil {
			t.Fatalf("write marker %s: %v", path, err)
		}
	}
}

func stagedAdminRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	initializeStagedAdminGitRepo(t, repo)

	hookPath := filepath.Join(repo, "bin")

	err := os.MkdirAll(hookPath, 0o755)
	if err != nil {
		t.Fatalf("create hook dir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(hookPath, "coding-ethos-run"),
		[]byte("#!/usr/bin/env bash\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write hook file: %v", err)
	}

	runGit(t, repo, "add", "bin/coding-ethos-run")

	return repo
}

func initializeStagedAdminGitRepo(t *testing.T, repo string) {
	t.Helper()

	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
}

func stagedAdminMergeRepo(t *testing.T) string {
	t.Helper()

	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test")
	runGit(t, repo, "branch", "-M", "main")

	writeTestFile(t, repo, ".pre-commit-config.yaml", "initial\n")
	writeTestFile(t, repo, "feature.txt", "initial\n")
	runGit(t, repo, "add", ".pre-commit-config.yaml", "feature.txt")
	runGit(t, repo, "commit", "-m", "initial")
	runGit(t, repo, "checkout", "-b", "feature")
	writeTestFile(t, repo, "feature.txt", "feature\n")
	runGit(t, repo, "add", "feature.txt")
	runGit(t, repo, "commit", "-m", "feature")
	runGit(t, repo, "checkout", "main")
	writeTestFile(t, repo, ".pre-commit-config.yaml", "main update\n")
	runGit(t, repo, "add", ".pre-commit-config.yaml")
	runGit(t, repo, "commit", "-m", "admin update")
	runGit(t, repo, "checkout", "feature")
	runGit(t, repo, "merge", "main", "--no-commit", "--no-ff")

	return repo
}

func writeTestFile(t *testing.T, repo, path, contents string) {
	t.Helper()

	fullPath := filepath.Join(repo, path)
	if err := os.WriteFile(fullPath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func stagedAdminPolicy() policy.Policy {
	return policy.Policy{
		ID:              "git.staged_admin_files",
		DefaultSeverity: blockDecision,
		Message:         "Administrative staged files require explicit handling.",
	}
}
