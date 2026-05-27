// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/astfacts"
)

func TestRebuildDuckDBIndexRemovesObsoleteStoreArtifacts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	for _, path := range ObsoleteCodeIntelArtifactPaths(root) {
		err := os.MkdirAll(filepath.Dir(path), duckDBStoreMode)
		if err != nil {
			t.Fatalf("create obsolete artifact dir: %v", err)
		}
		err = os.WriteFile(path, []byte("obsolete"), 0o600)
		if err != nil {
			t.Fatalf("write obsolete artifact: %v", err)
		}
	}

	err := NewEventLog(DefaultEventLogDir(root)).Append("run-1", []EventRecord{
		{
			Kind:    "hook_trace",
			TraceID: "trace-1",
			Tool:    "Bash",
		},
	})
	if err != nil {
		t.Fatalf("append event log: %v", err)
	}

	summary, err := RebuildDuckDBIndex(ctx, root, "", "")
	if err != nil {
		t.Fatalf("rebuild DuckDB index: %v", err)
	}
	if summary.EventCount != 1 ||
		summary.ImportedEventCount != 1 ||
		len(summary.RemovedObsoleteArtifacts) != len(ObsoleteCodeIntelArtifactPaths(root)) {
		t.Fatalf("unexpected rebuild summary: %#v", summary)
	}
	for _, path := range ObsoleteCodeIntelArtifactPaths(root) {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("obsolete artifact still exists: %s: %v", path, err)
		}
	}

	duckStore, err := OpenDuckDBReadOnly(ctx, DefaultDuckDBPath(root))
	if err != nil {
		t.Fatalf("open rebuilt DuckDB: %v", err)
	}
	defer duckStore.Close()

	analysis, err := AnalyzeDownstreamDuckDB(ctx, root, duckStore, 5)
	if err != nil {
		t.Fatalf("analyze DuckDB downstream: %v", err)
	}
	if analysis.StorageHealth.Backend != "duckdb" ||
		analysis.StorageHealth.SourceOfTruth != "event_log" ||
		analysis.StorageHealth.EventCount != 1 {
		t.Fatalf("storage health = %#v", analysis.StorageHealth)
	}
	if analysis.IssueSummary.StorageDecision == "" {
		t.Fatalf("missing issue summary: %#v", analysis.IssueSummary)
	}
}

func TestAnalyzeDownstreamDuckDBReportsStorePath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	customPath := filepath.Join(root, ".coding-ethos", "custom.duckdb")

	store, err := OpenDuckDB(ctx, customPath)
	if err != nil {
		t.Fatalf("open custom DuckDB: %v", err)
	}
	defer store.Close()

	analysis, err := AnalyzeDownstreamDuckDB(ctx, root, store, 5)
	if err != nil {
		t.Fatalf("analyze custom DuckDB: %v", err)
	}

	if analysis.StorageHealth.Path != customPath {
		t.Fatalf("storage path = %q, want %q", analysis.StorageHealth.Path, customPath)
	}
	if analysis.StorageHealth.Backend != "duckdb" {
		t.Fatalf("storage health = %#v", analysis.StorageHealth)
	}
}

func TestDuckDBGlobalRepoMapRefusesStaleSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "worker.py")
	original := []byte("def helper():\n    return 'ok'\n")

	writeDuckDBTestFile(t, sourcePath, original)

	store, err := OpenDuckDB(
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.duckdb"),
	)
	if err != nil {
		t.Fatalf("open DuckDB: %v", err)
	}
	defer store.Close()

	_, err = store.database.ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES (?, ?, ?, ?, ?, ?)`,
		"pkg/worker.py",
		"python",
		astfacts.ContentHash(original),
		len(original),
		2,
		"2026-01-01T00:00:00Z",
	)
	if err != nil {
		t.Fatalf("insert DuckDB code file: %v", err)
	}

	_, err = store.database.ExecContext(
		ctx,
		`INSERT INTO code_chunks(
			chunk_id, path, language, node_kind, start_byte, end_byte, start_line, end_line,
			content_hash, normalized_hash, search_text, raw_text, symbol_path,
			symbol_kind, symbol_name
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		"chunk-1",
		"pkg/worker.py",
		"python",
		"function_definition",
		0,
		len(original),
		1,
		2,
		astfacts.ContentHash(original),
		"normalized",
		string(original),
		string(original),
		"helper",
		"function",
		"helper",
	)
	if err != nil {
		t.Fatalf("insert DuckDB code chunk: %v", err)
	}

	writeDuckDBTestFile(t, sourcePath, []byte("def helper():\n    return 'changed'\n"))

	_, err = store.GlobalRepoMap(ctx, RepoMapQuery{
		Root:           root,
		Limit:          5,
		SymbolsPerFile: 1,
	})
	if err == nil || !strings.Contains(err.Error(), "stale code context") {
		t.Fatalf("global repo map error = %v, want stale code context", err)
	}
}

func writeDuckDBTestFile(t *testing.T, path string, payload []byte) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), duckDBStoreMode)
	if err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	err = os.WriteFile(path, payload, 0o600)
	if err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
}

func TestDuckDBRebuildLockRemovesStaleLock(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lockPath := filepath.Join(root, downstreamStateDir, "code-intel-rebuild.lock")

	err := os.MkdirAll(filepath.Dir(lockPath), duckDBStoreMode)
	if err != nil {
		t.Fatalf("create lock dir: %v", err)
	}

	err = os.WriteFile(lockPath, []byte(strconv.Itoa(-1)+"\n"), duckDBLockFileMode)
	if err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	release, err := acquireDuckDBRebuildLock(root)
	if err != nil {
		t.Fatalf("acquire stale lock: %v", err)
	}
	defer release()
}

func TestCleanupStaleDuckDBRebuildLockRemovesInvalidPID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lockPath := DuckDBRebuildLockPath(root)

	err := os.MkdirAll(filepath.Dir(lockPath), duckDBStoreMode)
	if err != nil {
		t.Fatalf("create lock dir: %v", err)
	}

	err = os.WriteFile(lockPath, []byte("-1\n"), duckDBLockFileMode)
	if err != nil {
		t.Fatalf("write stale lock: %v", err)
	}

	maintenance, err := CleanupStaleDuckDBRebuildLock(root, time.Now().UTC())
	if err != nil {
		t.Fatalf("cleanup stale lock: %v", err)
	}
	if !maintenance.Exists || !maintenance.Stale || !maintenance.Removed {
		t.Fatalf("maintenance = %#v, want stale removed lock", maintenance)
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("lock still exists after cleanup: %v", err)
	}
}

func TestCleanupStaleDuckDBRebuildLockRetainsCurrentPID(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	lockPath := DuckDBRebuildLockPath(root)

	err := os.MkdirAll(filepath.Dir(lockPath), duckDBStoreMode)
	if err != nil {
		t.Fatalf("create lock dir: %v", err)
	}

	err = os.WriteFile(
		lockPath,
		[]byte(strconv.Itoa(os.Getpid())+"\n"),
		duckDBLockFileMode,
	)
	if err != nil {
		t.Fatalf("write active lock: %v", err)
	}

	maintenance, err := CleanupStaleDuckDBRebuildLock(root, time.Now().UTC())
	if err != nil {
		t.Fatalf("cleanup active lock: %v", err)
	}
	if !maintenance.Exists || maintenance.Stale || maintenance.Removed {
		t.Fatalf("maintenance = %#v, want retained active lock", maintenance)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("active lock missing after cleanup: %v", err)
	}
}

func TestRebuildDuckDBIndexDeletesOversizedObsoleteArtifact(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	obsoletePath := ObsoleteCodeIntelArtifactPaths(root)[0]

	err := os.MkdirAll(filepath.Dir(obsoletePath), 0o700)
	if err != nil {
		t.Fatalf("create obsolete artifact dir: %v", err)
	}

	file, err := os.OpenFile(obsoletePath, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("create oversized obsolete artifact placeholder: %v", err)
	}
	err = file.Truncate(2 << 20)
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("close oversized obsolete artifact placeholder: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("truncate oversized obsolete artifact placeholder: %v", err)
	}

	summary, err := RebuildDuckDBIndex(ctx, root, "", "")
	if err != nil {
		t.Fatalf("rebuild DuckDB index: %v", err)
	}

	if len(summary.RemovedObsoleteArtifacts) != 1 ||
		summary.RemovedObsoleteArtifacts[0] != obsoletePath {
		t.Fatalf("unexpected obsolete artifact summary: %#v", summary)
	}

	if _, err := os.Stat(obsoletePath); !os.IsNotExist(err) {
		t.Fatalf("obsolete artifact should be removed: %v", err)
	}
}
