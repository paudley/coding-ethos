// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel_test

import (
	"context"
	"os"
	"path/filepath"
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
