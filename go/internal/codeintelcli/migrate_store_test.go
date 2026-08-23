// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestStatsDefaultsToConfiguredStateRoot(t *testing.T) {
	repositoryRoot := t.TempDir()
	stateRoot := t.TempDir()
	t.Setenv(codeintel.StateRootEnvironment, stateRoot)

	err := runCapturingStdout(t, context.Background(), []string{
		"stats",
		"--root", repositoryRoot,
	})
	if err != nil {
		t.Fatalf("run stats with private state: %v", err)
	}

	privatePath := codeintel.DefaultDBPath(stateRoot)
	if _, err = os.Stat(privatePath); err != nil {
		t.Fatalf("private code-intel store was not created: %v", err)
	}
	if _, err = os.Stat(codeintel.DefaultDBPath(repositoryRoot)); !os.IsNotExist(err) {
		t.Fatalf("stats created repository-local state: %v", err)
	}
}

func TestMigrateStoreCommandDefaultsDestinationToConfiguredStateRoot(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	stateRoot := t.TempDir()
	t.Setenv(codeintel.StateRootEnvironment, stateRoot)

	sourcePath := codeintel.DefaultDBPath(repositoryRoot)
	store, err := codeintel.Open(ctx, sourcePath)
	if err != nil {
		t.Fatalf("open migration command source: %v", err)
	}
	_, err = store.Database().ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES ('pkg/main.go', 'go', 'hash', 10, 1, '2026-08-23T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert migration command fixture: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close migration command source: %v", err)
	}

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(ctx, []string{"migrate-store", "--root", repositoryRoot})
	})
	if runErr != nil {
		t.Fatalf("run migrate-store: %v", runErr)
	}
	if !strings.Contains(output, `"verified": true`) ||
		!strings.Contains(output, filepath.Clean(stateRoot)) {
		t.Fatalf("migrate-store output is missing verification evidence:\n%s", output)
	}

	destinationPath := codeintel.DefaultDBPath(stateRoot)
	destination, err := codeintel.OpenReadOnly(ctx, destinationPath)
	if err != nil {
		t.Fatalf("open migration command destination: %v", err)
	}
	defer destination.Close()

	var contentHash string
	if err = destination.Database().QueryRowContext(
		ctx,
		"SELECT content_hash FROM code_files WHERE path = 'pkg/main.go'",
	).Scan(&contentHash); err != nil {
		t.Fatalf("read migrated command fixture: %v", err)
	}
	if contentHash != "hash" {
		t.Fatalf("migrated content hash = %q", contentHash)
	}
}

func TestExplicitStorePathOverridesConfiguredStateRoot(t *testing.T) {
	repositoryRoot := t.TempDir()
	stateRoot := t.TempDir()
	explicitPath := filepath.Join(t.TempDir(), "explicit.duckdb")
	t.Setenv(codeintel.StateRootEnvironment, stateRoot)

	if got := resolvedDBPath(repositoryRoot, explicitPath); got != explicitPath {
		t.Fatalf("resolved explicit path = %q, want %q", got, explicitPath)
	}
}

func TestMigrateStoreCommandAcceptsManagedDBDestination(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	destinationPath := filepath.Join(t.TempDir(), "managed", "code-intel.duckdb")
	sourcePath := codeintel.DefaultDBPath(repositoryRoot)

	store, err := codeintel.Open(ctx, sourcePath)
	if err != nil {
		t.Fatalf("open managed migration source: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close managed migration source: %v", err)
	}

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(ctx, []string{
			"migrate-store",
			"--root", repositoryRoot,
			"--db", destinationPath,
		})
	})
	if runErr != nil {
		t.Fatalf("run migrate-store with managed destination: %v", runErr)
	}
	if !strings.Contains(output, `"destination_path": "`+destinationPath+`"`) {
		t.Fatalf("managed destination is missing from output:\n%s", output)
	}
	if _, err = os.Stat(destinationPath); err != nil {
		t.Fatalf("managed destination was not created: %v", err)
	}
}
