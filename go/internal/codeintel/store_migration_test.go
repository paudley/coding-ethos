// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
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

func TestMigrateStorePreservesAndDeduplicatesEqualLogicalKeyRows(t *testing.T) {
	tests := []struct {
		name                string
		sourceCopies        int
		destinationCopies   int
		wantImported        int64
		wantMatched         int64
		wantDestinationRows int64
	}{
		{
			name:                "source duplicates are imported",
			sourceCopies:        3,
			destinationCopies:   0,
			wantImported:        3,
			wantMatched:         0,
			wantDestinationRows: 3,
		},
		{
			name:                "destination duplicates match once per source row",
			sourceCopies:        2,
			destinationCopies:   3,
			wantImported:        0,
			wantMatched:         2,
			wantDestinationRows: 3,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			repositoryRoot := t.TempDir()
			sourcePath := DefaultDBPath(repositoryRoot)
			destinationPath := filepath.Join(
				t.TempDir(),
				".coding-ethos",
				"code-intel.duckdb",
			)

			insertMigrationLogicalKeyRows(t, ctx, sourcePath, test.sourceCopies)
			insertMigrationLogicalKeyRows(t, ctx, destinationPath, test.destinationCopies)
			recordTestMigrationIdentity(t, ctx, destinationPath, repositoryRoot)

			result, err := MigrateStore(ctx, StoreMigrationOptions{
				RepositoryRoot:  repositoryRoot,
				SourcePath:      sourcePath,
				DestinationPath: destinationPath,
			})
			if err != nil {
				t.Fatalf("migrate equal logical-key duplicates: %v", err)
			}

			lshEvidence := migrationTableEvidence(t, result.Manifest.Tables, "lsh_bands")
			if lshEvidence.SourceRows != int64(test.sourceCopies) ||
				lshEvidence.ImportedRows != test.wantImported ||
				lshEvidence.MatchedRows != test.wantMatched ||
				lshEvidence.DestinationRows != test.wantDestinationRows {
				t.Fatalf("unexpected lsh_bands evidence: %#v", lshEvidence)
			}
			assertMigrationTableRowCount(
				t,
				ctx,
				destinationPath,
				"lsh_bands",
				test.wantDestinationRows,
			)

			for _, tableName := range []string{
				"code_intel_fts",
				"code_intel_search_terms",
			} {
				evidence := migrationTableEvidence(t, result.Manifest.Tables, tableName)
				wantImported := min(test.wantImported, 1)
				wantDestinationRows := min(test.wantDestinationRows, 1)
				wantDeduplicated := int64(0)
				if test.destinationCopies == 0 {
					wantDeduplicated = int64(test.sourceCopies) - wantImported
				}
				if evidence.SourceRows != int64(test.sourceCopies) ||
					evidence.ImportedRows != wantImported ||
					evidence.MatchedRows != test.wantMatched ||
					evidence.DeduplicatedRows != wantDeduplicated ||
					evidence.DestinationRows != wantDestinationRows {
					t.Fatalf("unexpected %s evidence: %#v", tableName, evidence)
				}
				assertMigrationTableRowCount(
					t,
					ctx,
					destinationPath,
					tableName,
					wantDestinationRows,
				)
			}
		})
	}
}

func TestMigrateStoreRejectsConflictingLogicalKeyVariants(t *testing.T) {
	for _, tableName := range []string{"lsh_bands", "code_intel_fts"} {
		for _, conflictLocation := range []string{"source", "destination"} {
			t.Run(tableName+"/"+conflictLocation, func(t *testing.T) {
				ctx := context.Background()
				repositoryRoot := t.TempDir()
				sourcePath := DefaultDBPath(repositoryRoot)
				destinationPath := filepath.Join(
					t.TempDir(),
					".coding-ethos",
					"code-intel.duckdb",
				)

				initializeMigrationStore(t, ctx, sourcePath)
				initializeMigrationStore(t, ctx, destinationPath)
				if conflictLocation == "source" {
					insertMigrationLogicalKeyConflict(t, ctx, sourcePath, tableName)
				} else {
					insertMigrationLogicalKeyConflict(t, ctx, destinationPath, tableName)
				}
				recordTestMigrationIdentity(t, ctx, destinationPath, repositoryRoot)

				_, err := MigrateStore(ctx, StoreMigrationOptions{
					RepositoryRoot:  repositoryRoot,
					SourcePath:      sourcePath,
					DestinationPath: destinationPath,
				})
				conflictingVariant := err != nil &&
					strings.Contains(err.Error(), "conflicting row variants") &&
					strings.Contains(err.Error(), tableName)
				conflictingUpgrade := err != nil && tableName == "code_intel_fts" &&
					strings.Contains(
						err.Error(),
						"conflicting code intelligence FTS rows share identity",
					)
				if !conflictingVariant && !conflictingUpgrade {
					t.Fatalf("expected %s conflict rejection, got %v", tableName, err)
				}

				assertMigrationTableRowCount(
					t,
					ctx,
					map[string]string{
						"source":      sourcePath,
						"destination": destinationPath,
					}[conflictLocation],
					tableName,
					2,
				)
				assertNoMigrationManifest(t, destinationPath)
			})
		}
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

func initializeMigrationStore(t *testing.T, ctx context.Context, databasePath string) {
	t.Helper()
	closeMigrationTestStore(t, openMigrationTestStore(t, ctx, databasePath))
}

func insertMigrationLogicalKeyRows(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	copies int,
) {
	t.Helper()
	store := openMigrationTestStore(t, ctx, databasePath)

	insertMigrationLogicalKeySupport(t, ctx, store.Database())
	for range copies {
		_, err := store.Database().ExecContext(
			ctx,
			`INSERT INTO lsh_bands(
				band_hash, band_index, chunk_id, path, symbol_name
			) VALUES ('band-shared', 1, 'chunk-shared', 'pkg/shared.go', 'Shared')`,
		)
		if err != nil {
			t.Fatalf("insert equal LSH migration row: %v", err)
		}

		_, err = store.Database().ExecContext(
			ctx,
			`INSERT INTO code_intel_fts(
				fts_id, kind, record_id, trace_id, policy_id, skill_id,
				path, message, search_text
			) VALUES (
				'fts-shared', 'finding', 'record-shared', 'trace-shared',
				'policy-shared', 'skill-shared', 'pkg/shared.go',
				'shared message', 'shared search text'
			)`,
		)
		if err != nil {
			t.Fatalf("insert equal FTS migration row: %v", err)
		}

		_, err = store.Database().ExecContext(
			ctx,
			`INSERT INTO code_intel_search_terms(term, fts_id)
			VALUES ('shared', 'fts-shared')`,
		)
		if err != nil {
			t.Fatalf("insert equal search-term migration row: %v", err)
		}
	}

	closeMigrationTestStore(t, store)
}

func insertMigrationLogicalKeyConflict(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	tableName string,
) {
	t.Helper()
	store := openMigrationTestStore(t, ctx, databasePath)
	insertMigrationLogicalKeySupport(t, ctx, store.Database())

	var err error
	switch tableName {
	case "lsh_bands":
		_, err = store.Database().ExecContext(
			ctx,
			`INSERT INTO lsh_bands(
				band_hash, band_index, chunk_id, path, symbol_name
			) VALUES
				('band-a', 1, 'chunk-shared', 'pkg/shared.go', 'Shared'),
				('band-b', 1, 'chunk-shared', 'pkg/shared.go', 'Shared')`,
		)
	case "code_intel_fts":
		_, err = store.Database().ExecContext(
			ctx,
			`INSERT INTO code_intel_fts(
				fts_id, kind, record_id, trace_id, path, message, search_text
			) VALUES
				('fts-shared', 'finding', 'record-shared', 'trace-shared',
				 'pkg/shared.go', NULL, 'shared search text'),
				('fts-shared', 'finding', 'record-shared', 'trace-shared',
				 'pkg/shared.go', 'different message', 'shared search text')`,
		)
	default:
		t.Fatalf("unsupported conflicting logical-key table %s", tableName)
	}
	if err != nil {
		t.Fatalf("insert %s conflict fixture: %v", tableName, err)
	}

	closeMigrationTestStore(t, store)
}

func insertMigrationLogicalKeySupport(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
) {
	t.Helper()
	for _, indexName := range []string{
		"idx_code_intel_fts_id_unique",
		"idx_code_intel_search_terms_unique",
	} {
		_, err := database.ExecContext(ctx, "DROP INDEX IF EXISTS "+indexName)
		if err != nil {
			t.Fatalf("drop v2 logical-key index %s: %v", indexName, err)
		}
	}

	_, err := database.ExecContext(
		ctx,
		"UPDATE schema_metadata SET value = '1' WHERE key = 'schema_version'",
	)
	if err != nil {
		t.Fatalf("mark logical-key fixture as schema v1: %v", err)
	}

	_, err = database.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES ('pkg/shared.go', 'go', 'shared-hash', 10, 1, '2026-08-23T00:00:00Z')`,
	)
	if err != nil {
		t.Fatalf("insert logical-key code file fixture: %v", err)
	}

	_, err = database.ExecContext(
		ctx,
		`INSERT OR IGNORE INTO code_chunks(
			chunk_id, path, language, node_kind, symbol_name,
			start_byte, end_byte, start_line, end_line,
			content_hash, search_text, raw_text
		) VALUES (
			'chunk-shared', 'pkg/shared.go', 'go', 'function', 'Shared',
			0, 10, 1, 1, 'chunk-hash', 'shared search text', 'func Shared() {}'
		)`,
	)
	if err != nil {
		t.Fatalf("insert logical-key code chunk fixture: %v", err)
	}
}

func assertMigrationTableRowCount(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	tableName string,
	want int64,
) {
	t.Helper()
	store, err := OpenReadOnly(ctx, databasePath)
	if err != nil {
		t.Fatalf("open migration store for row count: %v", err)
	}
	defer store.Close()

	query := "SELECT COUNT(*) FROM " + quoteMigrationIdentifier(tableName)
	var got int64
	if err = store.Database().QueryRowContext(ctx, query).Scan(&got); err != nil {
		t.Fatalf("count migration rows in %s: %v", tableName, err)
	}
	if got != want {
		t.Fatalf("row count in %s = %d, want %d", tableName, got, want)
	}
}

func assertNoMigrationManifest(t *testing.T, destinationPath string) {
	t.Helper()
	manifests, err := filepath.Glob(destinationPath + ".migration-*.json")
	if err != nil {
		t.Fatalf("glob migration manifests: %v", err)
	}
	if len(manifests) != 0 {
		t.Fatalf("failed migration wrote manifests: %#v", manifests)
	}
}

func recordTestMigrationIdentity(
	t *testing.T,
	ctx context.Context,
	databasePath string,
	repositoryRoot string,
) {
	t.Helper()
	database, err := sql.Open("duckdb", databasePath)
	if err != nil {
		t.Fatalf("open test migration identity database: %v", err)
	}
	defer database.Close()

	_, err = database.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO schema_metadata(key, value)
		VALUES ('repository_identity', ?)`,
		repositoryRoot,
	)
	if err != nil {
		t.Fatalf("record test migration identity: %v", err)
	}
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
