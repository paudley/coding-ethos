// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lint_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	. "blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const statusBlocked = "blocked"

func TestRunResolvesFileScopePolicies(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Scope: ScopeFiles,
		Files: []string{"src/app.py"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Status != "resolved" {
		t.Fatalf("status mismatch: got %q", result.Status)
	}

	if len(result.Decisions) != 2 {
		t.Fatalf("decision count mismatch: got %d", len(result.Decisions))
	}

	for _, decision := range result.Decisions {
		if decision.PolicyID != "python.conditional_imports" &&
			decision.PolicyID != "python.functional_idioms" {
			t.Fatalf("policy mismatch: got %q", decision.PolicyID)
		}

		if decision.Decision != "record" || decision.Severity != "record" {
			t.Fatalf("decision should be record/record: %#v", decision)
		}
	}
}

func TestRunMapsChangedScopeToFiles(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{Scope: ScopeChanged})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Scope != ScopeChanged {
		t.Fatalf("scope mismatch: got %q", result.Scope)
	}

	if len(result.Decisions) == 0 {
		t.Fatal("expected changed scope to resolve file policies")
	}
}

func TestRunRejectsUnknownScope(t *testing.T) {
	t.Parallel()

	_, err := Run(policy.ExampleBundle(), Options{Scope: "invalid"})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), `unsupported lint scope "invalid"`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunAcceptsCutoverScope(t *testing.T) {
	t.Parallel()

	bundle := policy.Bundle{
		Policies: map[string]policy.Policy{
			"filesystem.required_ignores": {
				ID:              "filesystem.required_ignores",
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Evaluators:      []policy.Evaluator{},
				DefenseLayers:   policy.DefenseLayers{Enforce: "block"},
				Message:         "required ignores missing",
				PrincipleIDs:    []string{"radical-visibility"},
			},
		},
		Dispatch: policy.Dispatch{
			Linter: map[string][]string{
				ScopeCutover: {"filesystem.required_ignores"},
			},
		},
	}

	result, err := Run(bundle, Options{Scope: ScopeCutover})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Scope != ScopeCutover {
		t.Fatalf("scope mismatch: got %q", result.Scope)
	}
}

func TestRunUsesRegisteredEvaluator(t *testing.T) {
	t.Parallel()

	result, err := Run(policy.ExampleBundle(), Options{
		Scope: ScopeStaged,
		Argv:  []string{"git", "commit", "--no-verify", "-m", "test"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	var found bool

	for _, decision := range result.Decisions {
		if decision.PolicyID == "git.hook_bypass" {
			found = true

			if decision.Decision != "block" {
				t.Fatalf("hook bypass decision mismatch: %#v", decision)
			}
		}
	}

	if !found {
		t.Fatalf("missing git.hook_bypass decision: %#v", result.Decisions)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func TestRunLimitsForbiddenStringFileContentScanToAutomationSurfaces(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	sourcePath := filepath.Join(repo, "go", "internal", "hooks", "runner.go")

	scriptPath := filepath.Join(repo, "scripts", "touch-hook.sh")
	for _, path := range []string{sourcePath, scriptPath} {
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatalf("mkdir fixture: %v", err)
		}
	}

	content := []byte("coding-ethos-hooks/bin/coding-ethos-policy\n")

	inlineErr0 := os.WriteFile(sourcePath, content, 0o600)
	if inlineErr0 != nil {
		t.Fatalf("write source fixture: %v", inlineErr0)
	}

	inlineErr1 := os.WriteFile(scriptPath, content, 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write script fixture: %v", inlineErr1)
	}

	sourceResult, err := Run(policy.ExampleBundle(), Options{
		Scope: ScopeStaged,
		Cwd:   repo,
		Files: []string{"go/internal/hooks/runner.go"},
	})
	if err != nil {
		t.Fatalf("run source lint: %v", err)
	}

	if sourceResult.Status != "resolved" {
		t.Fatalf(
			"source status mismatch: got %q, decisions %#v",
			sourceResult.Status,
			sourceResult.Decisions,
		)
	}

	scriptResult, err := Run(policy.ExampleBundle(), Options{
		Scope: ScopeStaged,
		Cwd:   repo,
		Files: []string{"scripts/touch-hook.sh"},
	})
	if err != nil {
		t.Fatalf("run script lint: %v", err)
	}

	if scriptResult.Status != statusBlocked ||
		!lintResultHasDecision(scriptResult, "shell.forbidden_strings") {
		t.Fatalf("expected forbidden-string script block, got %#v", scriptResult)
	}
}

func TestRunAcceptsCommitMessageScope(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")

	inlineErr2 := os.WriteFile(messagePath, []byte("bad header\n"), 0o600)
	if inlineErr2 != nil {
		t.Fatalf("write commit message: %v", inlineErr2)
	}

	bundle := policy.Bundle{
		Policies: map[string]policy.Policy{
			"git.commitlint": {
				ID:              "git.commitlint",
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Evaluators:      []policy.Evaluator{{Name: "git.commitlint"}},
				DefenseLayers:   policy.DefenseLayers{Enforce: "block"},
				Message:         "commit message invalid",
				PrincipleIDs:    []string{"one-path-for-critical-operations"},
			},
		},
		Dispatch: policy.Dispatch{
			Linter: map[string][]string{
				ScopeCommit: {"git.commitlint"},
			},
		},
	}

	result, err := Run(bundle, Options{
		Scope: ScopeCommit,
		Cwd:   repo,
		Files: []string{"COMMIT_EDITMSG"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status mismatch: got %q", result.Status)
	}
}

func TestRunBlocksSelfPromotionalCommitMessage(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	messagePath := filepath.Join(repo, "COMMIT_EDITMSG")

	err := os.WriteFile(
		messagePath,
		[]byte("fix(policy): Generated with Codex\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write commit message: %v", err)
	}

	result, err := Run(compiledRepoLintBundle(t), Options{
		Scope: ScopeCommit,
		Cwd:   repo,
		Files: []string{"COMMIT_EDITMSG"},
	})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Status != statusBlocked ||
		!lintResultHasDecision(result, "agent.self_promotion_commit_msg") {
		t.Fatalf("expected self-promotion commit block, got %#v", result)
	}
}

func lintResultHasDecision(result Result, policyID string) bool {
	for _, decision := range result.Decisions {
		if decision.PolicyID == policyID {
			return true
		}
	}

	return false
}

func compiledRepoLintBundle(tb testing.TB) policy.Bundle {
	tb.Helper()

	root := repoRootForLintTest(tb)

	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary: filepath.Join(root, "coding_ethos.yml"),
		Config:  filepath.Join(root, "config.yaml"),
	})
	if err != nil {
		tb.Fatalf("compile repo policy bundle: %v", err)
	}

	return bundle
}

func repoRootForLintTest(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		tb.Fatalf("resolve cwd: %v", err)
	}

	for {
		if fileExistsForLintTest(filepath.Join(dir, "coding_ethos.yml")) &&
			fileExistsForLintTest(filepath.Join(dir, "config.yaml")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatalf("repository root not found from %s", dir)
		}

		dir = parent
	}
}

func fileExistsForLintTest(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func TestRunRejectsPolicyWithNoRegisteredEvaluator(t *testing.T) {
	t.Parallel()

	bundle := policy.Bundle{
		Policies: map[string]policy.Policy{
			"python.missing_evaluator": {
				ID:              "python.missing_evaluator",
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Evaluators:      []policy.Evaluator{{Name: "python.missing_evaluator"}},
				DefenseLayers:   policy.DefenseLayers{Enforce: "block"},
				Message:         "missing evaluator",
				PrincipleIDs: []string{
					"static-analysis-is-the-first-line-of-defense",
				},
			},
		},
		Dispatch: policy.Dispatch{
			Linter: map[string][]string{
				ScopeFiles: {"python.missing_evaluator"},
			},
		},
	}

	_, err := Run(bundle, Options{Scope: ScopeFiles, Files: []string{"src/app.py"}})
	if err == nil {
		t.Fatal("expected error")
	}

	if !strings.Contains(err.Error(), "lint policy has no registered evaluator") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunExecutesSmokeExternalPolicies(t *testing.T) {
	t.Parallel()

	bundle := policy.Bundle{
		Policies: map[string]policy.Policy{
			"pytest.gate": {
				ID:              "pytest.gate",
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Evaluators: []policy.Evaluator{{
					Kind: "external",
					Name: "pytest.gate",
					Options: map[string]any{
						"command": []string{
							"sh",
							"-c",
							printRuffDiagnosticCommand + "; exit 9",
						},
						"parser": "ruff",
					},
				}},
				DefenseLayers: policy.DefenseLayers{Enforce: "block"},
				Message:       "pytest failed",
				PrincipleIDs:  []string{"testing-as-specification"},
			},
		},
		EvidenceMaps: []diagnostics.EvidenceMap{
			{
				Source:       "ruff",
				Codes:        []string{"F401"},
				PolicyID:     "python.direct_imports",
				PrincipleIDs: []string{"protocol-first-design"},
				Confidence:   "medium",
				Meaning:      "unused import evidence",
				Advice: diagnostics.EvidenceAdvice{
					Summary: "Remove the unused import or use the protocol.",
					Steps:   []string{"Remove unused imports."},
					Rerun:   []string{"make pre-commit"},
				},
			},
		},
		Dispatch: policy.Dispatch{
			Linter: map[string][]string{
				ScopeSmoke: {"pytest.gate"},
			},
		},
	}

	result, err := Run(bundle, Options{Scope: ScopeSmoke})
	if err != nil {
		t.Fatalf("run lint: %v", err)
	}

	if result.Status != statusBlocked {
		t.Fatalf("status = %q, want blocked", result.Status)
	}

	assertSmokeExternalPolicyEvidence(t, result)
	assertSmokeExternalPolicyDiagnostics(t, result)
	assertSmokeExternalPolicyFindings(t, result)
}

func assertSmokeExternalPolicyEvidence(t *testing.T, result Result) {
	t.Helper()

	if got := result.Decisions[0].Evidence["exit_code"]; got != 9 {
		t.Fatalf("exit evidence = %#v, want 9", got)
	}
}

func assertSmokeExternalPolicyDiagnostics(t *testing.T, result Result) {
	t.Helper()

	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "F401" {
		t.Fatalf("diagnostics = %#v, want parsed F401", result.Diagnostics)
	}

	if result.Diagnostics[0].PolicyID != "python.direct_imports" {
		t.Fatalf("diagnostic policy = %q", result.Diagnostics[0].PolicyID)
	}

	if result.Diagnostics[0].Advice != "Remove the unused import or use the protocol." {
		t.Fatalf("diagnostic advice = %q", result.Diagnostics[0].Advice)
	}
}

func assertSmokeExternalPolicyFindings(t *testing.T, result Result) {
	t.Helper()

	if len(result.Findings) != 1 {
		t.Fatalf("findings = %#v", result.Findings)
	}

	if result.Findings[0].CheckID != "python.direct_imports" ||
		result.Findings[0].SourceTool != "ruff" ||
		result.Findings[0].Advice != "Remove the unused import or use the protocol." ||
		!result.Findings[0].Blocking {
		t.Fatalf("normalized finding = %#v", result.Findings[0])
	}
}

const printRuffDiagnosticCommand = `printf '%s\n' ` +
	`'[{"filename":"pkg/app.py","code":"F401","message":"unused import",` +
	`"location":{"row":4,"column":8}}]'`
