// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLimitsForFilePreservesPythonLimitsUnderScripts(t *testing.T) {
	cfg := Config{}
	cfg.LineLimits.PythonHard = 1000
	cfg.LineLimits.PythonWarn = 800
	cfg.LineLimits.ShellHard = 500
	cfg.LineLimits.ShellWarn = 400

	tests := []struct {
		path     string
		hardWant int
		warnWant int
	}{
		{
			path:     "scripts/tool.py",
			hardWant: 1000,
			warnWant: 800,
		},
		{
			path:     "scripts/tool.sh",
			hardWant: 500,
			warnWant: 400,
		},
		{
			path:     "coding_ethos/module.py",
			hardWant: 1000,
			warnWant: 800,
		},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			hardGot, warnGot := limitsForFile(cfg, tc.path)
			if hardGot != tc.hardWant || warnGot != tc.warnWant {
				t.Fatalf(
					"limitsForFile(%q) = (%d, %d), want (%d, %d)",
					tc.path,
					hardGot,
					warnGot,
					tc.hardWant,
					tc.warnWant,
				)
			}
		})
	}
}

func TestLoadGeminiPromptPackParsesGeneratedContract(t *testing.T) {
	bundleRoot := filepath.Clean(filepath.Join("..", ".."))
	pack, err := loadGeminiPromptPack(bundleRoot)
	if err != nil {
		t.Fatalf("loadGeminiPromptPack(%q) returned error: %v", bundleRoot, err)
	}
	codeEthos, ok := pack.Checks["code_ethos"]
	if !ok {
		t.Fatalf("prompt pack missing code_ethos check: %#v", pack.Checks)
	}
	if codeEthos.FileScope != "code" {
		t.Fatalf("code_ethos file scope = %q, want %q", codeEthos.FileScope, "code")
	}
	if codeEthos.BatchSize != 3 {
		t.Fatalf("code_ethos batch size = %d, want %d", codeEthos.BatchSize, 3)
	}
	if codeEthos.MaxFileSizeKB != 50 {
		t.Fatalf(
			"code_ethos max_file_size_kb = %d, want %d",
			codeEthos.MaxFileSizeKB,
			50,
		)
	}
	if len(codeEthos.Selector.IncludeExtensions) == 0 {
		t.Fatal("code_ethos selector has no include_extensions")
	}
	if codeEthos.Selector.IncludeExtensions[0] != extPy {
		t.Fatalf(
			"code_ethos first include extension = %q, want %q",
			codeEthos.Selector.IncludeExtensions[0],
			extPy,
		)
	}
	if codeEthos.Selector.AllowExtensionlessInScripts {
		t.Fatal("code_ethos selector unexpectedly allows extensionless scripts")
	}
	if pack.Prompts["code_ethos"] == "" {
		t.Fatal("prompt pack has empty code_ethos prompt")
	}
}

func TestParseGeminiCLIOptions(t *testing.T) {
	options, err := parseGeminiCLIOptions(
		[]string{
			"--dry-run",
			"--full-check",
			"--check-type",
			"code_ethos",
			"one.py",
			"two.sh",
		},
	)
	if err != nil {
		t.Fatalf("parseGeminiCLIOptions() returned error: %v", err)
	}
	if !options.DryRun {
		t.Fatal("parseGeminiCLIOptions() did not enable dry-run")
	}
	if !options.FullCheck {
		t.Fatal("parseGeminiCLIOptions() did not enable full-check")
	}
	if options.CheckType != "code_ethos" {
		t.Fatalf("CheckType = %q, want %q", options.CheckType, "code_ethos")
	}
	if !reflect.DeepEqual(options.Files, []string{"one.py", "two.sh"}) {
		t.Fatalf("Files = %#v, want %#v", options.Files, []string{"one.py", "two.sh"})
	}
}

func TestParseGeminiCLIOptionsRejectsUnknownFlag(t *testing.T) {
	_, err := parseGeminiCLIOptions([]string{"--nope"})
	if err == nil {
		t.Fatal("parseGeminiCLIOptions() unexpectedly accepted unknown flag")
	}
}

func TestRootConfigValue(t *testing.T) {
	root := map[string]any{
		"go": map[string]any{
			"worktree": "lib/go",
		},
		"python": map[string]any{
			"manifest_validation": map[string]any{
				"enabled": true,
			},
		},
	}

	value, ok := rootConfigValue(root, "go.worktree")
	if !ok {
		t.Fatal("rootConfigValue() did not find go.worktree")
	}
	if value != "lib/go" {
		t.Fatalf("value = %#v, want %q", value, "lib/go")
	}

	_, ok = rootConfigValue(root, "python.missing")
	if ok {
		t.Fatal("rootConfigValue() unexpectedly found python.missing")
	}
}

func TestCheckForbiddenStringsExemptsBundleConfig(t *testing.T) {
	tempDir := t.TempDir()
	bundleRoot := filepath.Join(tempDir, "pre-commit")
	err := os.MkdirAll(filepath.Join(bundleRoot, "hooks"), 0o755)
	if err != nil {
		t.Fatalf("os.MkdirAll(%q) failed: %v", bundleRoot, err)
	}
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "lefthook.yml"),
		"min_version: 1.13.6\n",
	)
	configPath := filepath.Join(tempDir, "config.yaml")
	mustWriteTestFile(t, configPath, "text:\n  forbidden_strings:\n    - PLC0415\n")
	otherPath := filepath.Join(tempDir, "other.txt")
	mustWriteTestFile(t, otherPath, "PLC0415\n")

	t.Setenv(precommitRootEnv, bundleRoot)

	cfg := Config{}
	cfg.Text.ForbiddenStrings = []string{"PLC0415"}

	if got := checkForbiddenStrings(
		cfg,
		[]string{configPath},
	); got != 0 {
		t.Fatalf("checkForbiddenStrings(bundle config) = %d, want 0", got)
	}

	stderr := captureStderr(t, func() {
		if got := checkForbiddenStrings(
			cfg,
			[]string{otherPath},
		); got != 1 {
			t.Fatalf("checkForbiddenStrings(non-config) = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, `contains forbidden string "PLC0415"`) {
		t.Fatalf("unexpected stderr: %q", stderr)
	}
}

func TestValidateManifestData(t *testing.T) {
	settings := manifestValidationSettings{
		RequiredStringFields: []string{"version"},
		RequiredListSections: map[string]manifestValidationListSpec{
			"symlinks": {
				Required:             true,
				RequiredStringFields: []string{"source", "target"},
			},
			"repositories": {
				Required:             false,
				RequiredStringFields: []string{"name", "url"},
				OptionalStringFields: []string{"branch"},
			},
		},
	}

	valid := map[string]any{
		"version": "1",
		"symlinks": []any{
			map[string]any{"source": "a", "target": "b"},
		},
		"repositories": []any{
			map[string]any{
				"name":   "repo",
				"url":    "https://example.com",
				"branch": "main",
			},
		},
	}
	if errors := validateManifestData(valid, settings); len(errors) != 0 {
		t.Fatalf("validateManifestData(valid) = %#v, want no errors", errors)
	}

	invalid := map[string]any{
		"version": 1,
		"symlinks": []any{
			map[string]any{"source": "a"},
		},
		"repositories": []any{
			map[string]any{"name": "repo", "url": 123},
		},
	}
	errors := validateManifestData(invalid, settings)
	if len(errors) == 0 {
		t.Fatal("validateManifestData(invalid) returned no errors")
	}
}

func TestFindPlanMetadataFiles(t *testing.T) {
	settings := planCompletionSettings{
		MetadataFilename: "metadata.yaml",
		RootMarkers:      []string{"docs/plans/"},
	}
	files := []string{
		"docs/plans/feature-a/metadata.yaml",
		"docs/plans/feature-a/notes.md",
		"other/metadata.yaml",
	}

	matches := findPlanMetadataFiles(files, settings)
	if !reflect.DeepEqual(matches, []string{"docs/plans/feature-a/metadata.yaml"}) {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestCheckPlanCompletionErrors(t *testing.T) {
	tempDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		chdirErr := os.Chdir(previous)
		if chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	mustWriteTestFile(t, "docs/plans/feature-a/metadata.yaml", "status: review\n")
	mustWriteTestFile(
		t,
		"docs/plans/feature-a/tasks.md",
		"- [ ] unfinished\n- [x] done\n",
	)

	errors, err := checkPlanCompletionErrors(
		"docs/plans/feature-a/metadata.yaml",
		planCompletionSettings{
			CompletedStatusValues: []string{"review", "complete"},
		},
	)
	if err != nil {
		t.Fatalf("checkPlanCompletionErrors() returned error: %v", err)
	}
	if len(errors) == 0 {
		t.Fatal("checkPlanCompletionErrors() returned no errors")
	}
	if !strings.Contains(strings.Join(errors, "\n"), "PLAN COMPLETION FRAUD DETECTED") {
		t.Fatalf("unexpected error output: %#v", errors)
	}

	mustWriteTestFile(t, "docs/plans/feature-b/metadata.yaml", "status: in_progress\n")
	mustWriteTestFile(t, "docs/plans/feature-b/tasks.md", "- [ ] unfinished\n")
	errors, err = checkPlanCompletionErrors(
		"docs/plans/feature-b/metadata.yaml",
		planCompletionSettings{
			CompletedStatusValues: []string{"review", "complete"},
		},
	)
	if err != nil {
		t.Fatalf("checkPlanCompletionErrors() returned error: %v", err)
	}
	if len(errors) != 0 {
		t.Fatalf("checkPlanCompletionErrors(in_progress) = %#v, want no errors", errors)
	}
}

func TestExtractAndFilterPyprojectFindings(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "pyproject.toml")
	mustWriteTestFile(
		t,
		path,
		strings.TrimSpace(`
[tool.ruff]
exclude = [".venv", "generated"]

[tool.ruff.lint.per-file-ignores]
"tests/**" = ["S101"]
"pkg/**" = ["F401"]

[tool.mypy]
exclude = ["build"]

[[tool.mypy.overrides]]
module = ["external_pkg.*"]
ignore_missing_imports = true

[[tool.mypy.overrides]]
module = ["internal_pkg.*"]
ignore_missing_imports = true
disable_error_code = ["attr-defined"]

[tool.pyright]
ignore = ["vendor/**"]

[tool.pylint.main]
ignore-paths = ["generated"]
`)+"\n",
	)

	config, err := loadPyprojectConfig(path)
	if err != nil {
		t.Fatalf("loadPyprojectConfig() returned error: %v", err)
	}
	findings := filterAllowedPyprojectFindings(
		extractPyprojectFindings(config),
		pyprojectIgnoreSettings{
			AllowedIgnorePatterns:    []string{"tests/**"},
			AllowedExcludePatterns:   []string{".venv", "build"},
			AllowedMypyMissingImport: []string{"external_pkg.*"},
		},
	)

	rendered := make([]string, 0, len(findings))
	for _, finding := range findings {
		rendered = append(rendered, finding.render())
	}

	if slicesContains(rendered, "ruff per-file-ignores: tests/** -> S101") {
		t.Fatalf("allowed test ignore unexpectedly reported: %#v", rendered)
	}
	if slicesContains(
		rendered,
		"mypy override.ignore_missing_imports: external_pkg.*",
	) {
		t.Fatalf(
			"allowed external mypy import ignore unexpectedly reported: %#v",
			rendered,
		)
	}
	expected := []string{
		"ruff exclude: generated",
		"ruff per-file-ignores: pkg/** -> F401",
		"mypy override.disable_error_code: internal_pkg.* -> attr-defined",
		"mypy override.ignore_missing_imports: internal_pkg.*",
		"pyright ignore: vendor/**",
		"pylint ignore-paths: generated",
	}
	for _, want := range expected {
		if !slicesContains(rendered, want) {
			t.Fatalf("missing finding %q from %#v", want, rendered)
		}
	}
}

func TestCheckPyprojectIgnoresCommand(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  pyproject_ignores:
    enabled: true
    allowed_ignore_patterns:
      - tests/**
    allowed_exclude_patterns:
      - .venv
    allowed_mypy_missing_imports:
      - external_pkg.*
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	pyprojectPath := filepath.Join(tempDir, "pyproject.toml")
	mustWriteTestFile(
		t,
		pyprojectPath,
		strings.TrimSpace(`
[tool.ruff.lint.per-file-ignores]
"src/**" = ["F401"]
`)+"\n",
	)

	output := captureStderr(t, func() {
		if got := checkPyprojectIgnoresCommand(Config{}, []string{pyprojectPath}); got != 1 {
			t.Fatalf("checkPyprojectIgnoresCommand() = %d, want 1", got)
		}
	})

	if !strings.Contains(output, "contains forbidden linter file ignores") {
		t.Fatalf("unexpected output: %q", output)
	}
	if !strings.Contains(output, "ruff per-file-ignores: src/** -> F401") {
		t.Fatalf("missing rendered finding in output: %q", output)
	}
}

func TestFindCommentSuppressionsIgnoresStrings(t *testing.T) {
	tempDir := t.TempDir()
	path := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(
		t,
		path,
		strings.TrimSpace(`
"""
Module docstring mentioning # noqa should not count.
"""

text = "# type: ignore inside a string"

value = 1  # noqa: F401
`)+"\n",
	)

	patterns, err := compileCommentSuppressionPatterns(commentSuppressionSettings{
		Patterns: []commentSuppressionPattern{
			{Regex: `#\s*noqa\b`, Label: "noqa"},
			{Regex: `#\s*type:\s*ignore\b`, Label: "type: ignore"},
		},
	})
	if err != nil {
		t.Fatalf("compileCommentSuppressionPatterns() returned error: %v", err)
	}
	violations, err := findCommentSuppressions(path, patterns)
	if err != nil {
		t.Fatalf("findCommentSuppressions() returned error: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("len(violations) = %d, want 1 (%#v)", len(violations), violations)
	}
	if violations[0].Label != "noqa" {
		t.Fatalf("Label = %q, want %q", violations[0].Label, "noqa")
	}
}

func TestCheckCommentSuppressionsCommandUsesConfigPatterns(t *testing.T) {
	tempDir := t.TempDir()
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  comment_suppressions:
    enabled: true
    patterns:
      - regex: '#\s*custom:\s*bypass\b'
        label: custom bypass
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	pythonPath := filepath.Join(tempDir, "module.py")
	mustWriteTestFile(
		t,
		pythonPath,
		"result = 1  # custom: bypass\n",
	)

	output := captureStderr(t, func() {
		if got := checkCommentSuppressionsCommand(Config{}, []string{pythonPath}); got != 1 {
			t.Fatalf("checkCommentSuppressionsCommand() = %d, want 1", got)
		}
	})

	if !strings.Contains(output, "COMMENT-BASED LINT SUPPRESSION DETECTED") {
		t.Fatalf("unexpected output: %q", output)
	}
	if !strings.Contains(output, "[custom bypass] # custom: bypass") {
		t.Fatalf("missing configured label in output: %q", output)
	}
}

func TestExtractModuleDocstring(t *testing.T) {
	docstring, err := extractModuleDocstring(strings.TrimSpace(`
#!/usr/bin/env python3
# -*- coding: utf-8 -*-
# leading comment

"""Package docs.

See Also:
    PKG.md: Main package notes.
"""

import os
`))
	if err != nil {
		t.Fatalf("extractModuleDocstring() returned error: %v", err)
	}
	if !strings.Contains(docstring, "See Also:") {
		t.Fatalf("docstring = %q, want See Also content", docstring)
	}

	empty, err := extractModuleDocstring("import os\n")
	if err != nil {
		t.Fatalf("extractModuleDocstring(import) returned error: %v", err)
	}
	if empty != "" {
		t.Fatalf("extractModuleDocstring(import) = %q, want empty string", empty)
	}
}

func TestCollectModuleDocsViolations(t *testing.T) {
	tempDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		chdirErr := os.Chdir(previous)
		if chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	mustWriteTestFile(
		t,
		"docs/SOURCE_DOCS.md",
		"| `pkg/` | `PKG.md` | Main package docs |\n",
	)
	mustWriteTestFile(
		t,
		"pkg/__init__.py",
		strings.TrimSpace(`
"""Package docs.

See Also:
    PKG.md: Main package notes.
    missing.md: Missing doc reference.
    subdir/OTHER.md: Bad path reference.
"""
`)+"\n",
	)
	mustWriteTestFile(t, "pkg/PKG.md", "# Package docs\n")
	mustWriteTestFile(t, "pkg/README.md", "# Wrong name\n")
	mustWriteTestFile(t, "other/conftest.py", "")

	violations, err := collectModuleDocsViolations(
		[]string{"pkg/__init__.py", "other/conftest.py"},
		moduleDocsSettings{
			Enabled:            true,
			SourceDocsPath:     "docs/SOURCE_DOCS.md",
			CheckFilenames:     []string{"__init__.py", "conftest.py"},
			ExcludedDirs:       []string{".git", ".venv", "__pycache__"},
			BannedDocFilenames: []string{"README.md", "readme.md"},
		},
	)
	if err != nil {
		t.Fatalf("collectModuleDocsViolations() returned error: %v", err)
	}

	if !reflect.DeepEqual(violations.MissingDocstring, []string{"other/conftest.py"}) {
		t.Fatalf("MissingDocstring = %#v", violations.MissingDocstring)
	}
	if !reflect.DeepEqual(violations.MissingMarkdown, []string{"other/conftest.py"}) {
		t.Fatalf("MissingMarkdown = %#v", violations.MissingMarkdown)
	}
	if len(violations.MissingRefs) != 1 ||
		violations.MissingRefs[0].PythonFile != "pkg/__init__.py" ||
		!reflect.DeepEqual(
			violations.MissingRefs[0].Markdown,
			[]string{"pkg/README.md"},
		) {
		t.Fatalf("MissingRefs = %#v", violations.MissingRefs)
	}
	if !reflect.DeepEqual(violations.MissingIndex, []string{"pkg/README.md"}) {
		t.Fatalf("MissingIndex = %#v", violations.MissingIndex)
	}
	if len(violations.PathPrefixed) != 1 ||
		!reflect.DeepEqual(
			violations.PathPrefixed[0].Refs,
			[]string{"subdir/OTHER.md"},
		) {
		t.Fatalf("PathPrefixed = %#v", violations.PathPrefixed)
	}
	if len(violations.NonexistentRefs) != 1 ||
		!reflect.DeepEqual(violations.NonexistentRefs[0].Refs, []string{"missing.md"}) {
		t.Fatalf("NonexistentRefs = %#v", violations.NonexistentRefs)
	}
	if !reflect.DeepEqual(violations.BannedFilenames, []string{"pkg/README.md"}) {
		t.Fatalf("BannedFilenames = %#v", violations.BannedFilenames)
	}
}

func TestResolveGeminiRequestSettingsUsesOverrides(t *testing.T) {
	thinkingBudget := 512
	settings := GeminiSettings{
		Model:                geminiDefaultModel,
		ModelOverrides:       map[string]string{"code_ethos": "gemini-2.5-pro"},
		ServiceTier:          geminiServiceTierNormal,
		ServiceTierOverrides: map[string]string{"code_ethos": "flex"},
		ThinkingBudget:       &thinkingBudget,
		ThinkingBudgetOverrides: map[string]int{
			"code_ethos": 2048,
		},
		DisableSafetyFilters: true,
		Cache: GeminiCacheSettings{
			Enabled:    true,
			TTLSeconds: 3600,
		},
	}

	resolved := resolveGeminiRequestSettings(
		settings,
		"code_ethos",
		"/tmp/gemini-cache",
	)
	if resolved.Model != "gemini-2.5-pro" {
		t.Fatalf("Model = %q, want %q", resolved.Model, "gemini-2.5-pro")
	}
	if resolved.ServiceTier != "flex" {
		t.Fatalf("ServiceTier = %q, want %q", resolved.ServiceTier, "flex")
	}
	if resolved.ThinkingBudget == nil || *resolved.ThinkingBudget != 2048 {
		t.Fatalf("ThinkingBudget = %#v, want 2048", resolved.ThinkingBudget)
	}
	if !resolved.DisableSafetyFilters {
		t.Fatal("DisableSafetyFilters = false, want true")
	}
	if !resolved.Cache.Enabled {
		t.Fatal("Cache.Enabled = false, want true")
	}
	if resolved.Cache.Dir != "/tmp/gemini-cache" {
		t.Fatalf("Cache.Dir = %q, want %q", resolved.Cache.Dir, "/tmp/gemini-cache")
	}
}

func TestGeminiSafetySettingsDisabledUsesOffThresholds(t *testing.T) {
	settings := geminiSafetySettings(true)
	if len(settings) != 5 {
		t.Fatalf("len(settings) = %d, want 5", len(settings))
	}
	for _, item := range settings {
		if item.Threshold != "OFF" {
			t.Fatalf("threshold for %s = %q, want OFF", item.Category, item.Threshold)
		}
	}
}

func TestGeminiPromptForExplicitCachedContentReplacesPlaceholder(t *testing.T) {
	template := "Review these files.\n\n{code_content}\n"
	prompt := geminiPromptForExplicitCachedContent(template)
	if strings.Contains(prompt, "{code_content}") {
		t.Fatalf("prompt still contains placeholder: %q", prompt)
	}
	if !strings.Contains(prompt, "cached content") {
		t.Fatalf("prompt does not mention cached content: %q", prompt)
	}
}

func TestMatchesGeminiSelector(t *testing.T) {
	tempDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		chdirErr := os.Chdir(previous)
		if chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	mustWriteTestFile(t, "pkg/module.py", "print('ok')\n")
	mustWriteTestFile(t, "scripts/tool", "#!/usr/bin/env bash\necho ok\n")
	mustWriteTestFile(t, "vendor/generated.py", "print('skip')\n")
	mustWriteTestFile(t, "notes.txt", "hello\n")

	selector := GeminiFileSelector{
		IncludeExtensions:           []string{".py"},
		ExcludePrefixes:             []string{"vendor/"},
		AllowExtensionlessInScripts: true,
		ShebangMarkers:              []string{"bash", "sh"},
	}

	tests := []struct {
		path string
		want bool
	}{
		{path: "pkg/module.py", want: true},
		{path: "scripts/tool", want: true},
		{path: "vendor/generated.py", want: false},
		{path: "notes.txt", want: false},
	}

	for _, tc := range tests {
		got, err := matchesGeminiSelector(tc.path, selector)
		if err != nil {
			t.Fatalf("matchesGeminiSelector(%q) returned error: %v", tc.path, err)
		}
		if got != tc.want {
			t.Fatalf("matchesGeminiSelector(%q) = %t, want %t", tc.path, got, tc.want)
		}
	}
}

func TestPrepareGeminiChecksBuildsBatches(t *testing.T) {
	tempDir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd() failed: %v", err)
	}
	err = os.Chdir(tempDir)
	if err != nil {
		t.Fatalf("os.Chdir(%q) failed: %v", tempDir, err)
	}
	t.Cleanup(func() {
		chdirErr := os.Chdir(previous)
		if chdirErr != nil {
			t.Fatalf("restore working directory failed: %v", chdirErr)
		}
	})

	mustWriteTestFile(t, "a.py", "print('a')\n")
	mustWriteTestFile(t, "b.py", "print('b')\n")
	mustWriteTestFile(t, "c.py", "print('c')\n")
	mustWriteTestFile(t, "large.py", strings.Repeat("x", 2048))

	pack := GeminiPromptPack{
		Checks: map[string]GeminiPromptCheckSpec{
			"code_ethos": {
				FileScope:     "code",
				BatchSize:     2,
				MaxFileSizeKB: 1,
				Selector: GeminiFileSelector{
					IncludeExtensions: []string{".py"},
				},
			},
		},
		Prompts: map[string]string{
			"code_ethos": "Review this batch.\n{code_content}",
		},
	}

	prepared, err := prepareGeminiChecks(
		pack,
		[]string{"a.py", "b.py", "c.py", "large.py"},
		"",
		GeminiSettings{
			Model:       geminiDefaultModel,
			ServiceTier: geminiServiceTierNormal,
			Cache: GeminiCacheSettings{
				Enabled: true,
			},
		},
		filepath.Join(tempDir, ".cache"),
	)
	if err != nil {
		t.Fatalf("prepareGeminiChecks() returned error: %v", err)
	}
	if len(prepared) != 1 {
		t.Fatalf("len(prepared) = %d, want 1", len(prepared))
	}
	plan := prepared[0].Plan
	if !reflect.DeepEqual(
		plan.SelectedFiles,
		[]string{"a.py", "b.py", "c.py", "large.py"},
	) {
		t.Fatalf("SelectedFiles = %#v", plan.SelectedFiles)
	}
	if !reflect.DeepEqual(plan.IncludedFiles, []string{"a.py", "b.py", "c.py"}) {
		t.Fatalf("IncludedFiles = %#v", plan.IncludedFiles)
	}
	if !reflect.DeepEqual(plan.SkippedLargeFiles, []string{"large.py"}) {
		t.Fatalf("SkippedLargeFiles = %#v", plan.SkippedLargeFiles)
	}
	if len(plan.Batches) != 2 {
		t.Fatalf("len(plan.Batches) = %d, want 2", len(plan.Batches))
	}
	if !reflect.DeepEqual(plan.Batches[0].Files, []string{"a.py", "b.py"}) {
		t.Fatalf("first batch files = %#v", plan.Batches[0].Files)
	}
	if !reflect.DeepEqual(plan.Batches[1].Files, []string{"c.py"}) {
		t.Fatalf("second batch files = %#v", plan.Batches[1].Files)
	}
	if plan.Model != geminiDefaultModel {
		t.Fatalf("Model = %q, want %q", plan.Model, geminiDefaultModel)
	}
	if plan.ServiceTier != geminiServiceTierNormal {
		t.Fatalf("ServiceTier = %q, want %q", plan.ServiceTier, geminiServiceTierNormal)
	}
	if !plan.CacheEnabled {
		t.Fatal("CacheEnabled = false, want true")
	}
	if prepared[0].Batches[0].ExplicitAPIKey == "" {
		t.Fatal("first batch ExplicitAPIKey is empty")
	}
	if !strings.Contains(prepared[0].Batches[0].CachedPrompt, "cached content") {
		t.Fatalf(
			"CachedPrompt = %q, want cached-content guidance",
			prepared[0].Batches[0].CachedPrompt,
		)
	}
}

func TestFilterGeminiModalAllowlistedViolations(t *testing.T) {
	violations := []geminiViolation{
		{
			Severity:     "CRITICAL",
			File:         "app/handlers/modal.py",
			Message:      "This modal gating feature enablement silently degrades behavior.",
			EthosSection: "Section 19",
		},
		{
			Severity:     "WARNING",
			File:         "app/handlers/modal.py",
			Message:      "Use a clearer variable name.",
			EthosSection: "Section 8",
		},
	}

	filtered := filterGeminiModalAllowlistedViolations(
		violations,
		[]string{"app/**/*.py"},
	)
	if len(filtered) != 1 {
		t.Fatalf("len(filtered) = %d, want 1", len(filtered))
	}
	if filtered[0].Message != "Use a clearer variable name." {
		t.Fatalf("unexpected remaining violation: %#v", filtered[0])
	}
}

func TestParseGeminiChangedLines(t *testing.T) {
	diff := strings.Join([]string{
		"@@ -10,2 +10,3 @@",
		"@@ -20,0 +25,2 @@",
	}, "\n")
	changed := parseGeminiChangedLines(diff)
	for _, line := range []int{10, 11, 12, 25, 26} {
		if _, ok := changed[line]; !ok {
			t.Fatalf("parseGeminiChangedLines() missing line %d", line)
		}
	}
}

func TestFilterGeminiViolationsByDiff(t *testing.T) {
	violations := []geminiViolation{
		{
			Severity: "CRITICAL",
			File:     "pkg/module.py",
			Line:     12,
			Message:  "Changed-code failure",
		},
		{
			Severity: "WARNING",
			File:     "pkg/module.py",
			Line:     99,
			Message:  "Pre-existing issue",
		},
		{
			Severity: "INFO",
			File:     "pkg/module.py",
			Line:     0,
			Message:  "Unknown line should stay in diff",
		},
	}

	filtered := filterGeminiViolationsByDiff(
		violations,
		map[string]map[int]struct{}{
			"pkg/module.py": {
				12: {},
			},
		},
	)

	if len(filtered.InDiff) != 2 {
		t.Fatalf("len(filtered.InDiff) = %d, want 2", len(filtered.InDiff))
	}
	if len(filtered.PreExisting) != 1 {
		t.Fatalf("len(filtered.PreExisting) = %d, want 1", len(filtered.PreExisting))
	}
	if !filtered.hasBlockingCriticals() {
		t.Fatal("filtered.hasBlockingCriticals() = false, want true")
	}
	if !filtered.hasAnyInDiff() {
		t.Fatal("filtered.hasAnyInDiff() = false, want true")
	}
	if filtered.PreExisting[0].Message != "Pre-existing issue" {
		t.Fatalf("unexpected pre-existing violation: %#v", filtered.PreExisting[0])
	}
}

func TestGeminiCacheRoundTrip(t *testing.T) {
	cache := geminiResponseCache{
		Enabled: true,
		Dir:     t.TempDir(),
		TTL:     time.Hour,
	}
	key := "abc123"
	err := writeGeminiCache(cache, key, "{\"ok\":true}")
	if err != nil {
		t.Fatalf("writeGeminiCache() returned error: %v", err)
	}
	text, ok, err := readGeminiCache(cache, key)
	if err != nil {
		t.Fatalf("readGeminiCache() returned error: %v", err)
	}
	if !ok {
		t.Fatal("readGeminiCache() returned cache miss, want hit")
	}
	if text != "{\"ok\":true}" {
		t.Fatalf("cached text = %q, want %q", text, "{\"ok\":true}")
	}
}

func TestReadGeminiCacheExpiresEntries(t *testing.T) {
	cacheDir := t.TempDir()
	cache := geminiResponseCache{
		Enabled: true,
		Dir:     cacheDir,
		TTL:     time.Second,
	}
	key := "expired"
	path := geminiCachePath(cache, key)
	entry := geminiCacheEntry{
		CreatedAt: time.Now().Add(-2 * time.Second).UTC().Format(time.RFC3339Nano),
		Text:      "stale",
	}
	mustWriteTestFile(t, path, mustJSON(t, entry))

	_, ok, err := readGeminiCache(cache, key)
	if err != nil {
		t.Fatalf("readGeminiCache() returned error: %v", err)
	}
	if ok {
		t.Fatal("readGeminiCache() returned hit for expired entry")
	}
	_, statErr := os.Stat(path)
	if !os.IsNotExist(statErr) {
		t.Fatalf("expired cache file still exists: %v", statErr)
	}
}

func TestGeminiExplicitCacheRoundTrip(t *testing.T) {
	cache := geminiResponseCache{
		APIEnabled: true,
		Dir:        t.TempDir(),
		APITTL:     time.Hour,
	}
	key := "explicit"
	expire := time.Now().Add(time.Hour)
	err := writeGeminiExplicitCache(cache, key, "cachedContents/123", expire)
	if err != nil {
		t.Fatalf("writeGeminiExplicitCache() returned error: %v", err)
	}
	name, ok, err := readGeminiExplicitCache(cache, key)
	if err != nil {
		t.Fatalf("readGeminiExplicitCache() returned error: %v", err)
	}
	if !ok {
		t.Fatal("readGeminiExplicitCache() returned miss, want hit")
	}
	if name != "cachedContents/123" {
		t.Fatalf("cache name = %q, want %q", name, "cachedContents/123")
	}
}

func mustWriteTestFile(t *testing.T, path string, contents string) {
	t.Helper()
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("os.MkdirAll(%q) failed: %v", path, err)
	}
	err = os.WriteFile(path, []byte(contents), 0o644)
	if err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", path, err)
	}
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() {
		os.Stderr = original
	})

	fn()

	err = writer.Close()
	if err != nil {
		t.Fatalf("writer.Close() failed: %v", err)
	}
	os.Stderr = original

	var buffer bytes.Buffer
	_, err = buffer.ReadFrom(reader)
	if err != nil {
		t.Fatalf("buffer.ReadFrom() failed: %v", err)
	}
	err = reader.Close()
	if err != nil {
		t.Fatalf("reader.Close() failed: %v", err)
	}

	return buffer.String()
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	original := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stdout = writer
	t.Cleanup(func() {
		os.Stdout = original
	})

	fn()

	err = writer.Close()
	if err != nil {
		t.Fatalf("writer.Close() failed: %v", err)
	}
	os.Stdout = original

	var buffer bytes.Buffer
	_, err = buffer.ReadFrom(reader)
	if err != nil {
		t.Fatalf("buffer.ReadFrom() failed: %v", err)
	}
	err = reader.Close()
	if err != nil {
		t.Fatalf("reader.Close() failed: %v", err)
	}

	return buffer.String()
}

func slicesContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}

	return false
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}

	return string(data)
}
