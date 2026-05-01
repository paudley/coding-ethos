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

	for _, source := range []string{
		`env.HOME != ""`,
		`path.fiel == "pkg/app.py"`,
		`diagnostic.line.contains("1")`,
	} {
		err := Validate("test.unknown_input", source)
		if err == nil {
			t.Fatalf("validate CEL expression %q succeeded, want compile error", source)
		}
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

	pathInput, ok := activation["path"].(PathInput)
	if !ok {
		t.Fatalf("path input = %#v", activation["path"])
	}
	if pathInput.File != "src/tests/test_policy.py" ||
		pathInput.Dir != "src/tests" ||
		pathInput.Base != "test_policy.py" ||
		pathInput.Ext != ".py" ||
		!pathInput.IsTest ||
		!pathInput.InSourceRoot {
		t.Fatalf("path input = %#v", pathInput)
	}

	diagnostic, ok := activation["diagnostic"].(DiagnosticInput)
	if !ok || diagnostic.Tool != "ruff" || diagnostic.File != pathInput.File {
		t.Fatalf("diagnostic input = %#v", activation["diagnostic"])
	}

	repo, ok := activation["repo"].(RepoInput)
	if !ok || repo.Root != "/repo" || repo.PythonVersion != "3.13" {
		t.Fatalf("repo input = %#v", activation["repo"])
	}
}
