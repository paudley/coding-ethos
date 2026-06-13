// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDecisionImportRelativeInputNormalizesRepoRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	if got := decisionImportRelativeInput(root, root); got != "." {
		t.Fatalf("absolute root relative input = %q, want .", got)
	}
	if got := decisionImportRelativeInput(root, "."); got != "." {
		t.Fatalf("dot relative input = %q, want .", got)
	}
	if got := decisionImportRelativeInput(
		root,
		filepath.Join(root, "docs", "decisions"),
	); got != "docs/decisions" {
		t.Fatalf("absolute child relative input = %q", got)
	}

	if got := decisionImportRelativeInput(
		filepath.Join(root, "nested"),
		"../nested/docs/decisions",
	); got != "docs/decisions" {
		t.Fatalf("root-resolved relative input = %q", got)
	}
}

func TestDecisionImportScopeClassifiesFilesAndDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	scope, ok := decisionImportScope(root, "docs/decisions/startup.md")
	if !ok {
		t.Fatal("decisionImportScope rejected markdown file")
	}
	if !scope.exact || scope.relative != "docs/decisions/startup.md" {
		t.Fatalf("file scope = %#v", scope)
	}

	scope, ok = decisionImportScope(root, filepath.Join(root, "docs", "decisions"))
	if !ok {
		t.Fatal("decisionImportScope rejected absolute directory")
	}
	if scope.exact || scope.relative != "docs/decisions" {
		t.Fatalf("directory scope = %#v", scope)
	}
}

func TestDecisionPathInPruneScopesMatchesRootDirectoryAndExactFile(t *testing.T) {
	t.Parallel()

	scopes := []decisionImportPruneScope{
		{relative: "docs/decisions/startup.md", exact: true},
		{relative: ".coding-ethos/decisions"},
	}

	for _, path := range []string{
		"docs/decisions/startup.md",
		".coding-ethos/decisions/startup.md",
	} {
		if !decisionPathInPruneScopes(path, scopes) {
			t.Fatalf("decisionPathInPruneScopes(%q) = false, want true", path)
		}
	}

	if decisionPathInPruneScopes("docs/decisions/other.md", scopes) {
		t.Fatal("exact markdown scope matched sibling file")
	}
}

func TestDecisionImporterSkipRules(t *testing.T) {
	t.Parallel()

	importer := decisionImporter{
		root: t.TempDir(),
		gate: gitIgnoreMatcher{},
		config: IndexOptions{
			ExcludePatterns: []string{"docs/generated/**"},
		},
	}

	ctx := context.Background()
	if importer.skipsRelativeDir(ctx, filepath.Join(importer.root, "pkg"), "pkg") {
		t.Fatal("plain package directory was skipped")
	}
	if !importer.skipsRelativeDir(
		ctx,
		filepath.Join(importer.root, "pkg", "node_modules"),
		"pkg/node_modules",
	) {
		t.Fatal("nested node_modules directory was not skipped")
	}
	if !importer.skipsRelativeDir(
		ctx,
		filepath.Join(importer.root, "docs", "generated"),
		"docs/generated",
	) {
		t.Fatal("configured generated directory was not skipped")
	}
	if importer.skipsRelativeDir(
		ctx,
		filepath.Join(importer.root, ".coding-ethos", "decisions"),
		".coding-ethos/decisions",
	) {
		t.Fatal("default coding-ethos decision directory was skipped")
	}
}

func TestFirstMarkdownHeadingFallback(t *testing.T) {
	t.Parallel()

	got := firstMarkdownHeading([]byte("intro\n# Chosen heading\n## ignored\n"))
	if got != "Chosen heading" {
		t.Fatalf("firstMarkdownHeading() = %q", got)
	}
}

func TestSplitYAMLFrontMatterScansWithoutTrailingNewline(t *testing.T) {
	t.Parallel()

	frontMatter, body, found := splitYAMLFrontMatter(
		[]byte("---\r\ntitle: Example\r\n---\r\nbody"),
	)
	if !found {
		t.Fatal("splitYAMLFrontMatter did not find front matter")
	}
	if string(frontMatter) != "title: Example\r\n" || string(body) != "body" {
		t.Fatalf("front matter = %q body = %q", frontMatter, body)
	}

	_, _, found = splitYAMLFrontMatter([]byte("---\ntitle: Example"))
	if found {
		t.Fatal("unterminated front matter was accepted")
	}
}
