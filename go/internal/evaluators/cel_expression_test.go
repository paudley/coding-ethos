// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
	"blackcat.ca/coding-ethos/go/internal/policy"
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
				"when":     `shell_commands.exists(cmd, cmd.name in ["python", "python3"] && cmd.argv.exists(arg, arg.contains("subprocess")) && cmd.argv.exists(arg, arg.contains("git")))`,
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
	policyDef.DefaultSeverity = "record"
	policyDef.SupportedModes = []string{"block", "record"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Command: "python -c 'import subprocess; subprocess.run([\"git\"] )'",
			EvaluatorOptions: map[string]any{
				"when": `shell_commands.exists(cmd, cmd.name in ["python", "python3"] && cmd.argv.exists(arg, arg.contains("subprocess")) && cmd.argv.exists(arg, arg.contains("git")))`,
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate CEL expression: %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %#v, want one", decisions)
	}
	if decisions[0].Decision != "record" || decisions[0].Severity != "record" {
		t.Fatalf("decision = %#v, want record severity", decisions[0])
	}
	if decisions[0].Diagnostics[0].Severity != "record" {
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
				"when": `shell_commands.exists(cmd, cmd.name in ["python", "python3"] && cmd.argv.exists(arg, arg.contains("subprocess")) && cmd.argv.exists(arg, arg.contains("git")))`,
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
		t.Fatalf("diagnostic = %#v, want no implicit first-file location", decisions[0].Diagnostics[0])
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
				"protected_paths":    []string{"coding-ethos-hooks/bin/coding-ethos-policy"},
				"when": `
					event.name == "PreToolUse" &&
					event.provider == "codex" &&
					event.mode == "block" &&
					command_fact.has_inline_env &&
					command_invokes(command, "git") &&
					argv_invokes(argv, "git") &&
					diff.has_changes &&
					diff.changed_files.exists(file, file == "repo_config.yaml") &&
					diff.staged_files.exists(file, file == "coding-ethos-hooks/bin/coding-ethos-policy") &&
					findings.exists(item, item.code == "S101") &&
					repo_config_present(files, config.candidates) &&
					is_protected_path("coding-ethos-hooks/bin/coding-ethos-policy", repo.protected_paths) &&
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
	if err := os.WriteFile(largeFile, []byte("payload: "+strings.Repeat("x", 2048)), 0o600); err != nil {
		t.Fatalf("write large file: %v", err)
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
	sourceFile := filepath.Join(repo, "app.py")
	if err := os.WriteFile(sourceFile, []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write initial source: %v", err)
	}
	runCELGit(t, repo, "add", "app.py")
	runCELGit(t, repo, "commit", "-m", "initial")
	if err := os.WriteFile(sourceFile, []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	runCELGit(t, repo, "add", "app.py")

	policyDef := celExpressionPolicy()
	policyDef.ID = "filesystem.line_limits"
	policyDef.Source = policy.SourceRef{
		File: "coding_ethos.yml",
		Path: "principles[solid-is-law].policy.expressions[0]",
	}
	policyDef.Message = "Large source files must not keep growing."
	policyDef.Suggestion = "Split large files into focused modules before committing."
	policyDef.PrincipleIDs = []string{"solid-is-law"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:   repo,
			Files: []string{"app.py"},
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
		decisions[0].PolicyID != "filesystem.line_limits" ||
		decisions[0].Diagnostics[0].File != "app.py" {
		t.Fatalf("decisions = %#v", decisions)
	}
}

func TestEvaluateCELExpressionAllowsShrinkingLargeFileAtHookTime(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	sourceFile := filepath.Join(repo, "app.py")
	if err := os.WriteFile(
		sourceFile,
		[]byte(strings.Repeat("line\n", 4)),
		0o600,
	); err != nil {
		t.Fatalf("write source: %v", err)
	}

	policyDef := celExpressionPolicy()
	policyDef.ID = "filesystem.line_limits"
	policyDef.Message = "Large source files must not keep growing."
	policyDef.Suggestion = "Split large files into focused modules before committing."
	policyDef.PrincipleIDs = []string{"solid-is-law"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:        repo,
			Files:      []string{"app.py"},
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
	sourceFile := filepath.Join(repo, "app.py")
	if err := os.WriteFile(
		sourceFile,
		[]byte("one\ntwo\nthree\n"),
		0o600,
	); err != nil {
		t.Fatalf("write source: %v", err)
	}

	policyDef := celExpressionPolicy()
	policyDef.ID = "filesystem.line_limits"
	policyDef.Message = "Large source files must not keep growing."
	policyDef.Suggestion = "Split large files into focused modules before committing."
	policyDef.PrincipleIDs = []string{"solid-is-law"}

	decisions, err := EvaluateCELExpression(
		policyDef,
		Context{
			Cwd:        repo,
			Files:      []string{"app.py"},
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
		decisions[0].PolicyID != "filesystem.line_limits" ||
		decisions[0].Diagnostics[0].File != "app.py" {
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

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}

func celExpressionPolicy() policy.Policy {
	return policy.Policy{
		ID:              "custom.no_subprocess_git",
		Category:        "expression",
		Source:          policy.SourceRef{File: "config.yaml", Path: "policy.expressions"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
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
