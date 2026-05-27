// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
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
		!healthTargetHasBiomarker(snapshot.Targets[0], "complex_function") ||
		!healthTargetHasBiomarker(snapshot.Targets[0], "structural_clone") {
		t.Fatalf("top target missing explainable biomarkers: %#v", snapshot.Targets[0])
	}

	stored, err := store.CodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		GitHead: "abc123",
		Limit:   5,
	})
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
	absoluteSourcePath := filepath.Join(root, "pkg", "legacy.py")
	if err := os.WriteFile(lcovPath, []byte(`SF:pkg/legacy.py
DA:1,1
DA:2,0
end_of_record
SF:`+absoluteSourcePath+`
DA:1,1
DA:2,1
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
	if !found || record.FoundLines != 2 || record.CoveredLines != 2 {
		t.Fatalf("LCOV record = %#v, found=%t", record, found)
	}
}

func TestCodeHealthDirectoryFilterUsesStoredSnapshotTargets(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openHealthTestStore(t, root)
	defer store.Close()

	indexHealthFixture(t, ctx, store)
	indexHealthOtherFixture(t, ctx, store)

	_, err := store.RefreshCodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		GitHead: "repo-wide",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("refresh repo health: %v", err)
	}

	stored, err := store.CodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		Path:    "pkg",
		GitHead: "repo-wide",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("read directory-filtered health: %v", err)
	}

	if len(stored.Targets) != 2 {
		t.Fatalf("directory-filtered target count = %d, want 2", len(stored.Targets))
	}
	for _, target := range stored.Targets {
		if !strings.HasPrefix(target.Path, "pkg/") {
			t.Fatalf("directory filter returned %q", target.Path)
		}
	}
}

func TestCodeHealthPathRefreshPersistsRepoWideSnapshot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openHealthTestStore(t, root)
	defer store.Close()

	indexHealthFixture(t, ctx, store)
	indexHealthOtherFixture(t, ctx, store)

	filtered, err := store.RefreshCodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		Path:    "pkg",
		GitHead: "filtered-refresh",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("refresh filtered health: %v", err)
	}
	if len(filtered.Targets) != 2 {
		t.Fatalf("filtered target count = %d, want 2", len(filtered.Targets))
	}

	unfiltered, err := store.CodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		GitHead: "filtered-refresh",
		Limit:   10,
	})
	if err != nil {
		t.Fatalf("read unfiltered health: %v", err)
	}
	if len(unfiltered.Targets) <= len(filtered.Targets) {
		t.Fatalf("path refresh poisoned repo snapshot: filtered=%d unfiltered=%d",
			len(filtered.Targets), len(unfiltered.Targets))
	}
}

func TestCodeHealthAutoGitHeadAndHeadSpecificReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openHealthTestStore(t, root)
	defer store.Close()

	runHealthTestGit(t, root, "init", "--initial-branch", "main")
	runHealthTestGit(t, root, "config", "user.name", "Test User")
	runHealthTestGit(t, root, "config", "user.email", "test@example.invalid")
	runHealthTestGit(t, root, "config", "commit.gpgsign", "false")

	sourcePath := filepath.Join(root, "pkg", "legacy.py")
	if err := os.MkdirAll(filepath.Dir(sourcePath), 0o700); err != nil {
		t.Fatalf("create source dir: %v", err)
	}
	if err := os.WriteFile(sourcePath, []byte("print('one')\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	runHealthTestGit(t, root, "add", "pkg/legacy.py")
	runHealthTestGit(t, root, "commit", "-m", "test(repo): add legacy")
	firstHead := strings.TrimSpace(runHealthTestGitOutput(t, root, "rev-parse", "HEAD"))

	indexHealthFixture(t, ctx, store)
	first, err := store.RefreshCodeHealth(ctx, CodeHealthQuery{Root: root, Limit: 5})
	if err != nil {
		t.Fatalf("refresh first head: %v", err)
	}
	if first.GitHead != firstHead {
		t.Fatalf("first snapshot git head = %q, want %q", first.GitHead, firstHead)
	}

	if err := os.WriteFile(sourcePath, []byte("print('two')\n"), 0o600); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	runHealthTestGit(t, root, "add", "pkg/legacy.py")
	runHealthTestGit(t, root, "commit", "-m", "test(repo): update legacy")
	secondHead := strings.TrimSpace(runHealthTestGitOutput(t, root, "rev-parse", "HEAD"))

	second, err := store.RefreshCodeHealth(ctx, CodeHealthQuery{Root: root, Limit: 5})
	if err != nil {
		t.Fatalf("refresh second head: %v", err)
	}
	if second.GitHead != secondHead {
		t.Fatalf("second snapshot git head = %q, want %q", second.GitHead, secondHead)
	}

	storedFirst, err := store.CodeHealth(ctx, CodeHealthQuery{
		Root:    root,
		GitHead: firstHead,
		Limit:   5,
	})
	if err != nil {
		t.Fatalf("read first head: %v", err)
	}
	if storedFirst.ID != first.ID {
		t.Fatalf("head-specific read returned %q, want %q", storedFirst.ID, first.ID)
	}
	if len(second.Trend) < 2 || second.Trend[0].GitHead != secondHead ||
		second.Trend[1].GitHead != firstHead {
		t.Fatalf("trend does not preserve commit sequence: %#v", second.Trend)
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

func indexHealthOtherFixture(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	raw := strings.Repeat("if value:\n    total += value\n", 12)
	chunk := CodeChunk{
		ID:             "chunk-other",
		Path:           "cmd/other.go",
		Language:       "go",
		NodeKind:       "function_declaration",
		SymbolKind:     "function",
		SymbolName:     "other",
		SymbolPath:     "other",
		ContentHash:    "hash-other-chunk",
		NormalizedHash: "",
		SearchText:     "other function",
		RawText:        raw,
		StartLine:      1,
		EndLine:        70,
	}
	err := store.ReplaceCodeFileIndex(ctx, CodeFile{
		Path:         "cmd/other.go",
		Language:     "go",
		ContentHash:  "hash-other",
		IndexedAtUTC: "2026-01-01T00:00:00Z",
		SizeBytes:    len(raw),
		LineCount:    80,
	}, []CodeChunk{chunk}, nil)
	if err != nil {
		t.Fatalf("index other fixture: %v", err)
	}
}

func runHealthTestGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runHealthTestGitOutput(t, dir, args...)
}

func runHealthTestGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := realgit.Command(context.Background(), false, args...)
	command.Dir = dir
	command.Env = realgit.CleanGitLocalEnv(os.Environ())
	command.Env = append(
		command.Env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=Test User",
		"GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test User",
		"GIT_COMMITTER_EMAIL=test@example.invalid",
	)

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}

	return string(output)
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
