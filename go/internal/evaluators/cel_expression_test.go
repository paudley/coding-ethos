// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	. "blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	lineLimitAdvice = "Do not make cosmetic or documentation-only edits just to " +
		"satisfy the limit; apply SOLID refactoring and split the file into " +
		"focused modules before committing."
	lineLimitFile    = "app.py"
	lineLimitMessage = "Large source files must not keep growing."
	lineLimitPolicy  = "filesystem.line_limits"
)

func TestEvaluateCELExpressionBlocksMatchingCommand(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Command: "python -c 'import subprocess; subprocess.run([\"git\"] )'",
			Argv:    []string{"python", "-c", "import subprocess"},
			Scope:   "files",
			EvaluatorOptions: map[string]any{
				"skill_id": "safe-git-workflow",
				"when":     pythonSubprocessGitCEL(),
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 || decisions[0].PolicyID != "custom.no_subprocess_git" {
		t.Fatalf("decisions = %#v", decisions)
	}

	if decisions[0].Diagnostics[0].SkillID != "safe-git-workflow" {
		t.Fatalf("diagnostic = %#v", decisions[0].Diagnostics[0])
	}

	if decisions[0].Evidence["implementation"] != "cel" ||
		decisions[0].Evidence["input_schema_version"] != celexpr.SchemaVersion ||
		decisions[0].Diagnostics[0].Metadata["implementation"] != "cel" {
		t.Fatalf("missing CEL result metadata: %#v", decisions[0])
	}
}

func TestEvaluateCELExpressionUsesPolicyDefaultSeverity(t *testing.T) {
	t.Parallel()

	policyDef := celExpressionPolicy()
	policyDef.DefaultSeverity = recordDecision
	policyDef.SupportedModes = []string{blockDecision, recordDecision}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Command: "python -c 'import subprocess; subprocess.run([\"git\"] )'",
			EvaluatorOptions: map[string]any{
				"when": pythonSubprocessGitCEL(),
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one", decisions)
	}

	if decisions[0].Decision != recordDecision ||
		decisions[0].Severity != recordDecision {
		t.Fatalf("decision = %#v, want record severity", decisions[0])
	}

	if decisions[0].Diagnostics[0].Severity != recordDecision {
		t.Fatalf("diagnostic = %#v, want record severity", decisions[0].Diagnostics[0])
	}
}

func TestEvaluateCELExpressionIgnoresNonMatchingCommand(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Command: "python -m pytest",
			Scope:   "files",
			EvaluatorOptions: map[string]any{
				"when": pythonSubprocessGitCEL(),
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateCELExpressionBlocksMatchingPathScope(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Cwd:   "/repo",
			Files: []string{"src/tests/test_policy.py"},
			Scope: "files",
			Tool:  "ruff",
			EvaluatorOptions: map[string]any{
				"source_roots":   []string{"src"},
				"python_version": "3.13",
				"skill_id":       "lint-remediation",
				"when": `
					has_suffix(path.file, ".py") &&
					is_test_path(path.file) &&
					in_source_root(path.file, repo.source_roots) &&
					list_contains(files, path.file) &&
					metadata.tool == "ruff" &&
					repo.python_version == "3.13"
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one decision", decisions)
	}

	if decisions[0].Diagnostics[0].File != "src/tests/test_policy.py" {
		t.Fatalf("diagnostic = %#v", decisions[0].Diagnostics[0])
	}
}

func TestEvaluateCELExpressionUsesExplicitPathCollection(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Cwd:   "/repo",
			Files: []string{"src/app.py", "tests/test_policy.py"},
			Scope: "files",
			EvaluatorOptions: map[string]any{
				"source_roots": []string{"src"},
				"when": `
					path.file == "" &&
					paths.exists(item, item.file == "src/app.py" && item.in_source_root) &&
					paths.exists(item, item.file == "tests/test_policy.py" && item.is_test)
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one decision", decisions)
	}

	if decisions[0].Diagnostics[0].File != "" {
		t.Fatalf(
			"diagnostic = %#v, want no implicit first-file location",
			decisions[0].Diagnostics[0],
		)
	}
}

func TestEvaluateCELExpressionUsesExplicitDiagnosticInput(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Files: []string{"src/app.py"},
			Diagnostic: &diagnostics.Diagnostic{
				Tool:     "ruff",
				Code:     "F401",
				Message:  "unused import",
				File:     "src/app.py",
				Line:     9,
				Column:   2,
				Severity: "error",
				PolicyID: "python.direct_imports",
			},
			EvaluatorOptions: map[string]any{
				"when": `
					diagnostic.tool == "ruff" &&
					diagnostic.code == "F401" &&
					diagnostic.file == "src/app.py" &&
					diagnostic.line == 9 &&
					diagnostic.column == 2 &&
					diagnostic.severity == "error" &&
					diagnostic.policy_id == "python.direct_imports"
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one decision", decisions)
	}

	diagnostic := decisions[0].Diagnostics[0]
	if diagnostic.Tool != "ruff" ||
		diagnostic.Code != "F401" ||
		diagnostic.File != "src/app.py" ||
		diagnostic.Line != 9 ||
		diagnostic.Column != 2 {
		t.Fatalf("diagnostic location mismatch: %#v", diagnostic)
	}

	if diagnostic.Metadata["matched_diagnostic_policy_id"] != "python.direct_imports" {
		t.Fatalf("diagnostic metadata mismatch: %#v", diagnostic.Metadata)
	}
}

func TestEvaluateCELExpressionUsesExpandedFactInputs(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Argv:         []string{"git", "status"},
			ChangedFiles: []string{"repo_config.yaml"},
			Command:      "CODE_ETHOS_CONSUMER_ROOT=/repo git status",
			EventName:    "PreToolUse",
			Files: []string{
				"repo_config.yaml",
				"coding-ethos-hooks/bin/coding-ethos-policy",
			},
			Findings: []Finding{{
				Tool:     "ruff",
				Code:     "S101",
				File:     "tests/test_policy.py",
				Severity: "error",
			}},
			Provider:    "codex",
			StagedFiles: []string{"coding-ethos-hooks/bin/coding-ethos-policy"},
			Tool:        "Bash",
			EvaluatorOptions: map[string]any{
				"config_candidates":  []string{"repo_config.yaml"},
				"current_branch":     "main",
				"mode":               "block",
				"protected_branches": []string{"main"},
				"protected_paths": []string{
					"coding-ethos-hooks/bin/coding-ethos-policy",
				},
				"when": `
					event.name == "PreToolUse" &&
					event.provider == "codex" &&
					event.mode == "block" &&
					command_fact.has_inline_env &&
					command_invokes(command, "git") &&
					argv_invokes(argv, "git") &&
					diff.has_changes &&
					diff.changed_files.exists(file, file == "repo_config.yaml") &&
						diff.staged_files.exists(file,
							file == "coding-ethos-hooks/bin/coding-ethos-policy"
						) &&
					findings.exists(item, item.code == "S101") &&
					repo_config_present(files, config.candidates) &&
						is_protected_path(
							"coding-ethos-hooks/bin/coding-ethos-policy",
							repo.protected_paths
						) &&
					git.on_protected_branch
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one decision", decisions)
	}
}

func TestEvaluateCELExpressionBlocksLargeAddedFileFromFileChanges(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runCELGit(t, repo, "init")

	largeFile := filepath.Join(repo, "config.yaml")

	inlineErr0 := os.WriteFile(
		largeFile,
		[]byte("payload: "+strings.Repeat("x", 2048)),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write large file: %v", inlineErr0)
	}

	runCELGit(t, repo, "add", "config.yaml")

	policyDef := celExpressionPolicy()
	policyDef.ID = "filesystem.large_files"
	policyDef.Source = policy.SourceRef{
		File: "coding_ethos.yml",
		Path: "principles[security-by-design].policy.expressions[0]",
	}
	policyDef.Message = "Oversized newly added files are forbidden."
	policyDef.Suggestion = "Remove oversized generated or binary content from the commit."
	policyDef.PrincipleIDs = []string{"security-by-design"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:   repo,
			Files: []string{"config.yaml"},
			Scope: "staged",
			EvaluatorOptions: map[string]any{
				"when": `
					file_changes.exists(file,
						file.is_added &&
						file.ext == ".yaml" &&
						file.size_bytes > 1024
					)
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != "filesystem.large_files" ||
		decisions[0].Diagnostics[0].File != "config.yaml" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateCELExpressionBlocksLineLimitGrowthFromFileChanges(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runCELGit(t, repo, "init")
	runCELGit(t, repo, "config", "user.email", "test@example.com")
	runCELGit(t, repo, "config", "user.name", "Test User")

	sourceFile := filepath.Join(repo, lineLimitFile)

	inlineErr1 := os.WriteFile(sourceFile, []byte("one\n"), 0o600)
	if inlineErr1 != nil {
		t.Fatalf("write initial source: %v", inlineErr1)
	}

	runCELGit(t, repo, "add", lineLimitFile)
	runCELGit(t, repo, "commit", "-m", "initial")

	inlineErr2 := os.WriteFile(sourceFile, []byte("one\ntwo\nthree\n"), 0o600)
	if inlineErr2 != nil {
		t.Fatalf("rewrite source: %v", inlineErr2)
	}

	runCELGit(t, repo, "add", lineLimitFile)

	policyDef := celExpressionPolicy()
	policyDef.ID = lineLimitPolicy
	policyDef.Source = policy.SourceRef{
		File: "coding_ethos.yml",
		Path: "principles[solid-is-law].policy.expressions[0]",
	}
	policyDef.Message = lineLimitMessage
	policyDef.Suggestion = lineLimitAdvice
	policyDef.PrincipleIDs = []string{"solid-is-law"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:   repo,
			Files: []string{lineLimitFile},
			Scope: "staged",
			EvaluatorOptions: map[string]any{
				"when": `
					file_changes.exists(file,
						file.ext == ".py" &&
						file.line_count > 2 &&
						file.original_line_count >= 0 &&
						file.line_count > file.original_line_count
					)
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != lineLimitPolicy ||
		decisions[0].Diagnostics[0].File != lineLimitFile {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateRepoLineLimitAllowsUnderThresholdPythonFileGrowth(t *testing.T) {
	t.Parallel()

	decisions := evaluateRepoLineLimitAfterRewrite(
		t,
		pythonFileWithLines(510),
		func(initial string) string { return initial + "value_510 = 510\n" },
	)

	if len(decisions) != 0 {
		t.Fatalf("under-threshold Python file growth should be allowed: %#v", decisions)
	}
}

func TestEvaluateRepoLineLimitAllowsBlankOnlyGrowthInOverThresholdPythonFile(
	t *testing.T,
) {
	t.Parallel()

	decisions := evaluateRepoLineLimitAfterRewrite(
		t,
		pythonFileWithLines(1001),
		func(initial string) string { return initial + "\n" },
	)

	if len(decisions) != 0 {
		t.Fatalf("blank-only Python file growth should be allowed: %#v", decisions)
	}
}

func evaluateRepoLineLimitAfterRewrite(
	t *testing.T,
	initial string,
	rewrite func(initial string) string,
) []policy.Decision {
	t.Helper()

	repo := t.TempDir()
	runCELGit(t, repo, "init")
	runCELGit(t, repo, "config", "user.email", "test@example.com")
	runCELGit(t, repo, "config", "user.name", "Test User")
	sourceFile := filepath.Join(repo, lineLimitFile)

	inlineErr3 := os.WriteFile(sourceFile, []byte(initial), 0o600)
	if inlineErr3 != nil {
		t.Fatalf("write initial source: %v", inlineErr3)
	}

	runCELGit(t, repo, "add", lineLimitFile)
	runCELGit(t, repo, "commit", "-m", "initial")

	inlineErr4 := os.WriteFile(sourceFile, []byte(rewrite(initial)), 0o600)
	if inlineErr4 != nil {
		t.Fatalf("rewrite source: %v", inlineErr4)
	}

	runCELGit(t, repo, "add", lineLimitFile)

	policyDef := compiledRepoLineLimitPolicy(t)

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		Files:            []string{lineLimitFile},
		Scope:            "staged",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	return decisions
}

func TestEvaluateRepoLineLimitBlocksOverThresholdPythonFileGrowth(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runCELGit(t, repo, "init")
	runCELGit(t, repo, "config", "user.email", "test@example.com")
	runCELGit(t, repo, "config", "user.name", "Test User")
	sourceFile := filepath.Join(repo, lineLimitFile)

	initial := pythonFileWithLines(1001)

	inlineErr5 := os.WriteFile(sourceFile, []byte(initial), 0o600)
	if inlineErr5 != nil {
		t.Fatalf("write initial source: %v", inlineErr5)
	}

	runCELGit(t, repo, "add", lineLimitFile)
	runCELGit(t, repo, "commit", "-m", "initial")

	inlineErr6 := os.WriteFile(
		sourceFile,
		[]byte(initial+"value_1001 = 1001\n"),
		0o600,
	)
	if inlineErr6 != nil {
		t.Fatalf("rewrite source: %v", inlineErr6)
	}

	runCELGit(t, repo, "add", lineLimitFile)

	policyDef := compiledRepoLineLimitPolicy(t)

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		Files:            []string{lineLimitFile},
		Scope:            "staged",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != lineLimitPolicy ||
		decisions[0].Diagnostics[0].File != lineLimitFile {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateRepoLineLimitDiagnosticNamesOffendingFileInMultiFileRun(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runCELGit(t, repo, "init")
	runCELGit(t, repo, "config", "user.email", "test@example.com")
	runCELGit(t, repo, "config", "user.name", "Test User")
	smallFile := filepath.Join(repo, "small.py")
	largeFile := filepath.Join(repo, "large.py")

	inlineErr7 := os.WriteFile(smallFile, []byte("value = 1\n"), 0o600)
	if inlineErr7 != nil {
		t.Fatalf("write small source: %v", inlineErr7)
	}

	initialLarge := pythonFileWithLines(1001)

	inlineErr8 := os.WriteFile(largeFile, []byte(initialLarge), 0o600)
	if inlineErr8 != nil {
		t.Fatalf("write large source: %v", inlineErr8)
	}

	runCELGit(t, repo, "add", "small.py", "large.py")
	runCELGit(t, repo, "commit", "-m", "initial")

	inlineErr9 := os.WriteFile(smallFile, []byte("value = 2\n"), 0o600)
	if inlineErr9 != nil {
		t.Fatalf("rewrite small source: %v", inlineErr9)
	}

	inlineErr10 := os.WriteFile(
		largeFile,
		[]byte(initialLarge+"value_1001 = 1001\n"),
		0o600,
	)
	if inlineErr10 != nil {
		t.Fatalf("rewrite large source: %v", inlineErr10)
	}

	runCELGit(t, repo, "add", "small.py", "large.py")

	policyDef := compiledRepoLineLimitPolicy(t)

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		Files:            []string{"small.py", "large.py"},
		Scope:            "staged",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != lineLimitPolicy ||
		decisions[0].Diagnostics[0].File != "large.py" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateRepoLineLimitDoesNotReportGrowingMarkdownDoc(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runCELGit(t, repo, "init")
	runCELGit(t, repo, "config", "user.email", "test@example.com")
	runCELGit(t, repo, "config", "user.name", "Test User")

	docsDir := filepath.Join(repo, "docs")

	inlineErr11 := os.MkdirAll(docsDir, 0o700)
	if inlineErr11 != nil {
		t.Fatalf("create docs dir: %v", inlineErr11)
	}

	docFile := filepath.Join(docsDir, "SOURCE_DOCS.md")
	largeFile := filepath.Join(repo, "large.py")
	initialLarge := pythonFileWithLines(1001)

	inlineErr12 := os.WriteFile(docFile, []byte("# Source Docs\n"), 0o600)
	if inlineErr12 != nil {
		t.Fatalf("write docs source: %v", inlineErr12)
	}

	inlineErr13 := os.WriteFile(largeFile, []byte(initialLarge), 0o600)
	if inlineErr13 != nil {
		t.Fatalf("write large source: %v", inlineErr13)
	}

	runCELGit(t, repo, "add", "docs/SOURCE_DOCS.md", "large.py")
	runCELGit(t, repo, "commit", "-m", "initial")

	inlineErr14 := os.WriteFile(
		docFile,
		[]byte("# Source Docs\n\n- Added docs entry.\n"),
		0o600,
	)
	if inlineErr14 != nil {
		t.Fatalf("rewrite docs source: %v", inlineErr14)
	}

	inlineErr15 := os.WriteFile(
		largeFile,
		[]byte(initialLarge+"value_1001 = 1001\n"),
		0o600,
	)
	if inlineErr15 != nil {
		t.Fatalf("rewrite large source: %v", inlineErr15)
	}

	runCELGit(t, repo, "add", "docs/SOURCE_DOCS.md", "large.py")

	policyDef := compiledRepoLineLimitPolicy(t)

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		Files:            []string{"docs/SOURCE_DOCS.md", "large.py"},
		Scope:            "staged",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != lineLimitPolicy ||
		decisions[0].Diagnostics[0].File != "large.py" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateRepoLineLimitDoesNotApplyToSQLFiles(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	runCELGit(t, repo, "init")
	runCELGit(t, repo, "config", "user.email", "test@example.com")
	runCELGit(t, repo, "config", "user.name", "Test User")
	sourceFile := filepath.Join(repo, "query.sql")

	initial := strings.Repeat("SELECT 1;\n", 1200)

	inlineErr16 := os.WriteFile(sourceFile, []byte(initial), 0o600)
	if inlineErr16 != nil {
		t.Fatalf("write initial source: %v", inlineErr16)
	}

	runCELGit(t, repo, "add", "query.sql")
	runCELGit(t, repo, "commit", "-m", "initial")

	inlineErr17 := os.WriteFile(sourceFile, []byte(initial+"SELECT 2;\n"), 0o600)
	if inlineErr17 != nil {
		t.Fatalf("rewrite source: %v", inlineErr17)
	}

	runCELGit(t, repo, "add", "query.sql")

	policyDef := compiledRepoLineLimitPolicy(t)

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Cwd:              repo,
		Files:            []string{"query.sql"},
		Scope:            "staged",
		EvaluatorOptions: policyDef.Evaluators[0].Options,
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf(
			"SQL files should not be subject to filesystem.line_limits: %#v",
			decisions,
		)
	}
}

func TestEvaluateCELExpressionAllowsShrinkingLargeFileAtHookTime(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	sourceFile := filepath.Join(repo, lineLimitFile)

	inlineErr13 := os.WriteFile(
		sourceFile,
		[]byte(strings.Repeat("line\n", 4)),
		0o600,
	)
	if inlineErr13 != nil {
		t.Fatalf("write source: %v", inlineErr13)
	}

	policyDef := celExpressionPolicy()
	policyDef.ID = lineLimitPolicy
	policyDef.Message = lineLimitMessage
	policyDef.Suggestion = lineLimitAdvice
	policyDef.PrincipleIDs = []string{"solid-is-law"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:        repo,
			Files:      []string{lineLimitFile},
			Tool:       "Edit",
			OldContent: "line\nline\n",
			Content:    "line\n",
			Scope:      "PreToolUse",
			EvaluatorOptions: map[string]any{
				"when": `
					proposed_file_changes.exists(file,
						file.ext == ".py" &&
						file.proposed_line_count > 2 &&
						file.line_count_grows
					)
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("shrinking edit should be allowed: %#v", decisions)
	}
}

func TestEvaluateCELExpressionBlocksGrowingLargeFileAtHookTime(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	sourceFile := filepath.Join(repo, lineLimitFile)

	inlineErr14 := os.WriteFile(
		sourceFile,
		[]byte("one\ntwo\nthree\n"),
		0o600,
	)
	if inlineErr14 != nil {
		t.Fatalf("write source: %v", inlineErr14)
	}

	policyDef := celExpressionPolicy()
	policyDef.ID = lineLimitPolicy
	policyDef.Message = lineLimitMessage
	policyDef.Suggestion = lineLimitAdvice
	policyDef.PrincipleIDs = []string{"solid-is-law"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:        repo,
			Files:      []string{lineLimitFile},
			Tool:       "Edit",
			OldContent: "two\n",
			Content:    "two\nextra\n",
			Scope:      "PreToolUse",
			EvaluatorOptions: map[string]any{
				"when": `
					proposed_file_changes.exists(file,
						file.ext == ".py" &&
						file.proposed_line_count > 2 &&
						file.line_count_grows
					)
				`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != lineLimitPolicy ||
		decisions[0].Diagnostics[0].File != lineLimitFile {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateCELExpressionBlocksGrowingLargeApplyPatchAtHookTime(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()

	sourceFile := filepath.Join(
		repo,
		"go",
		"cmd",
		"coding-ethos-hook-runner",
		"main.go",
	)

	inlineErr15 := os.MkdirAll(filepath.Dir(sourceFile), 0o700)
	if inlineErr15 != nil {
		t.Fatalf("create source dir: %v", inlineErr15)
	}

	initial := strings.Repeat("line\n", 2001)

	inlineErr16 := os.WriteFile(sourceFile, []byte(initial), 0o600)
	if inlineErr16 != nil {
		t.Fatalf("write source: %v", inlineErr16)
	}

	policyDef := compiledRepoLineLimitPolicy(t)

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:   repo,
			Tool:  "Edit",
			Scope: "PreToolUse",
			Command: `*** Begin Patch
*** Update File: go/cmd/coding-ethos-hook-runner/main.go
@@
+newLine
*** End Patch
`,
			EvaluatorOptions: policyDef.Evaluators[0].Options,
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 1 ||
		decisions[0].PolicyID != lineLimitPolicy ||
		decisions[0].Diagnostics[0].File != "go/cmd/coding-ethos-hook-runner/main.go" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateCELExpressionAllowsManagedToolCapabilityContract(t *testing.T) {
	t.Parallel()

	policyDef := celExpressionPolicy()
	policyDef.ID = "runtime.managed_tool_capability_contract"

	decisions, err := EvaluateCELExpression(policyDef, Context{
		Scope: "files",
		EvaluatorOptions: map[string]any{
			"when": `
				tool_capabilities.exists(tool,
					tool.name != "gemini-check" &&
					(
						tool.requires_network ||
						tool.requires_git ||
						!list_contains(tool.tags, "no-network") ||
						!list_contains(tool.tags, "no-git") ||
						tool.sandbox_profile == "" ||
						tool.timeout_seconds <= 0 ||
						tool.memory_mb <= 0 ||
						tool.cpu_quota_percent <= 0 ||
						tool.seccomp_profile == ""
					)
				)
			`,
		},
	})
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want none", decisions)
	}
}

func TestEvaluateCELExpressionDoesNotFakeDiagnosticInput(t *testing.T) {
	t.Parallel()

	decisions, err := EvaluateCELExpression(
		celExpressionPolicy(),
		Context{
			Files: []string{"src/app.py"},
			Tool:  "ruff",
			EvaluatorOptions: map[string]any{
				"when": `diagnostic.tool == "ruff" || diagnostic.file == "src/app.py"`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("decisions = %#v, want no placeholder diagnostic match", decisions)
	}
}

func runCELGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	gitPath, err := realgit.Resolve("git")
	if err != nil {
		t.Fatalf("resolve git: %v", err)
	}

	cmd := exec.CommandContext(context.Background(), gitPath, args...)
	cmd.Dir = dir

	cmd.Env = append(
		os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_NOSYSTEM=1",
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func celExpressionPolicy() policy.Policy {
	return policy.Policy{
		ID:       "custom.no_subprocess_git",
		Category: "expression",
		Source: policy.SourceRef{
			File: "config.yaml",
			Path: "policy.expressions",
		},
		DefaultSeverity: "block",
		SupportedModes:  []string{blockDecision, recordDecision},
		Message:         "Git subprocesses are forbidden.",
		Suggestion:      "Use the protected Git wrapper.",
		DefenseLayers:   policy.CodeDefenseLayers(),
		Evaluators: []policy.Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
		}},
		PrincipleIDs: []string{"one-path-for-critical-operations"},
	}
}

func compiledRepoLineLimitPolicy(tb testing.TB) policy.Policy {
	tb.Helper()

	root := repoRootForCELTest(tb)

	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary: filepath.Join(root, "coding_ethos.yml"),
		Config:  filepath.Join(root, "config.yaml"),
	})
	if err != nil {
		tb.Fatalf("compile repo policy bundle: %v", err)
	}

	return bundle.Policies[lineLimitPolicy]
}

func repoRootForCELTest(tb testing.TB) string {
	tb.Helper()

	dir, err := filepath.Abs(".")
	if err != nil {
		tb.Fatalf("resolve cwd: %v", err)
	}

	for {
		if fileExistsForCELTest(filepath.Join(dir, "coding_ethos.yml")) &&
			fileExistsForCELTest(filepath.Join(dir, "config.yaml")) {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			tb.Fatalf("repository root not found from %s", dir)
		}

		dir = parent
	}
}

func fileExistsForCELTest(path string) bool {
	_, err := os.Stat(path)

	return err == nil
}

func pythonSubprocessGitCEL() string {
	return `shell_commands.exists(cmd,
		cmd.name in ["python", "python3"] &&
		cmd.argv.exists(arg, arg.contains("subprocess")) &&
		cmd.argv.exists(arg, arg.contains("git"))
	)`
}

func pythonFileWithLines(lines int) string {
	var builder strings.Builder
	for index := range lines {
		builder.WriteString("value_")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString(" = ")
		builder.WriteString(strconv.Itoa(index))
		builder.WriteString("\n")
	}

	return builder.String()
}
