// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import "testing"

func TestModuleDocsHonorsCodeIntelSubtreeExcludes(t *testing.T) {
	t.Parallel()

	rootConfig := map[string]any{
		"code_intel": map[string]any{
			"exclude_paths": []any{
				"coding-ethos/**",
				".coding-ethos/**",
				"data/**",
				"pkg/generated/*.py",
			},
		},
	}
	excluded := appendCodeIntelModuleDocsExcludedDirs([]string{".venv"}, rootConfig)
	settings := moduleDocsSettings{
		CheckFilenames: []string{"__init__.py", "conftest.py"},
		ExcludedDirs:   excluded,
	}

	for _, path := range []string{
		"coding-ethos/coding_ethos/__init__.py",
		".coding-ethos/cache/pkg/conftest.py",
		"data/generated/__init__.py",
	} {
		if shouldCheckModuleDocsFile(path, settings) {
			t.Fatalf("shouldCheckModuleDocsFile(%q) = true, want false", path)
		}
	}

	if !shouldCheckModuleDocsFile("pkg/generated/__init__.py", settings) {
		t.Fatal("shouldCheckModuleDocsFile(pkg/generated/__init__.py) = false, want true")
	}
}
