// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestASTIndexerSkipsUnchangedFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "app.py")
	content := []byte("def hello():\n    print('hello')\n")

	err := os.WriteFile(filePath, content, 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("first index: %v", err)
	}

	if summary.FilesIndexed != 1 {
		t.Fatalf("expected 1 file indexed, got %d", summary.FilesIndexed)
	}

	if len(summary.Skipped) != 0 {
		t.Fatalf("expected 0 skipped, got %d", len(summary.Skipped))
	}

	// Re-index without changes
	summary2, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("second index: %v", err)
	}

	if summary2.FilesIndexed != 0 {
		t.Fatalf("expected 0 files indexed on re-index, got %d", summary2.FilesIndexed)
	}

	if len(summary2.Skipped) != 1 || summary2.Skipped[0] != "app.py" {
		t.Fatalf("expected 1 skipped (app.py), got %v", summary2.Skipped)
	}
}

func TestASTIndexerSkipsFreshOversizedFileBeforeReading(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "app.py")

	err := os.WriteFile(filePath, bytes.Repeat([]byte("x"), 1024*1024+1), 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("first index: %v", err)
	}

	err = os.Chmod(filePath, 0)
	if err != nil {
		t.Fatalf("chmod unreadable: %v", err)
	}

	t.Cleanup(func() {
		chmodErr := os.Chmod(filePath, 0o600)
		if chmodErr != nil {
			t.Fatalf("restore readable file mode: %v", chmodErr)
		}
	})

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("second index should skip oversized file without reading: %v", err)
	}

	if summary.FilesIndexed != 0 ||
		len(summary.Skipped) != 1 ||
		summary.Skipped[0] != "app.py" {
		t.Fatalf("summary = %#v, want app.py skipped without reindex", summary)
	}
}

func TestASTIndexerReindexesModifiedFiles(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "app.py")
	content := []byte("def hello():\n    print('hello')\n")

	err := os.WriteFile(filePath, content, 0o600)
	if err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err = indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Modify file and re-index
	content2 := []byte("def hello():\n    print('hello world')\n")

	err = os.WriteFile(filePath, content2, 0o600)
	if err != nil {
		t.Fatalf("write modified file: %v", err)
	}

	summary3, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("third index: %v", err)
	}

	if summary3.FilesIndexed != 1 {
		t.Fatalf("expected 1 file indexed after modification, got %d", summary3.FilesIndexed)
	}

	if len(summary3.Skipped) != 0 {
		t.Fatalf("expected 0 skipped after modification, got %d", len(summary3.Skipped))
	}
}

func TestASTIndexerIndexesJavaScriptAndTypeScript(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()
	webDir := filepath.Join(tempDir, "web")

	err := os.MkdirAll(webDir, 0o700)
	if err != nil {
		t.Fatalf("mkdir web: %v", err)
	}

	files := map[string]string{
		"web/app.js": "import tool from 'pkg';\nexport function run() { return tool(); }\n",
		"web/app.ts": "import tool from 'pkg';\n" +
			"export function runTyped(): string { return tool(); }\n",
	}

	for path, content := range files {
		err = os.WriteFile(filepath.Join(tempDir, path), []byte(content), 0o600)
		if err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{"web"})
	if err != nil {
		t.Fatalf("index web: %v", err)
	}

	if summary.FilesIndexed != 2 {
		t.Fatalf("indexed %d files, want 2", summary.FilesIndexed)
	}

	for _, test := range []struct {
		path     string
		language string
	}{
		{path: "web/app.js", language: "javascript"},
		{path: "web/app.ts", language: "typescript"},
	} {
		file, found, err := store.GetCodeFile(ctx, test.path)
		if err != nil {
			t.Fatalf("get %s: %v", test.path, err)
		}

		if !found || file.Language != test.language || file.SourceModTimeUTC == "" {
			t.Fatalf("code file %s = %#v, found=%v", test.path, file, found)
		}
	}
}

func TestASTIndexerSkipsNestedCodingEthosCheckout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()

	files := map[string]string{
		"app/main.go":                 "package main\nfunc main() {}\n",
		"coding-ethos/go/internal.go": "package internal\nfunc SkipMe() {}\n",
		"nested/coding-ethos/tool.go": "package tool\nfunc AlsoSkipMe() {}\n",
	}

	for path, content := range files {
		writeIndexedTestFile(t, tempDir, path, content)
	}

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("index tree: %v", err)
	}

	if summary.FilesIndexed != 1 {
		t.Fatalf("indexed %d files, want 1", summary.FilesIndexed)
	}

	assertCodeFilePresence(t, ctx, store, "app/main.go", true)
	assertCodeFilePresence(t, ctx, store, "coding-ethos/go/internal.go", false)
	assertCodeFilePresence(t, ctx, store, "nested/coding-ethos/tool.go", false)
}

func TestASTIndexerIndexesRepoNamedCodingEthos(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()
	repo := filepath.Join(tempDir, "coding-ethos")

	writeIndexedTestFile(t, repo, "go/main.go", "package main\nfunc main() {}\n")
	writeIndexedTestFile(t, repo, "nested/coding-ethos/tool.go", "package tool\n")

	summary, err := indexer.IndexPaths(ctx, repo, []string{repo})
	if err != nil {
		t.Fatalf("index tree: %v", err)
	}

	if summary.FilesIndexed != 1 {
		t.Fatalf("indexed %d files, want 1", summary.FilesIndexed)
	}

	assertCodeFilePresence(t, ctx, store, "go/main.go", true)
	assertCodeFilePresence(t, ctx, store, "nested/coding-ethos/tool.go", false)
}

func TestASTIndexerUsesConfiguredRepoExcludes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()

	writeIndexedTestFile(t, tempDir, "app/main.go", "package main\nfunc main() {}\n")
	writeIndexedTestFile(t, tempDir, "public/dist/generated.go", "package generated\n")
	writeIndexedTestFile(
		t,
		tempDir,
		"repo_config.yaml",
		"code_intel:\n  exclude_paths:\n    - \"**/dist/**\"\n",
	)

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("index tree: %v", err)
	}

	if summary.FilesIndexed != 2 {
		t.Fatalf(
			"indexed %d files, want app/main.go and repo_config.yaml",
			summary.FilesIndexed,
		)
	}

	assertCodeFilePresence(t, ctx, store, "app/main.go", true)
	assertCodeFilePresence(t, ctx, store, "repo_config.yaml", true)
	assertCodeFilePresence(t, ctx, store, "public/dist/generated.go", false)
}

func TestASTIndexerRejectsInvalidConfiguredRepoExclude(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()

	writeIndexedTestFile(t, tempDir, "app/main.go", "package main\nfunc main() {}\n")
	writeIndexedTestFile(
		t,
		tempDir,
		"repo_config.yaml",
		"code_intel:\n  exclude_paths:\n    - \"[\"\n",
	)

	_, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err == nil {
		t.Fatal("expected invalid exclude pattern error")
	}

	if !strings.Contains(err.Error(), "code_intel.exclude_paths") {
		t.Fatalf("error = %v", err)
	}
}

func TestASTIndexerDoesNotHardCodeConsumerDistDirectory(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()

	writeIndexedTestFile(t, tempDir, "dist/source.go", "package dist\n")

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("index tree: %v", err)
	}

	if summary.FilesIndexed != 1 {
		t.Fatalf("indexed %d files, want 1", summary.FilesIndexed)
	}

	assertCodeFilePresence(t, ctx, store, "dist/source.go", true)
}

func TestASTIndexerStoresExtendedEdges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	indexer := NewASTIndexer(store)
	tempDir := t.TempDir()

	// 1. Python file with calls and inheritance
	pyPath := filepath.Join(tempDir, "app.py")
	pyContent := []byte(`
class Base: pass
class Sub(Base):
    def run(self):
        other()
`)

	err := os.WriteFile(pyPath, pyContent, 0o600)
	if err != nil {
		t.Fatalf("write py: %v", err)
	}

	// 2. Go test file
	goTestPath := filepath.Join(tempDir, "logic_test.go")
	goTestContent := []byte(`
package logic
func TestExecute() {
    Execute()
}
`)

	err = os.WriteFile(goTestPath, goTestContent, 0o600)
	if err != nil {
		t.Fatalf("write go test: %v", err)
	}

	// 3. Markdown file
	mdPath := filepath.Join(tempDir, "README.md")
	mdContent := []byte(`# Installation
Run the installer.
`)

	err = os.WriteFile(mdPath, mdContent, 0o600)
	if err != nil {
		t.Fatalf("write md: %v", err)
	}

	summary, err := indexer.IndexPaths(ctx, tempDir, []string{tempDir})
	if err != nil {
		t.Fatalf("index: %v", err)
	}

	if summary.FilesIndexed != 3 {
		t.Fatalf("indexed %d, want 3", summary.FilesIndexed)
	}

	// Check edges
	edges, err := store.CodeEdges(ctx, CodeEdgeQuery{Limit: 100})
	if err != nil {
		t.Fatalf("query edges: %v", err)
	}

	if !hasCodeEdge(edges, "inherits", "Base") {
		t.Errorf("missing inherits:Base edge. Edges: %#v", edges)
	}

	if !hasCodeEdge(edges, "calls", "other") {
		t.Errorf("missing calls:other edge. Edges: %#v", edges)
	}

	if !hasCodeEdge(edges, "verifies", "Execute") {
		t.Errorf("missing verifies:Execute edge. Edges: %#v", edges)
	}

	if !hasCodeEdge(edges, "documents", "Installation") {
		t.Errorf("missing documents:Installation edge. Edges: %#v", edges)
	}
}

func writeIndexedTestFile(t *testing.T, root, path, content string) {
	t.Helper()

	fullPath := filepath.Join(root, path)
	err := os.MkdirAll(filepath.Dir(fullPath), 0o700)
	if err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(fullPath), err)
	}

	err = os.WriteFile(fullPath, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func assertCodeFilePresence(
	t *testing.T,
	ctx context.Context,
	store *Store,
	path string,
	wantFound bool,
) {
	t.Helper()

	file, found, err := store.GetCodeFile(ctx, path)
	if err != nil {
		t.Fatalf("get %s: %v", path, err)
	}

	if found != wantFound {
		t.Fatalf("file %s found=%v, want %v: %#v", path, found, wantFound, file)
	}
}

func hasCodeEdge(edges []CodeEdge, kind, targetName string) bool {
	for _, edge := range edges {
		if edge.Kind == kind && edge.TargetName == targetName {
			return true
		}
	}

	return false
}
