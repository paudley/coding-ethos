// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import "testing"

func TestValidateAcceptsPathDiagnosticFindingAndRepoInputs(t *testing.T) {
	t.Parallel()

	source := `
		path.ext == ".py" &&
		path.is_test &&
		diagnostic.tool == "ruff" &&
		finding.file.endsWith("test_policy.py") &&
		repo.python_version == "3.13"
	`
	if err := Validate("test.path_scope", source); err != nil {
		t.Fatalf("validate CEL expression: %v", err)
	}
}

func TestValidateRejectsUnknownInputs(t *testing.T) {
	t.Parallel()

	err := Validate("test.unknown_input", `env.HOME != ""`)
	if err == nil {
		t.Fatalf("validate CEL expression succeeded, want unknown variable error")
	}
}

func TestValidateAcceptsReviewedHelperFunctions(t *testing.T) {
	t.Parallel()

	source := `
		has_prefix(path.file, "src/") &&
		has_suffix(path.file, ".py") &&
		glob_match("src/**/*.py", path.file) &&
		is_test_path(path.file) &&
		!is_generated_path(path.file) &&
		in_source_root(path.file, repo.source_roots) &&
		list_contains(files, path.file) &&
		any_has_prefix(files, "src/") &&
		any_has_suffix(files, ".py")
	`
	if err := Validate("test.helpers", source); err != nil {
		t.Fatalf("validate CEL expression: %v", err)
	}
}

func TestActivationBuildsStablePathAndRepoInputs(t *testing.T) {
	t.Parallel()

	activation := Activation(ActivationInput{
		Cwd:           "/repo",
		Files:         []string{"./src/tests/test_policy.py"},
		Tool:          "ruff",
		SourceRoots:   []string{"src"},
		PythonVersion: "3.13",
	})

	pathInput, ok := activation["path"].(map[string]any)
	if !ok {
		t.Fatalf("path input = %#v", activation["path"])
	}
	for key, want := range map[string]any{
		"file":           "src/tests/test_policy.py",
		"dir":            "src/tests",
		"base":           "test_policy.py",
		"ext":            ".py",
		"is_test":        true,
		"in_source_root": true,
	} {
		if got := pathInput[key]; got != want {
			t.Fatalf("path[%s] = %#v, want %#v", key, got, want)
		}
	}

	diagnostic, ok := activation["diagnostic"].(map[string]any)
	if !ok || diagnostic["tool"] != "ruff" || diagnostic["file"] != pathInput["file"] {
		t.Fatalf("diagnostic input = %#v", activation["diagnostic"])
	}

	repo, ok := activation["repo"].(map[string]any)
	if !ok || repo["root"] != "/repo" || repo["python_version"] != "3.13" {
		t.Fatalf("repo input = %#v", activation["repo"])
	}
}
