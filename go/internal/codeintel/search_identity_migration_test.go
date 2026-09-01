// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

func TestOpenMigratesExactDuplicateSearchIdentities(t *testing.T) {
	ctx := context.Background()
	path, database := openLegacySearchIdentityFixture(t, ctx)

	_, err := database.ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(
			fts_id, kind, record_id, trace_id, path, message, search_text
		) VALUES
			('legacy:one', 'finding', 'one', 'trace', 'one.go', 'same', 'same text'),
			('legacy:one', 'finding', 'one', 'trace', 'one.go', 'same', 'same text');
		INSERT INTO code_intel_search_terms(term, fts_id) VALUES
			('same', 'legacy:one'),
			('same', 'legacy:one')`,
	)
	if err != nil {
		t.Fatalf("insert legacy duplicate identities: %v", err)
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close legacy identity fixture: %v", err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("upgrade legacy duplicate identities: %v", err)
	}
	defer store.Close()

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("read upgraded identity stats: %v", err)
	}
	if stats.SchemaVersion != schemaVersion || stats.FtsRows != 1 ||
		stats.SearchTermRows != 1 || stats.FtsDuplicateRows != 0 ||
		stats.SearchTermDuplicateRows != 0 {
		t.Fatalf("unexpected upgraded identity stats: %#v", stats)
	}

	var message, searchText string
	if err = store.Database().QueryRowContext(
		ctx,
		`SELECT message, search_text FROM code_intel_fts
		WHERE fts_id = 'legacy:one'`,
	).Scan(&message, &searchText); err != nil {
		t.Fatalf("read preserved v1 search identity: %v", err)
	}
	if message != "same" || searchText != "same text" {
		t.Fatalf(
			"preserved v1 search identity = (%q, %q)",
			message,
			searchText,
		)
	}

	_, err = store.Database().ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(fts_id, kind, record_id, search_text)
		VALUES ('legacy:one', 'finding', 'two', 'other')`,
	)
	if err == nil {
		t.Fatal("upgraded FTS identity accepted a duplicate")
	}
	_, err = store.Database().ExecContext(
		ctx,
		`INSERT INTO code_intel_search_terms(term, fts_id)
		VALUES ('same', 'legacy:one')`,
	)
	if err == nil {
		t.Fatal("upgraded search-term identity accepted a duplicate")
	}
}

func TestOpenRejectsConflictingDuplicateSearchIdentity(t *testing.T) {
	ctx := context.Background()
	path, database := openLegacySearchIdentityFixture(t, ctx)

	_, err := database.ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(
			fts_id, kind, record_id, trace_id, path, message, search_text
		) VALUES
			('legacy:conflict', 'finding', 'one', 'trace', 'one.go', 'first', 'same text'),
			('legacy:conflict', 'finding', 'one', 'trace', 'one.go', 'second', 'same text')`,
	)
	if err != nil {
		t.Fatalf("insert conflicting legacy identities: %v", err)
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close conflicting identity fixture: %v", err)
	}

	_, err = Open(ctx, path)
	if err == nil || !strings.Contains(
		err.Error(),
		"conflicting code intelligence FTS rows share identity",
	) {
		t.Fatalf("expected conflicting identity rejection, got %v", err)
	}

	readOnly, err := sql.Open("duckdb", path+"?access_mode=READ_ONLY")
	if err != nil {
		t.Fatalf("open rejected legacy store read-only: %v", err)
	}
	defer readOnly.Close()

	var rows int
	if err = readOnly.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM code_intel_fts WHERE fts_id = 'legacy:conflict'",
	).Scan(&rows); err != nil {
		t.Fatalf("count retained conflicting identities: %v", err)
	}
	if rows != 2 {
		t.Fatalf("conflicting migration retained %d rows, want 2", rows)
	}
}

func TestRoutineV2SearchIdentityMigrationSkipsRecoveryScan(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, DefaultDBPath(t.TempDir()))
	if err != nil {
		t.Fatalf("open v2 search identity store: %v", err)
	}
	defer store.Close()

	dropFTSIdentityIndex(t, ctx, store.Database())

	_, err = store.Database().ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(
			fts_id, kind, record_id, message, search_text
		) VALUES
			('v2:conflict', 'finding', 'one', 'first', 'same text'),
			('v2:conflict', 'finding', 'one', 'second', 'same text')`,
	)
	if err != nil {
		t.Fatalf("insert v2 recovery sentinel: %v", err)
	}

	if err = migrateSearchIdentity(ctx, store.Database()); err != nil {
		t.Fatalf("routine v2 migration entered recovery scan: %v", err)
	}

	var rows int
	if err = store.Database().QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM code_intel_fts WHERE fts_id = 'v2:conflict'",
	).Scan(&rows); err != nil {
		t.Fatalf("count retained v2 recovery sentinel: %v", err)
	}
	if rows != 2 {
		t.Fatalf("routine v2 migration rewrote %d sentinel rows, want 2", rows)
	}
}

func TestExplicitSearchIdentityRecoveryRepairsV2Replay(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, DefaultDBPath(t.TempDir()))
	if err != nil {
		t.Fatalf("open v2 recovery store: %v", err)
	}
	defer store.Close()

	dropFTSIdentityIndex(t, ctx, store.Database())

	_, err = store.Database().ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(
			fts_id, kind, record_id, message, search_text
		) VALUES
			('v2:replay', 'finding', 'one', 'same', 'same text'),
			('v2:replay', 'finding', 'one', 'same', 'same text')`,
	)
	if err != nil {
		t.Fatalf("insert v2 replay: %v", err)
	}

	if err = deduplicateSearchIdentity(ctx, store.Database()); err != nil {
		t.Fatalf("run explicit v2 search identity recovery: %v", err)
	}

	var rows int
	if err = store.Database().QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM code_intel_fts WHERE fts_id = 'v2:replay'",
	).Scan(&rows); err != nil {
		t.Fatalf("count explicitly recovered v2 replay: %v", err)
	}
	if rows != 1 {
		t.Fatalf("explicit v2 recovery retained %d replay rows, want 1", rows)
	}
}

func TestRepeatedCodeIndexWriteKeepsSearchAndRelationshipsStable(t *testing.T) {
	ctx := context.Background()
	store, err := Open(ctx, DefaultDBPath(t.TempDir()))
	if err != nil {
		t.Fatalf("open repeated-index store: %v", err)
	}
	defer store.Close()

	file := CodeFile{
		Path:         "pkg/replayed.go",
		Language:     "go",
		ContentHash:  "file-one",
		SizeBytes:    30,
		LineCount:    3,
		IndexedAtUTC: "2026-08-30T00:00:00Z",
	}
	chunk := CodeChunk{
		ID:          "chunk-replayed",
		Path:        file.Path,
		Language:    "go",
		NodeKind:    "function",
		SymbolKind:  "function",
		SymbolName:  "Replayed",
		SymbolPath:  "Replayed",
		StartByte:   0,
		EndByte:     30,
		StartLine:   1,
		EndLine:     3,
		ContentHash: "chunk-one",
		SearchText:  "alpha beta",
		RawText:     "func Replayed() {}",
	}
	edge := CodeEdge{
		ID:            "edge-replayed",
		Kind:          "calls",
		Path:          file.Path,
		SourceChunkID: chunk.ID,
		TargetName:    "Other",
	}
	if err = store.ReplaceCodeFileIndex(
		ctx,
		file,
		[]CodeChunk{chunk},
		[]CodeEdge{edge},
	); err != nil {
		t.Fatalf("write initial code index: %v", err)
	}

	transaction, err := store.Database().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin AST link fixture: %v", err)
	}
	if err = insertASTFindingLink(ctx, transaction, ASTFindingLink{
		ID:          "link-replayed",
		FindingKind: "sarif_result",
		FindingID:   "result-replayed",
		ChunkID:     chunk.ID,
		Path:        file.Path,
		SymbolPath:  chunk.SymbolPath,
		ContentHash: chunk.ContentHash,
	}); err != nil {
		_ = transaction.Rollback()
		t.Fatalf("insert AST link fixture: %v", err)
	}
	if err = transaction.Commit(); err != nil {
		t.Fatalf("commit AST link fixture: %v", err)
	}

	file.ContentHash = "file-two"
	chunk.ContentHash = "chunk-two"
	chunk.SearchText = "alpha gamma"
	if err = store.ReplaceCodeFileIndex(
		ctx,
		file,
		[]CodeChunk{chunk},
		[]CodeEdge{edge},
	); err != nil {
		t.Fatalf("replay updated code index: %v", err)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("read replayed-index stats: %v", err)
	}
	if stats.CodeChunks != 1 || stats.CodeEdges != 1 || stats.ASTFindingLinks != 1 ||
		stats.FtsRows != 1 || stats.SearchTermRows != 2 ||
		stats.FtsDuplicateRows != 0 || stats.SearchTermDuplicateRows != 0 {
		t.Fatalf("unexpected replayed-index stats: %#v", stats)
	}

	var staleTerms int
	if err = store.Database().QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM code_intel_search_terms
		WHERE fts_id = 'code_chunk:chunk-replayed:' AND term = 'beta'`,
	).Scan(&staleTerms); err != nil {
		t.Fatalf("count stale replayed search terms: %v", err)
	}
	if staleTerms != 0 {
		t.Fatalf("replayed index retained %d stale search terms", staleTerms)
	}
}

func openLegacySearchIdentityFixture(
	t *testing.T,
	ctx context.Context,
) (string, *sql.DB) {
	t.Helper()

	path := DefaultDBPath(t.TempDir())
	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("initialize legacy search identity fixture: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close initialized search identity fixture: %v", err)
	}

	database, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open legacy search identity fixture: %v", err)
	}
	for _, statement := range []string{
		"DROP INDEX IF EXISTS idx_code_intel_fts_id_unique",
		"DROP INDEX IF EXISTS idx_code_intel_search_terms_unique",
		"UPDATE schema_metadata SET value = '1' WHERE key = 'schema_version'",
	} {
		if _, err = database.ExecContext(ctx, statement); err != nil {
			_ = database.Close()
			t.Fatalf("prepare legacy search identity fixture: %v", err)
		}
	}

	return path, database
}

func dropFTSIdentityIndex(t *testing.T, ctx context.Context, database *sql.DB) {
	t.Helper()

	_, err := database.ExecContext(ctx, "DROP INDEX idx_code_intel_fts_id_unique")
	if err != nil {
		t.Fatalf("drop FTS identity index: %v", err)
	}
}
