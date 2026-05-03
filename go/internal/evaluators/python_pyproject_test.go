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

func TestEvaluatePythonPyprojectIgnoresBlocksForbiddenIgnores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeEvaluatorTestFile(t, filepath.Join(root, "pyproject.toml"), `
[tool.ruff.lint.per-file-ignores]
"src/**" = ["F401"]

[[tool.mypy.overrides]]
module = "internal_pkg.*"
disable_error_code = ["attr-defined"]
`)

	decisions, err := EvaluatePythonPyprojectIgnores(
		pyprojectIgnoresPolicy(),
		Context{
			Cwd:   root,
			Files: []string{"pyproject.toml"},
			EvaluatorOptions: map[string]any{
				"allowed_ignore_patterns": []string{"tests/**"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate pyproject ignores: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}

	diagnostics := decisions[0].Diagnostics
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostic count mismatch: %#v", diagnostics)
	}

	if diagnostics[0].Code != "mypy.override.disable_error_code" ||
		diagnostics[1].Code != "ruff.per-file-ignores" {
		t.Fatalf("unexpected diagnostics: %#v", diagnostics)
	}
}

func TestEvaluatePythonPyprojectIgnoresHonorsAllowedPatterns(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeEvaluatorTestFile(t, filepath.Join(root, "pyproject.toml"), `
[tool.ruff.lint.per-file-ignores]
"tests/**" = ["F401"]

[[tool.mypy.overrides]]
module = "external_pkg.*"
ignore_missing_imports = true
`)

	decisions, err := EvaluatePythonPyprojectIgnores(
		pyprojectIgnoresPolicy(),
		Context{
			Cwd:   root,
			Files: []string{"pyproject.toml"},
			EvaluatorOptions: map[string]any{
				"allowed_ignore_patterns":      []string{"tests/**"},
				"allowed_mypy_missing_imports": []string{"external_pkg.*"},
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate pyproject ignores: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func TestEvaluatePythonUVExcludeNewerBlocksMissingSetting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeEvaluatorTestFile(t, filepath.Join(root, "pyproject.toml"), `
[project]
name = "example"
`)

	decisions, err := EvaluatePythonUVExcludeNewer(
		uvExcludeNewerPolicy(),
		Context{
			Cwd:   root,
			Files: []string{"pyproject.toml"},
			EvaluatorOptions: map[string]any{
				"expected_value": "7 days",
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate uv exclude-newer: %v", err)
	}

	if len(decisions) != 1 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
	if decisions[0].Diagnostics[0].Code != "uv.exclude-newer" {
		t.Fatalf("unexpected diagnostic: %#v", decisions[0].Diagnostics)
	}
}

func TestEvaluatePythonUVExcludeNewerCollectsAllInvalidPyprojects(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeEvaluatorTestFile(t, filepath.Join(root, "pyproject.toml"), `
[project]
name = "example"
`)
	writeEvaluatorTestFile(t, filepath.Join(root, "subpkg", "pyproject.toml"), `
[tool.uv]
exclude-newer = "1 day"
`)

	decisions, err := EvaluatePythonUVExcludeNewer(
		uvExcludeNewerPolicy(),
		Context{
			Cwd: root,
			Files: []string{
				"pyproject.toml",
				filepath.Join("subpkg", "pyproject.toml"),
			},
			EvaluatorOptions: map[string]any{
				"expected_value": "7 days",
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate uv exclude-newer: %v", err)
	}

	if len(decisions) != 2 {
		t.Fatalf("decision count mismatch: %#v", decisions)
	}
	if decisions[0].Diagnostics[0].File != "pyproject.toml" ||
		decisions[1].Diagnostics[0].File != filepath.Join("subpkg", "pyproject.toml") {
		t.Fatalf("unexpected diagnostics: %#v", decisions)
	}
}

func TestEvaluatePythonUVExcludeNewerHonorsExpectedSetting(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeEvaluatorTestFile(t, filepath.Join(root, "pyproject.toml"), `
[tool.uv]
exclude-newer = "7 days"
`)

	decisions, err := EvaluatePythonUVExcludeNewer(
		uvExcludeNewerPolicy(),
		Context{
			Cwd:   root,
			Files: []string{"pyproject.toml"},
			EvaluatorOptions: map[string]any{
				"expected_value": "7 days",
			},
		},
	)
	if err != nil {
		t.Fatalf("evaluate uv exclude-newer: %v", err)
	}

	if len(decisions) != 0 {
		t.Fatalf("expected no decisions, got %#v", decisions)
	}
}

func pyprojectIgnoresPolicy() policy.Policy {
	return policy.Policy{
		ID:              "python.pyproject_ignores",
		DefaultSeverity: "block",
		Message:         "pyproject.toml contains forbidden linter ignore configuration.",
		Suggestion:      "Move file-specific ignores into the target files.",
	}
}

func uvExcludeNewerPolicy() policy.Policy {
	return policy.Policy{
		ID:              "python.uv_exclude_newer",
		DefaultSeverity: "block",
		Message:         "uv dependency resolution must exclude newly uploaded packages.",
		Suggestion:      "Set [tool.uv].exclude-newer to the configured review window.",
	}
}

func writeEvaluatorTestFile(t *testing.T, path string, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create test directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write test file %s: %v", path, err)
	}
}
