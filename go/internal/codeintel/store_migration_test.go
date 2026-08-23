// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateStoreMergesAndVerifiesWithoutChangingSource(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	sourcePath := DefaultDBPath(repositoryRoot)
	destinationPath := filepath.Join(t.TempDir(), ".coding-ethos", "code-intel.duckdb")

	insertMigrationCodeFile(t, ctx, sourcePath, "pkg/source.go", "source-hash")
	insertMigrationCodeFile(t, ctx, destinationPath, "pkg/destination.go", "dest-hash")
	recordTestMigrationIdentity(t, ctx, destinationPath, repositoryRoot)

	sourceHashBefore, err := storeMigrationFileSHA256(sourcePath)
	if err != nil {
		t.Fatalf("hash source before migration: %v", err)
	}

	result, err := MigrateStore(ctx, StoreMigrationOptions{
		RepositoryRoot:  repositoryRoot,
		SourcePath:      sourcePath,
		DestinationPath: destinationPath,
	})
	if err != nil {
		t.Fatalf("migrate store: %v", err)
	}
	if !result.Verified || !result.Manifest.SourceUnchanged ||
		!result.Manifest.DestinationRowsVerified {
		t.Fatalf("migration result is not verified: %#v", result)
	}
	if result.Manifest.SourceSHA256Before != sourceHashBefore ||
		result.Manifest.SourceSHA256After != sourceHashBefore {
		t.Fatalf("source hash changed: %#v", result.Manifest)
	}

	codeFiles := migrationTableEvidence(t, result.Manifest.Tables, "code_files")
	if codeFiles.SourceRows != 1 || codeFiles.ImportedRows != 1 ||
		codeFiles.MatchedRows != 0 || codeFiles.DestinationRows != 2 {
		t.Fatalf("unexpected code_files evidence: %#v", codeFiles)
	}

	assertMigrationCodeFileHash(t, ctx, destinationPath, "pkg/source.go", "source-hash")
	assertMigrationCodeFileHash(t, ctx, destinationPath, "pkg/destination.go", "dest-hash")
	assertMigrationManifestDigest(t, result)
}

func TestMigrateStoreAcceptsEqualDuplicateRows(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	sourcePath := DefaultDBPath(repositoryRoot)
	destinationPath := filepath.Join(t.TempDir(), ".coding-ethos", "code-intel.duckdb")

	insertMigrationCodeFile(t, ctx, sourcePath, "pkg/shared.go", "same-hash")
	insertMigrationCodeFile(t, ctx, destinationPath, "pkg/shared.go", "same-hash")
	recordTestMigrationIdentity(t, ctx, destinationPath, repositoryRoot)

	result, err := MigrateStore(ctx, StoreMigrationOptions{
		RepositoryRoot:  repositoryRoot,
		SourcePath:      sourcePath,
		DestinationPath: destinationPath,
	})
	if err != nil {
		t.Fatalf("migrate equal duplicate: %v", err)
	}

	codeFiles := migrationTableEvidence(t, result.Manifest.Tables, "code_files")
	if codeFiles.ImportedRows != 0 || codeFiles.MatchedRows != 1 ||
		codeFiles.DestinationRows != 1 {
		t.Fatalf("unexpected equal duplicate evidence: %#v", codeFiles)
	}
}

func TestMigrateStoreRejectsUnequalDuplicateRows(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	sourcePath := DefaultDBPath(repositoryRoot)
	destinationPath := filepath.Join(t.TempDir(), ".coding-ethos", "code-intel.duckdb")

	insertMigrationCodeFile(t, ctx, sourcePath, "pkg/shared.go", "source-hash")
	insertMigrationCodeFile(t, ctx, destinationPath, "pkg/shared.go", "destination-hash")
	recordTestMigrationIdentity(t, ctx, destinationPath, repositoryRoot)

	_, err := MigrateStore(ctx, StoreMigrationOptions{
		RepositoryRoot:  repositoryRoot,
		SourcePath:      sourcePath,
		DestinationPath: destinationPath,
	})
	if err == nil ||
		!strings.Contains(err.Error(), "unequal duplicate row in code_files") {
		t.Fatalf("expected unequal duplicate rejection, got %v", err)
	}

	assertMigrationCodeFileHash(
		t,
		ctx,
		destinationPath,
		"pkg/shared.go",
		"destination-hash",
	)

	manifests, globErr := filepath.Glob(destinationPath + ".migration-*.json")
	if globErr != nil {
		t.Fatalf("glob migration manifests: %v", globErr)
	}
	if len(manifests) != 0 {
		t.Fatalf("failed migration wrote manifests: %#v", manifests)
	}
}

func TestMigrateStoreRejectsRepositoryIdentityMismatch(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	sourcePath := DefaultDBPath(repositoryRoot)
	destinationPath := filepath.Join(t.TempDir(), ".coding-ethos", "code-intel.duckdb")
	store := openMigrationTestStore(t, ctx, sourcePath)

	_, err := store.Database().ExecContext(
		ctx,
		`INSERT INTO traces(trace_id, trace_kind, repo_root, raw_json)
		VALUES ('wrong-root', 'hook', ?, '{}')`,
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("insert mismatched trace: %v", err)
	}
	closeMigrationTestStore(t, store)

	_, err = MigrateStore(ctx, StoreMigrationOptions{
		RepositoryRoot:  repositoryRoot,
		SourcePath:      sourcePath,
		DestinationPath: destinationPath,
	})
	if err == nil || !strings.Contains(err.Error(), "source store repository identity") {
		t.Fatalf("expected repository identity rejection, got %v", err)
	}
	if _, statErr := os.Stat(destinationPath); !os.IsNotExist(statErr) {
		t.Fatalf("identity rejection created destination: %v", statErr)
	}
}

func TestMigrateStoreRejectsUnexpectedSchemaVersion(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	sourcePath := DefaultDBPath(repositoryRoot)
	destinationPath := filepath.Join(t.TempDir(), ".coding-ethos", "code-intel.duckdb")
	store := openMigrationTestStore(t, ctx, sourcePath)

	_, err := store.Database().ExecContext(
		ctx,
		"UPDATE schema_metadata SET value = '999' WHERE key = 'schema_version'",
	)
	if err != nil {
		t.Fatalf("change test schema version: %v", err)
	}
	closeMigrationTestStore(t, store)

	_, err = MigrateStore(ctx, StoreMigrationOptions{
		RepositoryRoot:  repositoryRoot,
		SourcePath:      sourcePath,
		DestinationPath: destinationPath,
	})
	if err == nil || !strings.Contains(err.Error(), "schema version is 999") {
		t.Fatalf("expected schema version rejection, got %v", err)
	}
}

func TestMigrateStoreRejectsExistingManifestBeforeOpeningDestination(t *testing.T) {
	ctx := context.Background()
	repositoryRoot := t.TempDir()
	sourcePath := DefaultDBPath(repositoryRoot)
	destinationPath := filepath.Join(t.TempDir(), ".coding-ethos", "code-intel.duckdb")
	manifestPath := filepath.Join(t.TempDir(), "migration.json")

	insertMigrationCodeFile(t, ctx, sourcePath, "pkg/source.go", "source-hash")
	if err := os.WriteFile(manifestPath, []byte("existing audit\n"), 0o600); err != nil {
		t.Fatalf("write existing manifest fixture: %v", err)
	}

	_, err := MigrateStore(ctx, StoreMigrationOptions{
		RepositoryRoot:  repositoryRoot,
		SourcePath:      sourcePath,
		DestinationPath: destinationPath,
		ManifestPath:    manifestPath,
	})
	if err == nil || !strings.Contains(err.Error(), "audit file already exists") {
		t.Fatalf("expected existing manifest rejection, got %v", err)
	}
	if _, statErr := os.Stat(destinationPath); !os.IsNotExist(statErr) {
		t.Fatalf("manifest rejection created destination: %v", statErr)
	}
}

func insertMigrationCodeFile(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	path string,
	contentHash string,
) {
	t.Helper()
	store := openMigrationTestStore(t, ctx, databasePath)

	_, err := store.Database().ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES (?, 'go', ?, 10, 1, '2026-08-23T00:00:00Z')`,
		path,
		contentHash,
	)
	if err != nil {
		t.Fatalf("insert migration code file: %v", err)
	}

	closeMigrationTestStore(t, store)
}

func recordTestMigrationIdentity(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	repositoryRoot string,
) {
	t.Helper()
	store := openMigrationTestStore(t, ctx, databasePath)

	_, err := store.Database().ExecContext(
		ctx,
		`INSERT OR REPLACE INTO schema_metadata(key, value)
		VALUES ('repository_identity', ?)`,
		repositoryRoot,
	)
	if err != nil {
		t.Fatalf("record test migration identity: %v", err)
	}

	closeMigrationTestStore(t, store)
}

func assertMigrationCodeFileHash(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	path string,
	want string,
) {
	t.Helper()
	store, err := OpenReadOnly(ctx, databasePath)
	if err != nil {
		t.Fatalf("open migrated store read-only: %v", err)
	}
	defer store.Close()

	var got string
	if err = store.Database().QueryRowContext(
		ctx,
		"SELECT content_hash FROM code_files WHERE path = ?",
		path,
	).Scan(&got); err != nil {
		t.Fatalf("read migrated code file: %v", err)
	}
	if got != want {
		t.Fatalf("content hash for %s = %q, want %q", path, got, want)
	}
}

func openMigrationTestStore(t *testing.T, ctx context.Context, path string) *Store {
	t.Helper()

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open migration test store: %v", err)
	}

	return store
}

func closeMigrationTestStore(t *testing.T, store *Store) {
	t.Helper()

	if err := store.Close(); err != nil {
		t.Fatalf("close migration test store: %v", err)
	}
}

func migrationTableEvidence(
	t *testing.T,
	tables []StoreMigrationTable,
	name string,
) StoreMigrationTable {
	t.Helper()

	for _, table := range tables {
		if table.Table == name {
			return table
		}
	}

	t.Fatalf("migration evidence missing table %s", name)

	return StoreMigrationTable{}
}

func assertMigrationManifestDigest(t *testing.T, result StoreMigrationResult) {
	t.Helper()

	payload, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatalf("read migration manifest: %v", err)
	}

	var manifest StoreMigrationManifest
	if err = json.Unmarshal(payload, &manifest); err != nil {
		t.Fatalf("decode migration manifest: %v", err)
	}
	if manifest.Kind != storeMigrationManifestKind {
		t.Fatalf("manifest kind = %q", manifest.Kind)
	}

	if err = verifyStoreMigrationManifest(
		result.ManifestPath,
		result.DigestPath,
		result.ManifestSHA256,
	); err != nil {
		t.Fatalf("verify migration manifest: %v", err)
	}
}
