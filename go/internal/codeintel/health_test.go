// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeHealthPersistsRankedSnapshotAndTrend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openHealthTestStore(t, root)
	defer store.Close()

	indexHealthFixture(t, ctx, store)

	snapshot, err := store.RefreshCodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		Limit:   5,
		GitHead: "abc123",
	})
	if err != nil {
		t.Fatalf("refresh health: %v", err)
	}

	if snapshot.ID == "" || snapshot.GitHead != "abc123" {
		t.Fatalf("snapshot metadata = %#v", snapshot)
	}
	if len(snapshot.Targets) == 0 {
		t.Fatalf("health targets empty")
	}
	if snapshot.Targets[0].Rank != 1 || snapshot.Targets[0].PriorityScore <= 0 {
		t.Fatalf("top target not ranked with priority: %#v", snapshot.Targets[0])
	}
	if !healthTargetHasBiomarker(snapshot.Targets[0], "large_function") ||
		!healthTargetHasBiomarker(snapshot.Targets[0], "structural_clone") {
		t.Fatalf("top target missing explainable biomarkers: %#v", snapshot.Targets[0])
	}

	stored, err := store.CodeHealth(ctx, CodeHealthQuery{Root: root, Limit: 5})
	if err != nil {
		t.Fatalf("read stored health: %v", err)
	}
	if len(stored.Trend) == 0 || stored.Trend[0].SnapshotID != snapshot.ID {
		t.Fatalf("stored trend missing latest snapshot: %#v", stored.Trend)
	}
}

func TestCodeHealthImportsLCOVAndSupportsPathOverrides(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openHealthTestStore(t, root)
	defer store.Close()

	indexHealthFixture(t, ctx, store)

	config := []byte(`code_intel:
  health:
    path_overrides:
      - glob: "**/legacy.py"
        disabled_biomarkers:
          - large_function
`)
	if err := os.WriteFile(
		filepath.Join(root, "repo_config.yaml"),
		config,
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	lcovPath := filepath.Join(root, "coverage.lcov")
	if err := os.WriteFile(lcovPath, []byte(`SF:pkg/legacy.py
DA:1,1
DA:2,0
end_of_record
`), 0o600); err != nil {
		t.Fatalf("write LCOV: %v", err)
	}

	snapshot, err := store.RefreshCodeHealth(ctx, CodeHealthQuery{
		Root:     root,
		Path:     "pkg/legacy.py",
		Limit:    5,
		LCOVPath: lcovPath,
	})
	if err != nil {
		t.Fatalf("refresh health with LCOV: %v", err)
	}

	if len(snapshot.Targets) != 1 {
		t.Fatalf("health target count = %d, want 1", len(snapshot.Targets))
	}
	if healthTargetHasBiomarker(snapshot.Targets[0], "large_function") {
		t.Fatalf("path override did not disable large_function: %#v", snapshot.Targets[0])
	}

	coverage := store.healthCoverage(ctx)
	record, found := coverage["pkg/legacy.py"]
	if !found || record.FoundLines != 2 || record.CoveredLines != 1 {
		t.Fatalf("LCOV record = %#v, found=%t", record, found)
	}
}

func openHealthTestStore(t *testing.T, root string) *Store {
	t.Helper()

	store, err := Open(context.Background(), filepath.Join(root, "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	return store
}

func indexHealthFixture(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	raw := strings.Repeat("    if value:\n        total += value\n", 35)
	chunk := CodeChunk{
		ID:             "chunk-legacy",
		Path:           "pkg/legacy.py",
		Language:       "python",
		NodeKind:       "function_definition",
		SymbolKind:     "function",
		SymbolName:     "legacy",
		SymbolPath:     "legacy",
		ContentHash:    "hash-legacy-chunk",
		NormalizedHash: "clone-normalized",
		SearchText:     "legacy function",
		RawText:        raw,
		StartLine:      1,
		EndLine:        100,
	}
	err := store.ReplaceCodeFileIndex(ctx, CodeFile{
		Path:         "pkg/legacy.py",
		Language:     "python",
		ContentHash:  "hash-legacy",
		IndexedAtUTC: "2026-01-01T00:00:00Z",
		SizeBytes:    len(raw),
		LineCount:    120,
	}, []CodeChunk{chunk}, nil)
	if err != nil {
		t.Fatalf("index legacy fixture: %v", err)
	}

	clone := chunk
	clone.ID = "chunk-copy"
	clone.Path = "pkg/copy.py"
	err = store.ReplaceCodeFileIndex(ctx, CodeFile{
		Path:         "pkg/copy.py",
		Language:     "python",
		ContentHash:  "hash-copy",
		IndexedAtUTC: "2026-01-01T00:00:00Z",
		SizeBytes:    len(raw),
		LineCount:    120,
	}, []CodeChunk{clone}, nil)
	if err != nil {
		t.Fatalf("index copy fixture: %v", err)
	}
}

func healthTargetHasBiomarker(target CodeHealthTarget, biomarker string) bool {
	for _, item := range target.Evidence {
		if item.Biomarker == biomarker {
			return true
		}
	}

	return false
}
