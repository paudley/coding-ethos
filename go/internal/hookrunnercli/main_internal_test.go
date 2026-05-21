// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// fixtures.
//
//nolint:paralleltest,gocyclo,cyclop,funlen,lll,varnamelen // Uses process-global
package hookrunnercli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const geminiGeneratedCacheName = "cachedContents/generated"

func TestMain(m *testing.M) {
	_ = os.Setenv(hookOutputFormatEnv, hookOutputFormatHuman)

	if len(os.Args) >= minCollectionItems && os.Args[1] == "run-group" {
		cfg, err := loadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)
			os.Exit(1)
		}

		os.Exit(runHookGroupCommand(cfg, os.Args[2:]))
	}

	err := installHostGoTestGuard()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	os.Exit(m.Run())
}

func installHostGoTestGuard() error {
	guardDir, err := os.MkdirTemp("", "coding-ethos-test-go-guard-*")
	if err != nil {
		return fmt.Errorf("create test go guard dir: %w", err)
	}

	script := `#!/usr/bin/env sh
printf 'FATAL: hook-runner tests attempted to use host go. Install an explicit fake go binary for this test.\n' >&2
printf 'argv: %s\n' "$*" >&2
exit 97
`

	guardPath := filepath.Join(guardDir, "go")

	inlineErr0 := os.WriteFile(guardPath, []byte(script), 0o600)
	if inlineErr0 != nil {
		return fmt.Errorf("write test go guard: %w", inlineErr0)
	}

	inlineErr0 = os.Chmod(guardPath, 0o700)
	if inlineErr0 != nil {
		return fmt.Errorf("chmod test go guard: %w", inlineErr0)
	}

	inlineErr1 := os.Setenv(
		"PATH",
		guardDir+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if inlineErr1 != nil {
		return fmt.Errorf("set PATH for test go guard: %w", inlineErr1)
	}

	return nil
}

func TestConsumerRootHonorsExplicitEnvironment(t *testing.T) {
	root := filepath.Join(t.TempDir(), "coding-ethos")

	err := os.MkdirAll(root, 0o755)
	if err != nil {
		t.Fatalf("mkdir root: %v", err)
	}

	t.Setenv(consumerRootEnv, root)

	if got := consumerRoot(root); got != root {
		t.Fatalf("consumerRoot() = %q, want %q", got, root)
	}
}

func TestConsumerRootIgnoresUnrelatedExplicitEnvironment(t *testing.T) {
	root := t.TempDir()
	other := t.TempDir()
	t.Setenv(consumerRootEnv, root)

	if got := consumerRoot(other); got == root {
		t.Fatalf("consumerRoot() used unrelated explicit root %q", root)
	}
}

func TestConsumerRootIgnoresExplicitRootForIgnoredWorktreeScratch(t *testing.T) {
	root := setupGitHookTestRepo(t)
	mustWriteTestFile(t, filepath.Join(root, ".gitignore"), "sandbox-tmp/\n")

	ignoredRoot := filepath.Join(root, "sandbox-tmp", "case")
	if err := os.MkdirAll(ignoredRoot, 0o755); err != nil {
		t.Fatalf("mkdir ignored root: %v", err)
	}

	if got := resolveConsumerRoot(ignoredRoot, root, root, ""); got != ignoredRoot {
		t.Fatalf("resolveConsumerRoot() = %q, want ignored scratch root %q", got, ignoredRoot)
	}

	t.Setenv(consumerRootEnv, root)
	t.Chdir(ignoredRoot)
	if got := repoRoot(); got != "." {
		t.Fatalf("repoRoot() = %q, want cwd fallback", got)
	}
}

func TestResolveConsumerRootPrefersOwningGitRootOverSuperproject(t *testing.T) {
	ethosRoot := filepath.Join(t.TempDir(), "coding-ethos")
	superproject := filepath.Dir(ethosRoot)

	got := resolveConsumerRoot(ethosRoot, "", ethosRoot, superproject)
	if got != ethosRoot {
		t.Fatalf("resolveConsumerRoot() = %q, want owning git root %q", got, ethosRoot)
	}
}

func TestLoadGeminiPromptPackParsesGeneratedContract(t *testing.T) {
	t.Parallel()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}

	bundleRoot := filepath.Clean(
		filepath.Join(filepath.Dir(file), "..", "..", "..", "pre-commit"),
	)

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
	t.Parallel()

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
	t.Parallel()

	_, err := parseGeminiCLIOptions([]string{"--nope"})
	if err == nil {
		t.Fatal("parseGeminiCLIOptions() unexpectedly accepted unknown flag")
	}
}

func TestRootConfigValue(t *testing.T) {
	t.Parallel()

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

func TestQuietFilterSummarizesFailuresAndSuppressesNoise(t *testing.T) {
	t.Parallel()

	filter, err := compileQuietFilter(QuietFilterConfig{
		ANSIRegex:        `\x1b\[[0-9;]*m`,
		PassedRegex:      `^PASS `,
		FailedRegex:      `^FAIL `,
		SkippedRegex:     `^SKIP `,
		StatusRegex:      `^(PASS|FAIL|SKIP) `,
		PreexistingRegex: `^Pre-existing:`,
		SeparatorRegex:   `^=+$`,
		MetadataPrefixes: []string{"metadata:"},
		SuppressExact:    []string{"exact noise"},
		SuppressPrefixes: []string{"prefix noise"},
		SuppressRegexes:  []string{`regex noise \d+`},
		BannerWidth:      8,
	})
	if err != nil {
		t.Fatalf("compileQuietFilter() returned error: %v", err)
	}

	var output bytes.Buffer

	status := runQuietFilter(
		filter,
		strings.NewReader(strings.Join([]string{
			"PASS formatter",
			"metadata: hidden",
			"FAIL lint",
			"exact noise",
			"prefix noise detail",
			"regex noise 123",
			"========",
			"How to fix:",
			"  first advice",
			"Pre-existing:",
			"  old issue",
			"",
			"",
			"visible finding",
			"SKIP optional",
		}, "\n")),
		&output,
	)
	if status != 0 {
		t.Fatalf("runQuietFilter() = %d, want 0", status)
	}

	text := output.String()
	for _, want := range []string{
		"How to fix:",
		"first advice",
		"visible finding",
		"1 passed",
		"1 failed",
		"1 skipped",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("quiet-filter output missing %q: %q", want, text)
		}
	}

	for _, forbidden := range []string{"metadata: hidden", "exact noise", "old issue"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("quiet-filter output retained %q: %q", forbidden, text)
		}
	}
}

func TestQuietFilterRejectsInvalidRegex(t *testing.T) {
	t.Parallel()

	_, err := compileQuietFilter(QuietFilterConfig{FailedRegex: "["})
	if err == nil || !strings.Contains(err.Error(), "quiet_filter.failed_regex") {
		t.Fatalf("compileQuietFilter() error = %v", err)
	}
}

func TestValidateManifestData(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestStagedFilesUsesGitDiffNameOnly(t *testing.T) {
	tempDir := t.TempDir()
	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		`#!/usr/bin/env sh
case "$*" in
  "diff --cached --name-only")
    printf 'docs/plans/feature-a/metadata.yaml\npkg/app.py\n'
    exit 0
    ;;
esac
printf 'unexpected git invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODING_ETHOS_REAL_GIT", "")

	got := stagedFiles()

	want := []string{"docs/plans/feature-a/metadata.yaml", "pkg/app.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stagedFiles() = %#v, want %#v", got, want)
	}
}

func TestCheckPlanCompletionErrors(t *testing.T) {
	tempDir := t.TempDir()

	t.Chdir(tempDir)

	mustWriteTestFile(t, "docs/plans/feature-a/metadata.yaml", "status: review\n")
	mustWriteTestFile(
		t,
		"docs/plans/feature-a/tasks.md",
		"- [ ] unfinished\n- [x] done\n",
	)

	findings, status, err := checkPlanCompletionErrors(
		"docs/plans/feature-a/metadata.yaml",
		planCompletionSettings{
			CompletedStatusValues: []string{"review", "complete"},
		},
	)
	if err != nil {
		t.Fatalf("checkPlanCompletionErrors() returned error: %v", err)
	}

	if status != "review" {
		t.Fatalf("status = %q, want review", status)
	}

	if len(findings) == 0 {
		t.Fatal("checkPlanCompletionErrors() returned no findings")
	}

	if findings[0].Message != "unchecked plan item" {
		t.Fatalf("unexpected findings: %#v", findings)
	}

	mustWriteTestFile(t, "docs/plans/feature-b/metadata.yaml", "status: in_progress\n")
	mustWriteTestFile(t, "docs/plans/feature-b/tasks.md", "- [ ] unfinished\n")

	findings, _, err = checkPlanCompletionErrors(
		"docs/plans/feature-b/metadata.yaml",
		planCompletionSettings{
			CompletedStatusValues: []string{"review", "complete"},
		},
	)
	if err != nil {
		t.Fatalf("checkPlanCompletionErrors() returned error: %v", err)
	}

	if len(findings) != 0 {
		t.Fatalf(
			"checkPlanCompletionErrors(in_progress) = %#v, want no findings",
			findings,
		)
	}
}

func TestCheckPlanCompletionCommandReportsCompletedPlanWithUncheckedItems(
	t *testing.T,
) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	bundleRoot := writeTestBundleRoot(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  plan_completion:
    enabled: true
    metadata_filename: metadata.yaml
    root_markers:
      - docs/plans/
    completed_status_values:
      - review
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
	mustWriteTestFile(t, "docs/plans/feature-a/metadata.yaml", "status: review\n")
	mustWriteTestFile(t, "docs/plans/feature-a/tasks.md", "- [ ] unfinished\n")

	output := captureStderr(t, func() {
		if got := checkPlanCompletionCommand(
			Config{},
			[]string{"docs/plans/feature-a/metadata.yaml"},
		); got != 1 {
			t.Fatalf("checkPlanCompletionCommand() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"PLAN COMPLETION FRAUD DETECTED",
		"unchecked plan item",
		"unfinished",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("plan completion output missing %q:\n%s", want, output)
		}
	}
}

func TestFindCommentSuppressionsIgnoresStrings(t *testing.T) {
	t.Parallel()

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
	bundleRoot := writeTestBundleRoot(t, tempDir)

	t.Setenv(precommitRootEnv, bundleRoot)

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

	if !strings.Contains(
		output,
		"[custom bypass] comment-based lint suppression # custom: bypass",
	) {
		t.Fatalf("missing configured label in output: %q", output)
	}
}

func TestExtractModuleDocstring(t *testing.T) {
	t.Parallel()

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

	single, err := extractModuleDocstring("u'Package docs.'\nVALUE = 1\n")
	if err != nil {
		t.Fatalf("extractModuleDocstring(single) returned error: %v", err)
	}

	if single != "Package docs." {
		t.Fatalf("single docstring = %q", single)
	}

	_, err = extractModuleDocstring("'unterminated\n")
	if err == nil {
		t.Fatal("extractModuleDocstring(unterminated) returned nil error")
	}
}

func TestCollectModuleDocsViolations(t *testing.T) {
	tempDir := t.TempDir()

	t.Chdir(tempDir)

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

func TestModuleDocsRequiredRuntimeDirsStayExcluded(t *testing.T) {
	t.Parallel()

	excluded := appendRequiredModuleDocsExcludedDirs([]string{".venv"})
	settings := moduleDocsSettings{
		CheckFilenames: []string{"__init__.py", "conftest.py"},
		ExcludedDirs:   excluded,
	}

	for _, path := range []string{
		".git/hooks/__init__.py",
		".coding-ethos/cache/pr28-merge/pkg/__init__.py",
		".code-ethos/cache/pkg/conftest.py",
	} {
		if shouldCheckModuleDocsFile(path, settings) {
			t.Fatalf("shouldCheckModuleDocsFile(%q) = true, want false", path)
		}
	}

	if !shouldCheckModuleDocsFile("pkg/__init__.py", settings) {
		t.Fatal("shouldCheckModuleDocsFile(pkg/__init__.py) = false, want true")
	}
}

func TestCheckModuleDocsCommandDiscoversAndReportsRepositoryDocsContract(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	bundleRoot := writeTestBundleRoot(t, tempDir)
	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
python:
  module_docs:
    enabled: true
    source_docs_path: docs/SOURCE_DOCS.md
    check_filenames:
      - __init__.py
      - conftest.py
    excluded_dirs:
      - .git
      - .venv
      - __pycache__
    banned_doc_filenames:
      - README.md
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)
	mustWriteTestFile(
		t,
		"docs/SOURCE_DOCS.md",
		"| `pkg/` | `MODULE.md` | Package docs |\n",
	)
	mustWriteTestFile(
		t,
		"pkg/__init__.py",
		strings.TrimSpace(`
"""Package docs.

See Also:
    MISSING.md: Missing doc reference.
    subdir/MODULE.md: Bad path reference.
"""
`)+"\n",
	)
	mustWriteTestFile(t, "pkg/README.md", "# Wrong name\n")

	output := captureStderr(t, func() {
		if got := checkModuleDocsCommand(Config{}, nil); got != 1 {
			t.Fatalf("checkModuleDocsCommand() = %d, want 1", got)
		}
	})
	for _, want := range []string{
		"MODULE DOCUMENTATION CHECK FAILED",
		"missing_refs",
		"banned_filename",
		"path_prefixed_refs",
		"nonexistent_refs",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("module docs output missing %q:\n%s", want, output)
		}
	}
}

func TestFixTextNormalizesWhitespaceAndSkipsBinaryFiles(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	textPath := filepath.Join(tempDir, "text.txt")
	binaryPath := filepath.Join(tempDir, "binary.bin")

	mustWriteTestFile(t, textPath, "one \r\ntwo\t\r\n")

	err := os.WriteFile(binaryPath, []byte{0, 1, 2}, 0o600)
	if err != nil {
		t.Fatalf("write binary fixture: %v", err)
	}

	if got := fixText(Config{}, []string{textPath, binaryPath}); got != 0 {
		t.Fatalf("fixText() = %d, want 0", got)
	}

	text, err := os.ReadFile(textPath)
	if err != nil {
		t.Fatalf("read fixed text: %v", err)
	}

	if string(text) != "one\ntwo\n" {
		t.Fatalf("fixed text = %q", text)
	}

	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatalf("read binary fixture: %v", err)
	}

	if !bytes.Equal(binary, []byte{0, 1, 2}) {
		t.Fatalf("binary file changed: %#v", binary)
	}
}

func TestRuntimeIgnoreFindingsReportMissingIgnores(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	mustWriteTestFile(t, ".gitignore", ".code-ethos/cache/\n")

	findings := runtimeIgnoreFindings([]string{
		"",
		".code-ethos/cache/",
		".coding-ethos/",
	})
	if len(findings) != 1 ||
		!strings.Contains(findings[0], ".coding-ethos/ is not ignored") {
		t.Fatalf("findings = %#v", findings)
	}
}

func TestCheckRuntimeIgnoresCommandUsesGitIgnoreContract(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)
	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		`#!/usr/bin/env sh
case "$*" in
  "check-ignore --quiet .code-ethos/cache/"|"check-ignore --quiet .coding-ethos/"|"check-ignore --quiet .coding-ethos/hook-runs/example/stdout.log")
    exit 0
    ;;
esac
printf 'unexpected git invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CODING_ETHOS_REAL_GIT", "")

	if got := checkRuntimeIgnoresCommand(Config{}, nil); got != 0 {
		t.Fatalf("checkRuntimeIgnoresCommand() = %d, want 0", got)
	}

	if got := requiredRuntimeIgnorePaths(); len(got) != 3 {
		t.Fatalf("requiredRuntimeIgnorePaths() = %#v", got)
	}
}

func TestHookPlanFormatsAllSupportedOutputs(t *testing.T) {
	t.Parallel()

	plan := buildHookPlan(hookSettings{ParallelGroups: true})
	if len(plan.Groups) == 0 {
		t.Fatal("buildHookPlan() returned no groups")
	}

	for _, format := range []string{hookOutputFormatHuman, hookOutputFormatJSON, hookOutputFormatTOON} {
		output := formatHookPlan(plan, format)
		if !strings.Contains(output, "format") &&
			!strings.Contains(output, "HOOK PLAN") {
			t.Fatalf("formatHookPlan(%s) = %q", format, output)
		}
	}
}

func TestFormatGroupWithNoMatchingFilesIsNoop(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	textPath := filepath.Join(tempDir, "README.md")
	textBefore := "docs  \n"
	mustWriteTestFile(t, textPath, textBefore)

	if got := runFormatGroup(Config{}, []string{textPath}, false); got != 0 {
		t.Fatalf("runFormatGroup(no code files) = %d, want 0", got)
	}

	textAfter, err := os.ReadFile(textPath)
	if err != nil {
		t.Fatalf("read no-code file after format group: %v", err)
	}

	if string(textAfter) != textBefore {
		t.Fatalf("runFormatGroup changed non-code file to %q", textAfter)
	}

	if got := runFormatGroupCommand(Config{}, []string{textPath}); got != 0 {
		t.Fatalf("runFormatGroupCommand(no code files) = %d, want 0", got)
	}

	if got := runFormatGroup(Config{}, nil, true); got != 0 {
		t.Fatalf("runFormatGroup(restage no files) = %d, want 0", got)
	}
}

func TestFormatGroupRunsManagedFormattersForMatchingFiles(t *testing.T) {
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	mustWriteTestFile(t, "app.py", "print('ok')\n")
	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")

	fakeBin := filepath.Join(tempDir, "bin")

	err := os.MkdirAll(fakeBin, 0o700)
	if err != nil {
		t.Fatalf("create fake bin: %v", err)
	}

	mustWriteExecutable(t, filepath.Join(fakeBin, "uv"), "#!/usr/bin/env sh\nexit 0\n")
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin", "golangci-lint"),
		"#!/usr/bin/env sh\nexit 0\n",
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	bundleRoot := writeTestBundleRoot(t, tempDir)

	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(consumerRootEnv, tempDir)

	got := runFormatGroup(Config{}, []string{"app.py", "go/main.go"}, false)
	if !nativeSandboxAvailable && got != 0 {
		return
	}

	if got != 0 {
		t.Fatalf("runFormatGroup(matching files) = %d, want 0", got)
	}
}

func TestFormatGroupRestageSkipsUnchangedFiles(t *testing.T) {
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv("CODING_ETHOS_REAL_GIT", "")
	t.Setenv(consumerRootEnv, tempDir)

	mustWriteTestFile(t, "go/go.mod", "module example.test/repo\n")
	mustWriteTestFile(t, "go/main.go", "package main\n")
	t.Setenv(precommitRootEnv, writeTestBundleRoot(t, tempDir))
	runGitTestCommandInDir(t, tempDir, "add", ".")
	runGitTestCommandInDir(t, tempDir, "commit", "-m", "baseline")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin", "golangci-lint"),
		"#!/usr/bin/env sh\nexit 0\n",
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		"#!/usr/bin/env sh\necho unexpected git \"$@\" >&2\nexit 88\n",
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	got := runFormatGroup(Config{}, []string{"go/main.go"}, true)
	if !nativeSandboxAvailable && got != 0 {
		return
	}

	if got != 0 {
		t.Fatalf("runFormatGroup(unchanged restage) = %d, want 0", got)
	}
}

func TestFormatGroupRestagesFormatterChanges(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv("CODING_ETHOS_REAL_GIT", "")
	t.Setenv(consumerRootEnv, tempDir)

	mustWriteTestFile(t, "app.py", "print('ok')\n")
	snapshots := fileSnapshots([]string{"app.py"})
	mustWriteTestFile(t, "app.py", "print(\"ok\")\n")

	fakeBin := filepath.Join(tempDir, "bin")
	gitLog := filepath.Join(tempDir, "git.log")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		"#!/usr/bin/env sh\nprintf '%s\\n' \"$*\" >> "+shellQuoteForTest(gitLog)+"\nexit 0\n",
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	changed := changedExistingFiles([]string{"app.py"}, snapshots)
	if got := restageFiles(changed); got != 0 {
		t.Fatalf("restageFiles(changed) = %d, want 0", got)
	}

	content, err := os.ReadFile(gitLog)
	if err != nil {
		t.Fatalf("read git log: %v", err)
	}

	if !strings.Contains(string(content), "add -- app.py") {
		t.Fatalf("git add was not called for changed file:\n%s", string(content))
	}
}

func TestToolchainGroupCommandsWithNoMatchingFilesAreNoops(t *testing.T) {
	t.Parallel()

	commands := map[string]func(Config, []string) int{
		"actionlint":      runActionlint,
		"bandit":          runBandit,
		"complexity":      runPythonComplexity,
		"dotenv-linter":   runDotenvLinter,
		"go-test":         runGoTests,
		"go-vet":          runGoVet,
		"gofmt-check":     runGoFormatCheck,
		"golangci-lint":   runGolangciLint,
		"hadolint":        runHadolint,
		"maintainability": runPythonMaintainability,
		"sqlfluff":        runSQLFluff,
		"tombi":           runTombi,
		"vulture":         runPythonVulture,
	}

	for name, run := range commands {
		if got := run(Config{}, nil); got != 0 {
			t.Fatalf("%s with no files = %d, want 0", name, got)
		}
	}
}

func TestRunHookGroupInProcessAggregatesCommandResults(t *testing.T) {
	t.Parallel()

	group := hookGroup{
		Name: "sample",
		Commands: []hookCommand{
			{Name: "pass", Run: func(Config, []string) int { return 0 }},
			{Name: "fail", Run: func(Config, []string) int { return 2 }},
		},
	}

	result := runHookGroupInProcess(Config{}, group, []string{"pkg/app.py"})
	if result.Name != "sample" || result.ExitCode != 1 || result.Status != statusFail {
		t.Fatalf("group result = %#v", result)
	}

	if len(result.Commands) != 2 ||
		result.Commands[0].Status != statusPass ||
		result.Commands[1].Status != statusFail {
		t.Fatalf("command results = %#v", result.Commands)
	}
}

func TestRunHookGroupInProcessFiltersCommandFilesAndSkipsEmpty(t *testing.T) {
	t.Parallel()

	received := []string(nil)
	ranSkipped := false
	group := hookGroup{
		Name: "sample",
		Commands: []hookCommand{
			{
				Name: "python",
				Filter: func(files []string) []string {
					return []string{"pkg/app.py"}
				},
				Run: func(_ Config, files []string) int {
					received = append([]string(nil), files...)

					return 0
				},
			},
			{
				Name: "empty",
				Filter: func(_ []string) []string {
					return nil
				},
				Run: func(Config, []string) int {
					ranSkipped = true

					return 1
				},
			},
		},
	}

	result := runHookGroupInProcess(
		Config{},
		group,
		[]string{"README.md", "pkg/app.py"},
	)
	if result.ExitCode != 0 || result.Status != statusPass {
		t.Fatalf("group result = %#v", result)
	}

	if !reflect.DeepEqual(received, []string{"pkg/app.py"}) {
		t.Fatalf("filtered command files = %#v", received)
	}

	if ranSkipped {
		t.Fatal("command with empty filtered file list ran")
	}
}

func TestRunNamedHookGroupsSkipsGroupsWithNoMatchingFiles(t *testing.T) {
	t.Parallel()

	if got := runNamedHookGroups(
		Config{},
		[]string{"go", "python-static", "ai"},
		[]string{"README.md"},
	); got != 0 {
		t.Fatalf("runNamedHookGroups(no matching files) = %d, want 0", got)
	}
}

func TestGoGroupStopsBeforeExpensiveTailWhenLintFails(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	mustWriteTestFile(t, "go/main.go", "package main\n")

	ranGoTest := false
	ranCoverage := false
	commands := canonicalHookCommands()
	commands["go-format"] = func(Config, []string) int { return 0 }
	commands["go-vet"] = func(Config, []string) int { return 0 }
	commands["policy-golangci-lint"] = func(Config, []string) int { return 1 }
	commands["go-test"] = func(Config, []string) int {
		ranGoTest = true

		return 0
	}
	commands["go-coverage"] = func(Config, []string) int {
		ranCoverage = true

		return 0
	}

	group := canonicalHookGroupsFromCommands(commands)["go"]
	result := runHookGroupInProcess(Config{}, group, []string{"go/main.go"})

	if result.ExitCode != 1 || result.Status != statusFail {
		t.Fatalf("go group result = %#v", result)
	}

	if ranGoTest || ranCoverage {
		t.Fatalf(
			"go group ran expensive tail after lint failure: test=%t coverage=%t",
			ranGoTest,
			ranCoverage,
		)
	}

	if len(result.Commands) != goGroupSequentialPrefix {
		t.Fatalf("commands = %#v, want sequential prefix only", result.Commands)
	}
}

func TestRunHookGroupsInProcessReturnsFailure(t *testing.T) {
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)

	groups := []hookGroup{
		{
			Name: "pass",
			Commands: []hookCommand{
				{Name: "ok", Run: func(Config, []string) int { return 0 }},
			},
		},
		{
			Name: "fail",
			Commands: []hookCommand{
				{Name: "bad", Run: func(Config, []string) int { return 1 }},
			},
		},
	}

	output := captureStdout(t, func() {
		if got := runHookGroupsInProcess(Config{}, groups, nil); got != 1 {
			t.Fatalf("runHookGroupsInProcess = %d, want 1", got)
		}
	})

	for _, want := range []string{
		"status: FAIL",
		"failed_checks[1]{name,status}:",
		"bad,FAIL",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("failure summary missing %q:\n%s", want, output)
		}
	}
}

func TestFormatRootConfigValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		value any
		want  string
	}{
		{value: nil, want: ""},
		{value: "text", want: "text"},
		{value: true, want: "true"},
		{value: 7, want: "7"},
		{value: map[string]any{"b": float64(2)}, want: `{"b":2}`},
	}
	for _, test := range tests {
		got, err := formatRootConfigValue(test.value)
		if err != nil {
			t.Fatalf("formatRootConfigValue(%#v) returned error: %v", test.value, err)
		}

		if got != test.want {
			t.Fatalf(
				"formatRootConfigValue(%#v) = %q, want %q",
				test.value,
				got,
				test.want,
			)
		}
	}
}

func TestManifestValidationHelpers(t *testing.T) {
	t.Parallel()

	settings := manifestValidationSettings{
		RequiredStringFields: []string{"version", "name"},
		RequiredListSections: map[string]manifestValidationListSpec{
			"repositories": {
				Required:             true,
				RequiredStringFields: []string{"name", "url"},
				OptionalStringFields: []string{"branch"},
			},
		},
	}

	valid := map[string]any{
		"version": "1",
		"name":    "demo",
		"repositories": []any{
			map[string]any{
				"name":   "repo",
				"url":    "https://example.invalid",
				"branch": "main",
			},
		},
	}
	if got := validateManifestData(valid, settings); len(got) != 0 {
		t.Fatalf("valid manifest errors = %#v", got)
	}

	invalid := map[string]any{
		"version": 1,
		"repositories": []any{
			map[string]any{"name": "repo", "branch": 2},
			"not-a-map",
		},
	}

	errors := validateManifestData(invalid, settings)
	if len(errors) < 4 {
		t.Fatalf("invalid manifest errors = %#v", errors)
	}
}

func TestModuleDocsHookFindingsCoversViolationTypes(t *testing.T) {
	t.Parallel()

	violations := moduleDocsViolations{
		MissingDocstring: []string{"pkg/__init__.py"},
		MissingMarkdown:  []string{"pkg/MODULE.md"},
		MissingRefs: []moduleDocsMissingRefs{{
			PythonFile: "pkg/__init__.py",
			Markdown:   []string{"MODULE.md"},
		}},
		MissingIndex: []string{"pkg/MODULE.md"},
		PathPrefixed: []moduleDocsPathRefs{{
			PythonFile: "pkg/__init__.py",
			Refs:       []string{"pkg/MODULE.md"},
		}},
		NonexistentRefs: []moduleDocsBadRefs{{
			PythonFile: "pkg/conftest.py",
			Refs:       []string{"MISSING.md"},
		}},
		BannedFilenames: []string{"pkg/README.md"},
	}

	findings := moduleDocsHookFindings(violations)
	if len(findings) != moduleDocsFindingCount(violations) {
		t.Fatalf("findings = %#v", findings)
	}

	if got := reportModuleDocsViolations(moduleDocsViolations{}); got != 0 {
		t.Fatalf("empty module docs report = %d, want 0", got)
	}
}

func TestFormatHookReportJSONIncludesNormalizedPayload(t *testing.T) {
	t.Parallel()

	output := formatHookReport(hookReport{
		Tool:  "policy",
		Title: "POLICY FAILED",
		Findings: []hookFinding{{
			File:    "pkg/app.py",
			Line:    7,
			Message: "blocked",
		}},
	}, hookOutputFormatJSON)
	for _, want := range []string{
		`"format": "json"`,
		`"status": "FAIL"`,
		`"file": "pkg/app.py"`,
		`"message": "blocked"`,
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("JSON hook report missing %q:\n%s", want, output)
		}
	}
}

func TestGeminiGitDiffHelpersClassifyChangedAndAddedFiles(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("CODING_ETHOS_REAL_GIT", "")

	sourcePath := filepath.Join(tempDir, "src", "app.py")
	mustWriteTestFile(t, sourcePath, "print('ok')\n")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "git"),
		`#!/usr/bin/env sh
case "$*" in
  "diff --name-only origin/main...HEAD")
    printf 'src/app.py\nREADME.md\n'
    exit 0
    ;;
  "diff --no-ext-diff -U0 origin/main...HEAD -- src/app.py")
    printf '@@ -0,0 +2,3 @@\n+one\n+two\n+three\n'
    exit 0
    ;;
  "diff --no-ext-diff -U0 --staged src/app.py")
    printf '@@ -10 +10,2 @@\n+one\n+two\n'
    exit 0
    ;;
  "status --porcelain "*)
    printf 'A  src/app.py\n'
    exit 0
    ;;
esac
printf 'unexpected git invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	files, err := changedFilesForGeminiFullCheck()
	if err != nil {
		t.Fatalf("changedFilesForGeminiFullCheck() returned error: %v", err)
	}

	if !reflect.DeepEqual(files, []string{"src/app.py", "README.md"}) {
		t.Fatalf("changed files = %#v", files)
	}

	branchLines := changedLinesForGeminiFile("src/app.py", "branch")
	for _, line := range []int{2, 3, 4} {
		if _, ok := branchLines[line]; !ok {
			t.Fatalf("branch changed lines missing %d: %#v", line, branchLines)
		}
	}

	stagedLines := changedLinesForGeminiFile("src/app.py", "staged")
	for _, line := range []int{10, 11} {
		if _, ok := stagedLines[line]; !ok {
			t.Fatalf("staged changed lines missing %d: %#v", line, stagedLines)
		}
	}

	collected := collectGeminiChangedLines([]string{"src/app.py"}, "branch")
	if len(collected["src/app.py"]) != 3 {
		t.Fatalf("collectGeminiChangedLines() = %#v", collected)
	}

	if !isGeminiAddedOrUntracked(context.Background(), "src/app.py") {
		t.Fatal("isGeminiAddedOrUntracked() = false, want true")
	}

	filtered := filterGeminiViolationsByDiff(
		context.Background(),
		[]geminiViolation{
			{File: "src/app.py", Line: 3, Severity: severityCritical},
			{File: "src/app.py", Line: 9, Severity: severityWarning},
			{File: "missing.py", Line: 4, Severity: severityCritical},
			{Message: "global", Severity: severityCritical},
		},
		map[string]map[int]struct{}{"src/app.py": branchLines},
	)
	if len(filtered.InDiff) != 3 || len(filtered.PreExisting) != 1 {
		t.Fatalf("filtered violations = %#v", filtered)
	}

	if got := normalizeGeminiModalAllowlistPattern("./src/app.py"); got != "src/app.py" {
		t.Fatalf("normalizeGeminiModalAllowlistPattern() = %q", got)
	}
}

func TestGeminiOutcomeSummaryAndPreparedCounts(t *testing.T) {
	prepared := []geminiPreparedCheck{
		{
			Plan: GeminiCheckPlan{IncludedFiles: []string{"a.py", "b.py"}},
			Batches: []geminiPreparedBatch{
				{Files: []string{"a.py"}},
				{Files: []string{"b.py"}},
			},
		},
		{
			Plan:    GeminiCheckPlan{IncludedFiles: []string{"c.py"}},
			Batches: []geminiPreparedBatch{{Files: []string{"c.py"}}},
		},
	}
	if got := countGeminiBatches(prepared); got != 3 {
		t.Fatalf("countGeminiBatches() = %d, want 3", got)
	}

	if got := geminiPreparedFiles(
		prepared,
	); !reflect.DeepEqual(
		got,
		[]string{"a.py", "b.py", "c.py"},
	) {
		t.Fatalf("geminiPreparedFiles() = %#v", got)
	}

	outcomes := []geminiCheckOutcome{
		{
			Filtered: geminiFilteredViolations{InDiff: []geminiViolation{{
				File:     "a.py",
				Line:     2,
				Severity: severityCritical,
			}}},
		},
		{BatchErrors: 1},
	}

	hasErrors, hasCriticals, hasAnyInDiff := summarizeGeminiOutcomes(outcomes)
	if !hasErrors || !hasCriticals || !hasAnyInDiff {
		t.Fatalf("summary = %t, %t, %t", hasErrors, hasCriticals, hasAnyInDiff)
	}

	stderr := captureStderr(t, func() {
		if got := geminiOutcomeExitCode(outcomes); got != 1 {
			t.Fatalf("geminiOutcomeExitCode() = %d, want 1", got)
		}
	})
	if !strings.Contains(stderr, "CRITICAL Gemini violations") {
		t.Fatalf("gemini outcome stderr = %q", stderr)
	}
}

func TestIsShellFileDetectsShebang(t *testing.T) {
	t.Parallel()

	script := filepath.Join(t.TempDir(), "tool")
	mustWriteTestFile(t, script, "#!/usr/bin/env bash\necho ok\n")

	if !isShellFile(script) {
		t.Fatalf("%s should be shell file", script)
	}
}

func TestConfigGetReadsOverrideAndDefaults(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteTestFile(t, overridePath, strings.TrimSpace(`
go:
  enabled: true
custom:
  nested:
    value: answer
    list:
      - one
      - two
`)+"\n")
	t.Setenv(configEnv, overridePath)

	if got := configGet(Config{}, []string{"custom.nested.value"}); got != 0 {
		t.Fatalf("configGet(value) = %d, want 0", got)
	}

	if got := configGet(Config{}, []string{"custom.missing", "fallback"}); got != 0 {
		t.Fatalf("configGet(default) = %d, want 0", got)
	}

	if got := configGet(Config{}, []string{"custom.missing"}); got != 1 {
		t.Fatalf("configGet(missing) = %d, want 1", got)
	}

	if got := configGet(Config{}, nil); got != 1 {
		t.Fatalf("configGet(nil) = %d, want 1", got)
	}
}

func TestLoadSettingsFromOverride(t *testing.T) {
	overridePath := filepath.Join(t.TempDir(), "config.yaml")
	mustWriteTestFile(t, overridePath, strings.TrimSpace(`
go:
  module_path: go
  worktree: go
python:
  manifest_validation:
    enabled: true
  plan_completion:
    enabled: true
  module_docs:
    enabled: true
`)+"\n")
	t.Setenv(configEnv, overridePath)

	cfg, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if cfg.LineLimits.PythonHard == 0 {
		t.Fatalf("line limits defaults were not applied: %#v", cfg.LineLimits)
	}

	manifest, err := loadManifestValidationSettings()
	if err != nil || !manifest.Enabled || len(manifest.CandidatePaths) == 0 {
		t.Fatalf("manifest settings = %#v, %v", manifest, err)
	}

	plan, err := loadPlanCompletionSettings()
	if err != nil || !plan.Enabled || plan.MetadataFilename == "" {
		t.Fatalf("plan settings = %#v, %v", plan, err)
	}

	moduleDocs, err := loadModuleDocsSettings()
	if err != nil || !moduleDocs.Enabled || moduleDocs.SourceDocsPath == "" {
		t.Fatalf("module docs settings = %#v, %v", moduleDocs, err)
	}
}

func TestValidateManifestCommandPassesAndFails(t *testing.T) {
	t.Setenv("CODING_ETHOS_SANDBOX_ACTIVE", "0")

	root := t.TempDir()
	t.Chdir(root)
	bundleRoot := filepath.Join(root, "pre-commit")
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "managed-toolchain.tsv"),
		"",
	)
	mustWriteTestFile(t, filepath.Join(bundleRoot, "hooks", "pyproject.toml"), "")

	t.Setenv(precommitRootEnv, bundleRoot)

	overridePath := filepath.Join(root, "config.yaml")
	mustWriteTestFile(t, overridePath, strings.TrimSpace(`
python:
  manifest_validation:
    enabled: true
    candidate_paths:
      - manifest.yaml
`)+"\n")
	t.Setenv(configEnv, overridePath)

	mustWriteTestFile(t, "manifest.yaml", strings.TrimSpace(`
version: "1"
symlinks:
  - source: a
    target: b
`)+"\n")

	if got := validateManifestCommand(Config{}, nil); got != 0 {
		t.Fatalf("validateManifestCommand(valid) = %d, want 0", got)
	}

	mustWriteTestFile(t, "manifest.yaml", "version: 1\nsymlinks:\n  - source: a\n")

	if got := validateManifestCommand(Config{}, nil); got != 1 {
		t.Fatalf("validateManifestCommand(invalid) = %d, want 1", got)
	}
}

func TestResolveGeminiRequestSettingsUsesOverrides(t *testing.T) {
	t.Parallel()

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

func TestParseGeminiTextResponseHandlesSuccessErrorsAndCaching(t *testing.T) {
	t.Parallel()

	cacheDir := t.TempDir()
	settings := geminiRequestSettings{
		Cache: geminiResponseCache{
			Enabled: true,
			Dir:     cacheDir,
			TTL:     time.Minute,
		},
	}
	response := &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body: io.NopCloser(strings.NewReader(`{
			"candidates": [{
				"content": {"parts": [{"text": "one"}, {"text": "two"}]}
			}]
		}`)),
	}

	text, retry, err := parseGeminiTextResponse(response, settings, "success")
	if err != nil {
		t.Fatalf("parseGeminiTextResponse(success) returned error: %v", err)
	}

	if retry {
		t.Fatal("parseGeminiTextResponse(success) marked retryable")
	}

	if text != "onetwo" {
		t.Fatalf("text = %q, want onetwo", text)
	}

	cached, ok, err := readGeminiCache(settings.Cache, "success")
	if err != nil || !ok || cached != "onetwo" {
		t.Fatalf("cache read = %q, %v, %v", cached, ok, err)
	}

	errorResponse := &http.Response{
		StatusCode: http.StatusTooManyRequests,
		Status:     "429 Too Many Requests",
		Body: io.NopCloser(strings.NewReader(`{
			"error": {"message": "quota exhausted", "status": "RESOURCE_EXHAUSTED"}
		}`)),
	}

	_, retry, err = parseGeminiTextResponse(errorResponse, settings, "failure")
	if err == nil || !retry || !strings.Contains(err.Error(), "quota exhausted") {
		t.Fatalf("parseGeminiTextResponse(error) retry=%v err=%v", retry, err)
	}

	_, retry, err = parseGeminiTextResponse(&http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(`{"candidates":[]}`)),
	}, settings, "missing-text")
	if err == nil || retry {
		t.Fatalf("parseGeminiTextResponse(missing text) retry=%v err=%v", retry, err)
	}
}

func TestParseGeminiResultAcceptsFencedJSONAndRejectsInvalidVerdict(t *testing.T) {
	t.Parallel()

	result, err := parseGeminiResult(
		"```json\n{\"verdict\":\"fail\",\"violations\":[{\"file\":\"app.py\",\"line\":3,\"severity\":\"HIGH\",\"message\":\"bad\"}]}\n```",
	)
	if err != nil {
		t.Fatalf("parseGeminiResult(valid) returned error: %v", err)
	}

	if result.Verdict != "fail" || len(result.Violations) != 1 ||
		result.Violations[0].File != "app.py" {
		t.Fatalf("result = %#v", result)
	}

	_, err = parseGeminiResult(`{not-json}`)
	if err == nil {
		t.Fatal("parseGeminiResult(invalid JSON) returned nil error")
	}
}

func TestCandidateFilesForGeminiFiltersByAllSelectedChecks(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)
	mustWriteTestFile(t, "pkg/app.py", "print('ok')\n")
	mustWriteTestFile(t, "scripts/tool", "#!/usr/bin/env bash\necho ok\n")
	mustWriteTestFile(t, "scripts/notes", "deployment notes\n")
	mustWriteTestFile(t, "vendor/skip.py", "print('skip')\n")

	pack := GeminiPromptPack{
		Checks: map[string]GeminiPromptCheckSpec{
			"python": {
				Selector: GeminiFileSelector{
					IncludeExtensions: []string{".py"},
					ExcludePrefixes:   []string{"vendor/"},
				},
			},
			"script": {
				Selector: GeminiFileSelector{
					ShebangMarkers:              []string{"bash"},
					AllowExtensionlessInScripts: true,
				},
			},
		},
	}

	files, scope, err := candidateFilesForGemini(
		GeminiCLIOptions{
			Files: []string{
				"pkg/app.py",
				"scripts/tool",
				"scripts/notes",
				"vendor/skip.py",
			},
		},
		pack,
	)
	if err != nil {
		t.Fatalf("candidateFilesForGemini() returned error: %v", err)
	}

	if scope != geminiScopeStaged {
		t.Fatalf("scope = %q, want staged", scope)
	}

	if !reflect.DeepEqual(files, []string{"pkg/app.py", "scripts/tool"}) {
		t.Fatalf("files = %#v", files)
	}
}

func TestGeminiSourceFilesExcludeNonCode(t *testing.T) {
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	mustWriteTestFile(t, "README.md", "# notes\n")
	mustWriteTestFile(t, "config.yaml", "name: value\n")
	mustWriteTestFile(t, "scripts/notes", "deployment notes\n")
	mustWriteTestFile(t, "scripts/deploy", "#!/usr/bin/env sh\necho ok\n")
	mustWriteTestFile(t, "src/app.py", "print('ok')\n")
	mustWriteTestFile(t, "web/app.ts", "export const ok = true;\n")

	got := geminiSourceFiles([]string{
		"README.md",
		"config.yaml",
		"scripts/notes",
		"scripts/deploy",
		"src/app.py",
		"web/app.ts",
	})
	want := []string{"scripts/deploy", "src/app.py", "web/app.ts"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("geminiSourceFiles() = %#v, want %#v", got, want)
	}
}

func TestBuildGeminiExecutionPlanCopiesPreparedPlans(t *testing.T) {
	t.Parallel()

	plan := buildGeminiExecutionPlan(
		[]geminiPreparedCheck{
			{Plan: GeminiCheckPlan{Name: "code", IncludedFiles: []string{"a.py"}}},
			{Plan: GeminiCheckPlan{Name: "docs", IncludedFiles: []string{"README.md"}}},
		},
		geminiScopeBranch,
		true,
	)
	if !plan.DryRun || plan.Scope != geminiScopeBranch || len(plan.Checks) != 2 ||
		plan.Checks[1].Name != "docs" {
		t.Fatalf("plan = %#v", plan)
	}
}

func TestGeminiOutcomeCollectionFilteringAndReports(t *testing.T) {
	t.Parallel()

	prepared := []geminiPreparedCheck{{
		Plan: GeminiCheckPlan{
			Name:          "code",
			Model:         "gemini-test",
			ServiceTier:   "standard",
			IncludedFiles: []string{"app.py"},
			Batches:       []GeminiBatchPlan{{Files: []string{"app.py"}}},
		},
		Batches: []geminiPreparedBatch{{Files: []string{"app.py"}}},
	}}

	outcomes, jobs := initializeGeminiOutcomesAndJobs(prepared)
	if len(outcomes) != 1 || len(jobs) != 1 || jobs[0].CheckIndex != 0 ||
		jobs[0].BatchIndex != 0 {
		t.Fatalf("outcomes=%#v jobs=%#v", outcomes, jobs)
	}

	results := make(chan geminiBatchJobResult, 1)
	results <- geminiBatchJobResult{
		CheckIndex: 0,
		BatchIndex: 0,
		Outcome: geminiBatchOutcome{
			Files: []string{"app.py"},
			Result: geminiResult{
				Verdict: "fail",
				Violations: []geminiViolation{
					{
						Severity:     severityCritical,
						File:         "app.py",
						Line:         2,
						Message:      "critical issue",
						EthosSection: "solid-is-law",
					},
					{
						Severity: severityWarning,
						File:     "app.py",
						Line:     9,
						Message:  "old issue",
					},
				},
			},
		},
	}

	close(results)

	collectGeminiBatchResults(outcomes, results)
	finalizeGeminiOutcomes(context.Background(), outcomes, map[string]map[int]struct{}{
		"app.py": {2: {}},
	})

	if outcomes[0].BatchesCompleted != 1 ||
		len(outcomes[0].Filtered.InDiff) != 1 ||
		len(outcomes[0].Filtered.PreExisting) != 1 ||
		geminiOutcomeStatus(outcomes[0]) != statusFail {
		t.Fatalf("outcome = %#v", outcomes[0])
	}

	for _, format := range []string{hookOutputFormatHuman, hookOutputFormatJSON, hookOutputFormatTOON} {
		report := formatGeminiReport("staged", outcomes, format)
		if !strings.Contains(report, "critical issue") ||
			!strings.Contains(report, "app.py") {
			t.Fatalf("formatGeminiReport(%s) = %q", format, report)
		}
	}

	errors := geminiReportBatchErrors([]geminiOutcomeReport{{
		Name: "code",
		BatchErrors: []geminiBatchError{
			{Batch: 1, Files: []string{"app.py"}, Error: "boom"},
		},
	}})
	if len(errors) != 1 || errors[0].Check != "code" {
		t.Fatalf("batch errors = %#v", errors)
	}

	if formatSeverityIcon(severityCritical) != "XX" ||
		formatSeverityIcon(severityWarning) != "W " ||
		formatSeverityIcon("INFO") != "--" {
		t.Fatal("unexpected severity icon formatting")
	}
}

func TestGeminiBatchRequestInputsUseExplicitCacheBinding(t *testing.T) {
	t.Parallel()

	job := geminiBatchJob{
		Batch: geminiPreparedBatch{
			Prompt:         "raw prompt",
			CachedPrompt:   "cached prompt",
			ExplicitAPIKey: "corpus",
		},
	}

	prompt, dependency, cachedContent := geminiBatchRequestInputs(
		job,
		map[string]string{"corpus": "cachedContents/1"},
	)
	if prompt != "cached prompt" || dependency != "corpus" ||
		cachedContent != "cachedContents/1" {
		t.Fatalf("cached inputs = %q %q %q", prompt, dependency, cachedContent)
	}

	prompt, dependency, cachedContent = geminiBatchRequestInputs(job, nil)
	if prompt != "raw prompt" || dependency != "" || cachedContent != "" {
		t.Fatalf("raw inputs = %q %q %q", prompt, dependency, cachedContent)
	}
}

func TestExecuteGeminiChecksUsesHTTPClientAndFiltersResults(t *testing.T) {
	client := &http.Client{
		Timeout: time.Second,
		Transport: fakeRoundTripper(func(request *http.Request) *http.Response {
			if request.URL.Path != "/v1beta/models/gemini-test:generateContent" {
				return testHTTPResponse(
					http.StatusNotFound,
					`{"error":{"message":"not found"}}`,
				)
			}

			return testHTTPResponse(http.StatusOK, `{
				"candidates": [{
					"content": {"parts": [{"text": "{\"verdict\":\"fail\",\"violations\":[{\"severity\":\"CRITICAL\",\"file\":\"app.py\",\"line\":2,\"message\":\"broken\"}]}"}]}
				}]
			}`)
		}),
	}

	prepared := []geminiPreparedCheck{{
		Plan: GeminiCheckPlan{
			Name:          "code",
			Model:         "gemini-test",
			ServiceTier:   "standard",
			IncludedFiles: []string{"app.py"},
			Batches:       []GeminiBatchPlan{{Files: []string{"app.py"}}},
		},
		Request: geminiRequestSettings{
			CheckName:            "code",
			Model:                "gemini-test",
			ServiceTier:          "standard",
			Cache:                geminiResponseCache{Enabled: false},
			DisableSafetyFilters: true,
		},
		Batches: []geminiPreparedBatch{{
			Prompt: "review app.py",
			Files:  []string{"app.py"},
		}},
	}}

	outcomes := executeGeminiChecksWithClient(
		context.Background(),
		GeminiSettings{TimeoutSeconds: 5, MaxConcurrentAPICalls: 2},
		"test-key",
		prepared,
		map[string]map[int]struct{}{"app.py": {2: {}}},
		client,
	)
	if len(outcomes) != 1 ||
		outcomes[0].BatchesCompleted != 1 ||
		len(outcomes[0].Filtered.InDiff) != 1 ||
		outcomes[0].Filtered.InDiff[0].Message != "broken" {
		t.Fatalf("outcomes = %#v", outcomes)
	}
}

type fakeRoundTripper func(*http.Request) *http.Response

func (transport fakeRoundTripper) RoundTrip(
	request *http.Request,
) (*http.Response, error) {
	return transport(request), nil
}

func testHTTPResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestRunGeminiCheckDryRunBuildsPlanFromGeneratedPromptPack(t *testing.T) {
	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	bundleRoot := writeTestBundleRoot(t, tempDir)
	mustWriteTestFile(t, "pkg/app.py", "print('ok')\n")

	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(consumerRootEnv, tempDir)
	overridePath := filepath.Join(tempDir, "repo_config.yaml")
	mustWriteTestFile(
		t,
		overridePath,
		strings.TrimSpace(`
gemini:
  enabled: true
  model: gemini-test
  service_tier: standard
  cache:
    enabled: false
    dirname: gemini-cache
`)+"\n",
	)
	t.Setenv(configEnv, overridePath)

	output := captureStdout(t, func() {
		if got := runGeminiCheck(
			Config{},
			[]string{"--dry-run", "--check-type", "code_ethos", "pkg/app.py"},
		); got != 0 {
			t.Fatalf("runGeminiCheck(dry-run) = %d, want 0", got)
		}
	})
	if !strings.Contains(output, `"dry_run": true`) ||
		!strings.Contains(output, `"pkg/app.py"`) ||
		!strings.Contains(output, `"code_ethos"`) {
		t.Fatalf("dry-run output = %s", output)
	}
}

func TestLoadGeminiSettingsUsesConsumerRuntimeCacheDir(t *testing.T) {
	consumerRoot := t.TempDir()
	bundleRoot := filepath.Join(consumerRoot, "coding-ethos", "pre-commit")

	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "managed-toolchain.tsv"),
		"",
	)
	mustWriteTestFile(t, filepath.Join(bundleRoot, "hooks", "pyproject.toml"), "")
	mustWriteTestFile(
		t,
		filepath.Join(consumerRoot, ".git"),
		"gitdir: /tmp/unrelated-real-git-dir\n",
	)
	mustWriteTestFile(
		t,
		filepath.Join(consumerRoot, "coding-ethos", "config.yaml"),
		strings.TrimSpace(`
gemini:
  model: gemini-2.5-flash
  cache:
    dirname: gemini-response-cache
`)+"\n",
	)

	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(consumerRootEnv, consumerRoot)

	_, paths, err := loadGeminiSettings()
	if err != nil {
		t.Fatalf("loadGeminiSettings() returned error: %v", err)
	}

	want := filepath.Join(
		consumerRoot,
		".code-ethos",
		"cache",
		"gemini-response-cache",
	)
	if paths.CacheDir != want {
		t.Fatalf("CacheDir = %q, want %q", paths.CacheDir, want)
	}

	if strings.Contains(paths.CacheDir, string(filepath.Separator)+".git") {
		t.Fatalf("CacheDir writes inside .git: %q", paths.CacheDir)
	}
}

func TestGeminiSafetySettingsDisabledUsesOffThresholds(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	template := "Review these files.\n\n{code_content}\n"

	prompt := geminiPromptForExplicitCachedContent(template)
	if strings.Contains(prompt, "{code_content}") {
		t.Fatalf("prompt still contains placeholder: %q", prompt)
	}

	if !strings.Contains(prompt, "cached content") {
		t.Fatalf("prompt does not mention cached content: %q", prompt)
	}
}

func TestGeminiPromptAndCacheHelpers(t *testing.T) {
	t.Parallel()

	if got := geminiPromptWithInlineContent(
		"Review\n{code_content}",
		"print('ok')",
	); !strings.Contains(
		got,
		"print('ok')",
	) {
		t.Fatalf("inline prompt = %q", got)
	}

	if got := geminiPromptWithInlineContent(
		"Review",
		"print('ok')",
	); !strings.Contains(
		got,
		"\n\nprint('ok')",
	) {
		t.Fatalf("appended prompt = %q", got)
	}

	if got := geminiPromptWithInlineContent("Review", "  "); got != "Review" {
		t.Fatalf("empty content prompt = %q", got)
	}

	if got := geminiDurationLiteral(0); got != "3600s" {
		t.Fatalf("default duration = %q", got)
	}

	if got := geminiDurationLiteral(90 * time.Second); got != "90s" {
		t.Fatalf("duration = %q", got)
	}
}

func TestGeminiAPIKeyAndExplicitCacheRoundTrip(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "  key-123  ")

	if got := geminiAPIKey(); got != "key-123" {
		t.Fatalf("geminiAPIKey() = %q", got)
	}

	cache := geminiResponseCache{
		APIEnabled: true,
		Dir:        t.TempDir(),
	}
	key := geminiExplicitContentKey("gemini-test", "content")
	expires := time.Now().UTC().Add(time.Hour)

	err := writeGeminiExplicitCache(cache, key, "cachedContents/abc", expires)
	if err != nil {
		t.Fatalf("write explicit cache: %v", err)
	}

	name, ok, err := readGeminiExplicitCache(cache, key)
	if err != nil {
		t.Fatalf("read explicit cache: %v", err)
	}

	if !ok || name != "cachedContents/abc" {
		t.Fatalf("explicit cache = %q, %v", name, ok)
	}

	expiredKey := key + "-expired"

	err = writeGeminiExplicitCache(
		cache,
		expiredKey,
		"cachedContents/old",
		time.Now().UTC().Add(-time.Hour),
	)
	if err != nil {
		t.Fatalf("write expired explicit cache: %v", err)
	}

	name, ok, err = readGeminiExplicitCache(cache, expiredKey)
	if err != nil || ok || name != "" {
		t.Fatalf("expired explicit cache = %q, %v, %v", name, ok, err)
	}
}

func TestCreateAndEnsureGeminiExplicitCacheUseAPIAndPersistResult(t *testing.T) {
	t.Parallel()

	requests := 0
	client := &http.Client{
		Transport: fakeRoundTripper(func(request *http.Request) *http.Response {
			requests++

			if request.URL.Path != "/v1beta/cachedContents" {
				return testHTTPResponse(
					http.StatusNotFound,
					`{"error":{"message":"not found"}}`,
				)
			}

			if request.Header.Get("X-Goog-Api-Key") != "test-key" {
				return testHTTPResponse(
					http.StatusForbidden,
					`{"error":{"message":"bad key"}}`,
				)
			}

			return testHTTPResponse(http.StatusOK, `{
			"name": "`+geminiGeneratedCacheName+`",
			"expireTime": "2027-01-01T00:00:00Z"
		}`)
		}),
	}

	created, err := createGeminiExplicitCache(
		context.Background(),
		client,
		"test-key",
		"gemini-test",
		"source corpus",
		time.Hour,
		"coding-ethos-test",
	)
	if err != nil {
		t.Fatalf("createGeminiExplicitCache() returned error: %v", err)
	}

	if created.Name != geminiGeneratedCacheName {
		t.Fatalf("created cache = %#v", created)
	}

	cache := geminiResponseCache{
		APIEnabled: true,
		Dir:        t.TempDir(),
		APITTL:     time.Hour,
	}
	key := geminiExplicitContentKey("gemini-test", "source corpus")

	name, ok := ensureGeminiExplicitCache(
		context.Background(),
		client,
		"test-key",
		geminiExplicitCacheSeed{
			Cache:   cache,
			Model:   "gemini-test",
			Content: "source corpus",
		},
		key,
	)
	if !ok || name != geminiGeneratedCacheName {
		t.Fatalf("ensure cache = %q, %v", name, ok)
	}

	name, ok = ensureGeminiExplicitCache(
		context.Background(),
		client,
		"test-key",
		geminiExplicitCacheSeed{
			Cache:   cache,
			Model:   "gemini-test",
			Content: "source corpus",
		},
		key,
	)
	if !ok || name != geminiGeneratedCacheName {
		t.Fatalf("ensure cached hit = %q, %v", name, ok)
	}

	if requests != 2 {
		t.Fatalf("requests = %d, want create + first ensure only", requests)
	}
}

func TestCreateGeminiExplicitCacheReportsAPIAndShapeErrors(t *testing.T) {
	t.Parallel()

	errorClient := &http.Client{
		Transport: fakeRoundTripper(func(_ *http.Request) *http.Response {
			return testHTTPResponse(
				http.StatusBadRequest,
				`{"error":{"message":"bad request","status":"INVALID_ARGUMENT"}}`,
			)
		}),
	}

	_, err := createGeminiExplicitCache(
		context.Background(),
		errorClient,
		"key",
		"model",
		"content",
		time.Hour,
		"display",
	)
	if err == nil || !strings.Contains(err.Error(), "bad request") {
		t.Fatalf("API error = %v", err)
	}

	noNameClient := &http.Client{
		Transport: fakeRoundTripper(func(_ *http.Request) *http.Response {
			return testHTTPResponse(
				http.StatusOK,
				`{"expireTime":"2026-01-01T00:00:00Z"}`,
			)
		}),
	}

	_, err = createGeminiExplicitCache(
		context.Background(),
		noNameClient,
		"key",
		"model",
		"content",
		time.Hour,
		"display",
	)
	if !errors.Is(err, errGeminiCreateNoName) {
		t.Fatalf("missing-name error = %v", err)
	}
}

func TestGenerateGeminiTextRetriesAndWritesCache(t *testing.T) {
	t.Parallel()

	requests := 0
	client := &http.Client{
		Transport: fakeRoundTripper(func(_ *http.Request) *http.Response {
			requests++
			if requests == 1 {
				return testHTTPResponse(
					http.StatusServiceUnavailable,
					`{"error":{"message":"try again"}}`,
				)
			}

			return testHTTPResponse(http.StatusOK, `{
			"candidates": [{
				"content": {"parts": [{"text": "{\"verdict\":\"pass\"}"}]}
			}]
		}`)
		}),
	}
	settings := geminiRequestSettings{
		Model:                 "gemini-test",
		ServiceTier:           geminiServiceTierNormal,
		MaxRetries:            1,
		InitialBackoffSeconds: 0,
		Cache: geminiResponseCache{
			Enabled: true,
			Dir:     t.TempDir(),
			TTL:     time.Hour,
		},
	}

	text, err := generateGeminiText(
		context.Background(),
		client,
		settings,
		"key",
		"prompt",
		"dependency",
		"",
	)
	if err != nil {
		t.Fatalf("generateGeminiText() returned error: %v", err)
	}

	if text != `{"verdict":"pass"}` || requests != 2 {
		t.Fatalf("text=%q requests=%d", text, requests)
	}

	text, err = generateGeminiText(
		context.Background(),
		client,
		settings,
		"key",
		"prompt",
		"dependency",
		"",
	)
	if err != nil {
		t.Fatalf("generateGeminiText(cache hit) returned error: %v", err)
	}

	if text != `{"verdict":"pass"}` || requests != 2 {
		t.Fatalf("cache hit text=%q requests=%d", text, requests)
	}
}

func TestMatchesGeminiSelector(t *testing.T) {
	tempDir := t.TempDir()

	t.Chdir(tempDir)

	mustWriteTestFile(t, "pkg/module.py", "print('ok')\n")
	mustWriteTestFile(t, "scripts/tool", "#!/usr/bin/env bash\necho ok\n")
	mustWriteTestFile(t, "scripts/notes", "deployment notes\n")
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
		{path: "scripts/notes", want: false},
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

	t.Chdir(tempDir)

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
	t.Parallel()

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

func TestParseGeminiResultAcceptsViolationArray(t *testing.T) {
	t.Parallel()

	result, err := parseGeminiResult(`[
		{
			"severity": "warning",
			"file": "./pkg/app.py",
			"line": -1,
			"ethosSection": "Section 18",
			"message": "  Add documentation.  "
		}
	]`)
	if err != nil {
		t.Fatalf("parseGeminiResult() returned error: %v", err)
	}

	if result.Verdict != passVerdict {
		t.Fatalf("Verdict = %q, want %q", result.Verdict, passVerdict)
	}

	if len(result.Violations) != 1 {
		t.Fatalf("violation count = %d, want 1", len(result.Violations))
	}

	violation := result.Violations[0]
	if violation.Severity != severityWarning ||
		violation.File != "pkg/app.py" ||
		violation.Line != 0 ||
		violation.Message != "Add documentation." {
		t.Fatalf("violation was not normalized: %#v", violation)
	}
}

func TestFormatGeminiReportTOON(t *testing.T) {
	t.Parallel()

	report := formatGeminiReport(
		"staged",
		[]geminiCheckOutcome{
			{
				Plan: GeminiCheckPlan{
					Name:          "code_ethos",
					Model:         "gemini-2.5-flash",
					ServiceTier:   "standard",
					IncludedFiles: []string{"pkg/app.py"},
					Batches: []GeminiBatchPlan{
						{Files: []string{"pkg/app.py"}},
					},
				},
				Filtered: geminiFilteredViolations{
					InDiff: []geminiViolation{
						{
							Severity:     "CRITICAL",
							File:         "pkg/app.py",
							Line:         12,
							EthosSection: "Section 19",
							Message:      "Do not introduce modal behavior",
						},
					},
				},
			},
		},
		hookOutputFormatTOON,
	)
	for _, fragment := range []string{
		"format: toon",
		"tool: gemini",
		"scope: staged",
		"status: FAIL",
		"outcomes[1]{name,status,model,service_tier,included_files,batches}:",
		"code_ethos,FAIL,gemini-2.5-flash,standard,1,1",
		"violations[1]{scope,severity,file,line,ethos_section,message}:",
		"in_diff,CRITICAL,pkg/app.py,12,Section 19,Do not introduce modal behavior",
	} {
		if !strings.Contains(report, fragment) {
			t.Fatalf("Gemini TOON report missing %q:\n%s", fragment, report)
		}
	}
}

func TestFormatGeminiReportJSON(t *testing.T) {
	t.Parallel()

	report := formatGeminiReport(
		"staged",
		[]geminiCheckOutcome{
			{
				Plan: GeminiCheckPlan{
					Name:          "code_ethos",
					Model:         "gemini-2.5-flash",
					ServiceTier:   "standard",
					IncludedFiles: []string{"pkg/app.py"},
					Batches: []GeminiBatchPlan{
						{Files: []string{"pkg/app.py"}},
					},
				},
				Filtered: geminiFilteredViolations{
					InDiff: []geminiViolation{
						{
							Severity: "WARNING",
							File:     "pkg/app.py",
							Line:     7,
							Message:  "Clarify the branch",
						},
					},
				},
			},
		},
		hookOutputFormatJSON,
	)

	var summary geminiReportSummary

	err := json.Unmarshal([]byte(report), &summary)
	if err != nil {
		t.Fatalf("Gemini JSON report did not decode: %v\n%s", err, report)
	}

	if summary.Format != hookOutputFormatJSON || summary.Scope != "staged" ||
		summary.Status != statusWarn || len(summary.Outcomes) != 1 {
		t.Fatalf("unexpected Gemini JSON summary: %#v", summary)
	}
}

func TestShellAndYamlCommandsExecuteExternalToolsAndReportFindings(t *testing.T) {
	nativeSandboxAvailable := nativeSandboxRuntimeAvailable()

	tempDir := setupGitHookTestRepo(t)
	t.Chdir(tempDir)
	t.Setenv(consumerRootEnv, tempDir)

	bundleRoot := writeTestBundleRoot(t, tempDir)

	t.Setenv(precommitRootEnv, bundleRoot)
	t.Setenv(hookOutputFormatEnv, hookOutputFormatTOON)
	mustWriteTestFile(t, "scripts/run.sh", "#!/usr/bin/env sh\necho $NAME\n")
	mustWriteTestFile(t, "config.yaml", "bad: [\n")

	fakeBin := filepath.Join(tempDir, "bin")
	mustWriteExecutable(
		t,
		filepath.Join(
			tempDir,
			"code-ethos",
			"build",
			"toolchain",
			"github-bin",
			"shellcheck",
		),
		`#!/usr/bin/env sh
printf '{"comments":[{"file":"scripts/run.sh","line":2,"column":6,"level":"warning","code":2086,"message":"Double quote to prevent globbing and word splitting."}]}'
exit 1
`,
	)
	mustWriteExecutable(
		t,
		filepath.Join(tempDir, "code-ethos", "build", "toolchain", "go-bin", "shfmt"),
		"#!/usr/bin/env sh\nprintf -- '--- scripts/run.sh.orig\\n+++ scripts/run.sh\\n@@ -1 +1 @@\\n'\nexit 1\n",
	)
	mustWriteExecutable(
		t,
		filepath.Join(fakeBin, "uv"),
		`#!/usr/bin/env sh
case "$*" in
  *" yamllint "*)
    printf 'config.yaml:2:5: [error] wrong indentation (indentation)\n'
    exit 1
    ;;
esac
printf 'unexpected uv invocation: %s\n' "$*" >&2
exit 2
`,
	)
	t.Setenv("PATH", fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout := captureStdout(t, func() {
		got := runShellcheck(Config{}, []string{"scripts/run.sh"})
		if !nativeSandboxAvailable && got != 1 {
			return
		}

		if got != 1 {
			t.Fatalf("runShellcheck() = %d, want 1", got)
		}

		got = runShfmt(Config{}, []string{"scripts/run.sh"})
		if got != 1 {
			t.Fatalf("runShfmt() = %d, want 1", got)
		}

		got = runYamllint(Config{}, []string{"config.yaml"})
		if got != 1 {
			t.Fatalf("runYamllint() = %d, want 1", got)
		}
	})
	if !nativeSandboxAvailable {
		if !strings.Contains(stdout, "runtime.sandbox_denial") {
			t.Fatalf("nested sandbox output missing denial:\n%s", stdout)
		}

		return
	}

	for _, want := range []string{
		"tool: shellcheck",
		"tool: shfmt",
		"tool: yamllint",
		"shfmt,scripts/run.sh,1,1,error,format",
		"scripts/run.sh",
		"config.yaml",
	} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("shell/yaml output missing %q:\n%s", want, stdout)
		}
	}
}

func TestCanonicalHookGroupsExposeExpectedGroups(t *testing.T) {
	t.Parallel()

	groups := canonicalHookGroups()
	for _, name := range []string{
		"syntax",
		"python-policy",
		"python-static",
		"docs",
		"security",
		"shell",
		"ai",
	} {
		group, ok := groups[name]
		if !ok {
			t.Fatalf("canonicalHookGroups() missing %q", name)
		}

		if len(group.Commands) == 0 {
			t.Fatalf("canonicalHookGroups()[%q] has no commands", name)
		}
	}
}

func TestDefaultHookCommandRegistryHasRunnableGroups(t *testing.T) {
	t.Parallel()

	registry := defaultHookCommandRegistry()
	if len(registry.Commands) == 0 {
		t.Fatal("registry has no commands")
	}

	for groupName, group := range registry.Groups {
		if len(group.Commands) == 0 {
			t.Fatalf("group %q has no commands", groupName)
		}

		for _, command := range group.Commands {
			if command.Name == "" || command.Run == nil {
				t.Fatalf("group %q has invalid command %#v", groupName, command)
			}
		}
	}
}

func TestParseGeminiChangedLines(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
		context.Background(),
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func mustWriteTestFile(t *testing.T, path, contents string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("os.MkdirAll(%q) failed: %v", path, err)
	}

	err = os.WriteFile(path, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", path, err)
	}
}

func mustWriteExecutable(t *testing.T, path, contents string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("os.MkdirAll(%q) failed: %v", filepath.Dir(path), err)
	}

	err = os.WriteFile(path, []byte(contents), 0o600)
	if err != nil {
		t.Fatalf("os.WriteFile(%q) failed: %v", path, err)
	}

	err = os.Chmod(path, 0o700)
	if err != nil {
		t.Fatalf("os.Chmod(%q) failed: %v", path, err)
	}
}

func buildTestSandboxHelper(t *testing.T, output string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(output), 0o755)
	if err != nil {
		t.Fatalf("os.MkdirAll(%q) failed: %v", filepath.Dir(output), err)
	}

	command := exec.Command(
		filepath.Join(runtime.GOROOT(), "bin", "go"),
		"build",
		"-buildvcs=false",
		"-o",
		output,
		"./cmd/coding-ethos-sandbox",
	)
	command.Dir = testGoModuleRoot(t)

	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("build sandbox helper: %v\n%s", err, output)
	}
}

func testGoModuleRoot(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()

	return captureStandardStream(t, "stderr", &os.Stderr, fn)
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	return captureStandardStream(t, "stdout", &os.Stdout, fn)
}

func captureStandardStream(
	t *testing.T,
	streamName string,
	stream **os.File,
	fn func(),
) string {
	t.Helper()

	unlock := lockStandardStreamCapture(t, streamName)
	defer unlock()

	original := *stream

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}

	*stream = writer

	t.Cleanup(func() {
		*stream = original
	})

	fn()

	err = writer.Close()
	if err != nil {
		t.Fatalf("writer.Close() failed: %v", err)
	}

	*stream = original

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

func lockStandardStreamCapture(t *testing.T, streamName string) func() {
	t.Helper()

	lockPath := filepath.Join(os.TempDir(), "coding-ethos-test-"+streamName+".lock")

	lockFD, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open %s capture lock: %v", streamName, err)
	}

	err = syscall.Flock(lockFD, syscall.LOCK_EX)
	if err != nil {
		_ = syscall.Close(lockFD)

		t.Fatalf("lock %s capture: %v", streamName, err)
	}

	return func() {
		unlockErr := syscall.Flock(lockFD, syscall.LOCK_UN)
		closeErr := syscall.Close(lockFD)

		if unlockErr != nil {
			t.Fatalf("unlock %s capture: %v", streamName, unlockErr)
		}

		if closeErr != nil {
			t.Fatalf("close %s capture lock: %v", streamName, closeErr)
		}
	}
}

func slicesContains(values []string, target string) bool {
	return slices.Contains(values, target)
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() failed: %v", err)
	}

	return string(data)
}

func writeTestBundleRoot(t *testing.T, root string) string {
	t.Helper()

	ethosRoot := filepath.Join(root, "code-ethos")
	bundleRoot := filepath.Join(root, "code-ethos", "pre-commit")
	mustWriteTestFile(t, filepath.Join(ethosRoot, "config.yaml"), "version: 1\n")
	buildTestSandboxHelper(t, filepath.Join(ethosRoot, "bin", "coding-ethos-sandbox"))
	mustWriteTestFile(
		t,
		filepath.Join(root, "code-ethos", "build", "policy", "policy-bundle.json"),
		`{"version":1,"policies":{},"skills":{},"evidence_maps":[]}`,
	)
	mustWriteTestFile(
		t,
		filepath.Join(root, "code-ethos", ".code-ethos", "gemini", "prompt-pack.json"),
		`{
  "version": 1,
  "checks": {
    "code_ethos": {
      "fileScope": "code",
      "selector": {
        "includeExtensions": [".py"],
        "excludeSubstrings": [],
        "excludePrefixes": [],
        "shebangMarkers": [],
        "allowExtensionlessInScripts": false
      },
      "batchSize": 3,
      "maxFileSizeKb": 50
    }
  },
  "prompts": {
    "code_ethos": "Review the selected files."
  }
}
`,
	)
	mustWriteTestFile(
		t,
		filepath.Join(bundleRoot, "hooks", "managed-toolchain.tsv"),
		"",
	)
	mustWriteTestFile(t, filepath.Join(bundleRoot, "hooks", "pyproject.toml"), "")

	_, err := toolconfigs.Sync(ethosRoot, root, "")
	if err != nil {
		t.Fatalf("sync generated tool configs: %v", err)
	}

	return bundleRoot
}

func nativeSandboxRuntimeAvailable() bool {
	if os.Getenv("CODING_ETHOS_AGENT_SHELL_SANDBOX") == "1" {
		return false
	}

	_, err := sandbox.ValidateNativeRuntime()

	return err == nil
}
