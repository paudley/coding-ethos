// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRejectsUnknownCommand(t *testing.T) {
	t.Parallel()

	if err := run(context.Background(), []string{"unknown"}); err == nil {
		t.Fatalf("expected unknown command error")
	}
}

func TestStatsCreatesStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	err := run(context.Background(), []string{"stats", "--root", root, "--db", dbPath})
	if err != nil {
		t.Fatalf("stats command returned error: %v", err)
	}
}

func TestVectorStatsCreatesSQLiteVectorStore(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	err := run(context.Background(), []string{"vector-stats", "--root", root})
	if err != nil {
		t.Fatalf("vector-stats command returned error: %v", err)
	}
}

func TestIndexCodeAndCodeChunksCommands(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	sourcePath := filepath.Join(root, "cmd", "app.go")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte(`package main

func main() {
	println("ok")
}
`), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	ctx := context.Background()
	if err := run(ctx, []string{"index-code", "--root", root, "--db", dbPath, "cmd"}); err != nil {
		t.Fatalf("index-code command returned error: %v", err)
	}
	if err := run(ctx, []string{
		"code-chunks", "--root", root, "--db", dbPath,
		"--path", "cmd/app.go", "--symbol-name", "main",
	}); err != nil {
		t.Fatalf("code-chunks command returned error: %v", err)
	}
}
