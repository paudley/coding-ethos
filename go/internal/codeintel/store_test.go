// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/astfacts"
	. "blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	codeChunkRecordKind = "code_chunk"
	vectorBackendName   = "sqlite-vec"
)

func TestStoreIngestsLintTracesAndReportsRepeatedFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	first := lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z")
	second := lintTracePayload(t, "trace-b.json", "2026-01-01T00:01:00Z")

	inlineErr0 := ingester.IngestLintTrace(ctx, first)
	if inlineErr0 != nil {
		t.Fatalf("ingest first trace: %v", inlineErr0)
	}

	inlineErr1 := ingester.IngestLintTrace(ctx, second)
	if inlineErr1 != nil {
		t.Fatalf("ingest second trace: %v", inlineErr1)
	}

	repeated, err := store.RepeatedFailures(ctx, RepeatedFailureQuery{
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Path:     "pkg/app.py",
	})
	if err != nil {
		t.Fatalf("query repeated failures: %v", err)
	}

	if len(repeated) != 1 {
		t.Fatalf("repeated failures = %#v", repeated)
	}

	if repeated[0].TraceCount != 2 || repeated[0].Count != 2 {
		t.Fatalf("repeated count = %#v", repeated[0])
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertStats(t, stats, Stats{
		Traces:            2,
		Findings:          1,
		Remediations:      1,
		RemediationEvents: 2,
		FtsRows:           4,
	})
}

func TestStoreSearchesRemediationText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	ingester := NewTraceIngester(store)

	inlineErr2 := ingester.IngestLintTrace(
		ctx,
		lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z"),
	)
	if inlineErr2 != nil {
		t.Fatalf("ingest trace: %v", inlineErr2)
	}

	results, err := store.Search(ctx, SearchQuery{Text: "unused", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected search results")
	}

	if results[0].TraceID != "trace-a.json" {
		t.Fatalf("search result = %#v", results[0])
	}
}

func TestStoreIngestTraceDirsFindsLintAndHookTraces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	ingester := NewTraceIngester(store)

	writeFile(
		t,
		filepath.Join(root, ".coding-ethos", "lint-runs", "trace-a.json"),
		lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z"),
	)
	writeFile(
		t,
		filepath.Join(root, ".coding-ethos", "hook-runs", "run-a", "event.json"),
		hookTracePayload(t),
	)

	summary, err := ingester.IngestTraceDirs(ctx, root)
	if err != nil {
		t.Fatalf("ingest trace dirs: %v", err)
	}

	if summary.FilesScanned != 2 || summary.FilesIngested != 2 {
		t.Fatalf("summary = %#v", summary)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.Traces != 2 {
		t.Fatalf("stats = %#v", stats)
	}

	summary, err = ingester.IngestTraceDirs(ctx, root)
	if err != nil {
		t.Fatalf("reingest trace dirs: %v", err)
	}

	if summary.FilesScanned != 2 || summary.FilesIngested != 0 {
		t.Fatalf("reingest summary = %#v", summary)
	}
}

func TestIngestHookTraceFilePreservesSourcePath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	tracePath := filepath.Join(root, ".coding-ethos", "hook-runs", "run-a", "event.json")

	writeFile(t, tracePath, hookTracePayload(t))

	err := IngestHookTraceFile(ctx, root, tracePath)
	if err != nil {
		t.Fatalf("ingest hook trace file: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	var sourcePath string

	err = store.Database().QueryRowContext(
		ctx,
		`SELECT COALESCE(source_path, '') FROM traces LIMIT 1`,
	).Scan(&sourcePath)
	if err != nil {
		t.Fatalf("query trace source path: %v", err)
	}

	if sourcePath != tracePath {
		t.Fatalf("source path = %q, want %q", sourcePath, tracePath)
	}
}

func TestSourcePathsIngestedChecksExactAndChildPathsInOneCall(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, ctx)
	parent := filepath.Join(root, ".coding-ethos", "lint-runs")
	child := filepath.Join(parent, "run-a.json")

	err := store.IngestTrace(ctx, Trace{
		ID:            "trace-a",
		Kind:          "lint",
		RecordedAtUTC: "2026-01-01T00:00:00Z",
		SourcePath:    child,
	})
	if err != nil {
		t.Fatalf("ingest trace: %v", err)
	}

	results, err := store.SourcePathsIngested(ctx, []SourcePathIngestRequest{
		{Path: child},
		{Path: parent, IncludeChildren: true},
		{Path: filepath.Join(root, ".coding-ethos", "missing")},
	})
	if err != nil {
		t.Fatalf("check source paths ingested: %v", err)
	}

	if !results[filepath.ToSlash(filepath.Clean(child))] {
		t.Fatalf("exact child source path not marked ingested: %#v", results)
	}
	if !results[filepath.ToSlash(filepath.Clean(parent))] {
		t.Fatalf("parent source path not marked ingested: %#v", results)
	}
	if results[filepath.ToSlash(filepath.Clean(filepath.Join(root, ".coding-ethos", "missing")))] {
		t.Fatalf("missing source path marked ingested: %#v", results)
	}
}

func TestStoreIngestTraceDirsBackfillsMissingTraceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	ingester := NewTraceIngester(store)

	writeFile(
		t,
		filepath.Join(root, ".coding-ethos", "hook-runs", "run-a", "event.json"),
		hookTracePayloadWithIDs(t, "", "deny-hook-a", "2026-01-01T00:02:00Z"),
	)

	summary, err := ingester.IngestTraceDirs(ctx, root)
	if err != nil {
		t.Fatalf("ingest trace dirs: %v", err)
	}

	if summary.FilesScanned != 1 || summary.FilesIngested != 1 {
		t.Fatalf("summary = %#v", summary)
	}

	usage, err := store.HookUsage(ctx, HookUsageQuery{Limit: 10})
	if err != nil {
		t.Fatalf("query hook usage: %v", err)
	}

	if len(usage) != 1 ||
		!strings.HasPrefix(usage[0].LastTraceID, "source-run-a-event.json-") {
		t.Fatalf("hook usage trace fallback = %#v", usage)
	}
}

func TestRefreshRepositoryRecordsDiffEditPatternsWithGitHeadAndAST(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700)
	if err != nil {
		t.Fatalf("create pkg dir: %v", err)
	}

	sourcePath := filepath.Join(root, "pkg", "app.py")

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'old'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	runCodeIntelGit(t, root, "add", "pkg/app.py")
	runCodeIntelGit(t, root, "commit", "-m", "initial")
	head := strings.TrimSpace(runCodeIntelGitOutput(t, root, "rev-parse", "HEAD"))

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'new'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("modify source: %v", err)
	}

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	patterns, err := store.RepeatedDiffEditPatterns(ctx, DiffEditPatternQuery{
		Path: "pkg/app.py",
	})
	if err != nil {
		t.Fatalf("query repeated edits: %v", err)
	}

	if len(patterns) != 1 {
		t.Fatalf("patterns = %#v, want one pattern", patterns)
	}

	pattern := patterns[0]
	if pattern.SeenCount != 2 ||
		pattern.FirstGitHead != head ||
		pattern.LastGitHead != head ||
		pattern.ASTSymbolPath != "build_message" ||
		pattern.RemovedSHA256 == "" ||
		pattern.AddedSHA256 == "" {
		t.Fatalf("pattern = %#v", pattern)
	}
}

func TestRefreshRepositoryRecordsDiffEditPatternsWithoutASTChunk(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	notePath := filepath.Join(root, "notes.txt")

	err := os.WriteFile(notePath, []byte("old\n"), 0o600)
	if err != nil {
		t.Fatalf("write note: %v", err)
	}

	runCodeIntelGit(t, root, "add", "notes.txt")
	runCodeIntelGit(t, root, "commit", "-m", "initial")

	err = os.WriteFile(notePath, []byte("new\n"), 0o600)
	if err != nil {
		t.Fatalf("modify note: %v", err)
	}

	_, err = RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}

	dbPath := DefaultDBPath(root)

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}
	defer database.Close()

	var count int

	err = database.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM diff_edit_patterns
		WHERE target_path = 'notes.txt' AND ast_chunk_id IS NULL`,
	).Scan(&count)
	if err != nil {
		t.Fatalf("query note edit pattern: %v", err)
	}

	if count != 1 {
		t.Fatalf("note edit patterns = %d, want 1", count)
	}

	patterns, err := store.RepeatedDiffEditPatterns(ctx, DiffEditPatternQuery{
		Path: "notes.txt",
	})
	if err != nil {
		t.Fatalf("query note edit pattern: %v", err)
	}

	if len(patterns) != 1 || patterns[0].ASTChunkID != "" {
		t.Fatalf("patterns = %#v, want one non-AST pattern", patterns)
	}
}

func TestRefreshRepositoryMarksDeletedFilesAndFiltersActiveAnalysis(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	err := os.MkdirAll(filepath.Join(root, "pkg"), 0o700)
	if err != nil {
		t.Fatalf("create pkg dir: %v", err)
	}

	sourcePath := filepath.Join(root, "pkg", "app.py")

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'old'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	runCodeIntelGit(t, root, "add", "pkg/app.py")
	runCodeIntelGit(t, root, "commit", "-m", "initial")

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'new'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("modify source: %v", err)
	}

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}

	err = os.Remove(sourcePath)
	if err != nil {
		t.Fatalf("remove source: %v", err)
	}

	summary, err := RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("delete refresh: %v", err)
	}

	if len(summary.CodeIndex.Deleted) != 1 ||
		summary.CodeIndex.Deleted[0] != "pkg/app.py" {
		t.Fatalf("deleted summary = %#v", summary.CodeIndex.Deleted)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("get deleted code file: %v", err)
	}

	if !found || file.DeletedAtUTC == "" || file.StaleReason != "deleted" {
		t.Fatalf("file = %#v, found = %v", file, found)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: "pkg/app.py"})
	if err != nil {
		t.Fatalf("query deleted code chunks: %v", err)
	}

	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v, want no active chunks", chunks)
	}

	searchResults, err := store.Search(ctx, SearchQuery{Text: "BuildMessage", Limit: 5})
	if err != nil {
		t.Fatalf("search deleted code chunks: %v", err)
	}

	if len(searchResults) != 0 {
		t.Fatalf("search results = %#v, want no deleted code chunks", searchResults)
	}

	patterns, err := store.RepeatedDiffEditPatterns(ctx, DiffEditPatternQuery{
		Path: "pkg/app.py",
	})
	if err != nil {
		t.Fatalf("query deleted diff edit patterns: %v", err)
	}

	if len(patterns) != 0 {
		t.Fatalf("patterns = %#v, want no active deleted-file patterns", patterns)
	}
}

func TestRefreshRepositoryMarksGitRMDeleteIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.py")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'old'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")
	runCodeIntelGit(t, root, "add", "pkg/app.py")
	runCodeIntelGit(t, root, "commit", "-m", "initial")

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("initial refresh: %v", err)
	}

	runCodeIntelGit(t, root, "rm", "pkg/app.py")
	err = os.MkdirAll(filepath.Join(root, "pkg"), 0o700)
	if err != nil {
		t.Fatalf("restore deleted parent dir: %v", err)
	}

	summary, err := RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("delete refresh: %v", err)
	}

	if len(summary.CodeIndex.Deleted) != 1 ||
		summary.CodeIndex.Deleted[0] != "pkg/app.py" {
		t.Fatalf("deleted summary = %#v", summary.CodeIndex.Deleted)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("get deleted code file: %v", err)
	}

	if !found || file.DeletedAtUTC == "" || file.StaleReason != "deleted_by_intent" {
		t.Fatalf("file = %#v, found = %v", file, found)
	}

	intents, err := store.CodeDeleteIntents(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("query delete intents: %v", err)
	}

	if len(intents) != 1 ||
		intents[0].IntentKind != "git_index_delete" ||
		intents[0].Status != "allowed" {
		t.Fatalf("delete intents = %#v", intents)
	}

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("second delete refresh: %v", err)
	}

	intents, err = store.CodeDeleteIntents(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("query delete intents after second refresh: %v", err)
	}

	if len(intents) != 1 {
		t.Fatalf("duplicate delete intents after second refresh: %#v", intents)
	}

	firstIntentID := intents[0].ID
	runCodeIntelGit(t, root, "commit", "-m", "delete app")

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'new'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("recreate source file: %v", err)
	}

	runCodeIntelGit(t, root, "add", "pkg/app.py")
	runCodeIntelGit(t, root, "commit", "-m", "restore app")

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("refresh recreated file: %v", err)
	}

	runCodeIntelGit(t, root, "rm", "pkg/app.py")
	err = os.MkdirAll(filepath.Join(root, "pkg"), 0o700)
	if err != nil {
		t.Fatalf("restore second deleted parent dir: %v", err)
	}

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("second delete cycle refresh: %v", err)
	}

	intents, err = store.CodeDeleteIntents(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("query delete intents after second delete cycle: %v", err)
	}

	if len(intents) != 2 {
		t.Fatalf("delete intents after second delete cycle = %#v", intents)
	}

	if intents[0].ID == intents[1].ID ||
		(intents[0].ID != firstIntentID && intents[1].ID != firstIntentID) {
		t.Fatalf("delete intent IDs do not preserve history: %#v", intents)
	}
}

func TestHookTraceDeleteIntentMarksMissingFileDeletedByIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.py")

	err := os.MkdirAll(filepath.Dir(sourcePath), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	err = os.WriteFile(
		sourcePath,
		[]byte("def build_message():\n    return 'old'\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write source: %v", err)
	}

	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")
	runCodeIntelGit(t, root, "add", "pkg/app.py")
	runCodeIntelGit(t, root, "commit", "-m", "initial")

	_, err = RefreshRepository(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("initial refresh: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	payload := hookTracePayloadWithCommand(
		t,
		"hook-delete-a",
		"rm pkg/app.py",
		"allowed",
		false,
	)
	err = NewTraceIngester(store).IngestHookTrace(ctx, payload)
	if err != nil {
		t.Fatalf("ingest hook trace: %v", err)
	}

	err = os.Remove(sourcePath)
	if err != nil {
		t.Fatalf("remove source: %v", err)
	}

	deleted, err := store.MarkMissingCodeFilesDeleted(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("mark deleted files: %v", err)
	}

	if len(deleted) != 1 || deleted[0] != "pkg/app.py" {
		t.Fatalf("deleted = %#v", deleted)
	}

	file, found, err := store.GetCodeFile(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("get deleted code file: %v", err)
	}

	if !found || file.StaleReason != "deleted_by_intent" {
		t.Fatalf("file = %#v, found = %v", file, found)
	}

	intents, err := store.CodeDeleteIntents(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("query delete intents: %v", err)
	}

	if len(intents) != 1 ||
		intents[0].IntentKind != "hook_command_delete" ||
		intents[0].TraceID != "hook-delete-a" {
		t.Fatalf("delete intents = %#v", intents)
	}
}

func TestBlockedHookTraceDoesNotRecordDeleteIntent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	payload := hookTracePayloadWithCommand(
		t,
		"hook-blocked-delete-a",
		"rm pkg/app.py",
		"blocked",
		true,
	)
	err = NewTraceIngester(store).IngestHookTrace(ctx, payload)
	if err != nil {
		t.Fatalf("ingest hook trace: %v", err)
	}

	intents, err := store.CodeDeleteIntents(ctx, "pkg/app.py")
	if err != nil {
		t.Fatalf("query delete intents: %v", err)
	}

	if len(intents) != 0 {
		t.Fatalf("blocked delete trace produced intents: %#v", intents)
	}
}

func TestRefreshRepositoryMarksIgnoredToolStateInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	err := os.MkdirAll(filepath.Join(root, ".wolf"), 0o700)
	if err != nil {
		t.Fatalf("create tool state dir: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(root, ".wolf", "buglog.json"),
		[]byte(`{"items":[]}`),
		0o600,
	)
	if err != nil {
		t.Fatalf("write tool state: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	err = store.ReplaceCodeFileIndex(
		ctx,
		CodeFile{
			Path:         ".wolf/buglog.json",
			Language:     "json",
			ContentHash:  "old",
			IndexedAtUTC: "2026-01-01T00:00:00Z",
			SizeBytes:    12,
			LineCount:    1,
		},
		[]CodeChunk{{
			ID:          "wolf-chunk",
			Path:        ".wolf/buglog.json",
			Language:    "json",
			NodeKind:    "object",
			ContentHash: "chunk",
			StartLine:   1,
			EndLine:     1,
			SearchText:  "items",
		}},
		nil,
	)
	if err != nil {
		t.Fatalf("seed ignored code index: %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Fatalf("close seeded store: %v", err)
	}

	summary, err := RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}

	if len(summary.CodeIndex.Deleted) != 1 ||
		summary.CodeIndex.Deleted[0] != ".wolf/buglog.json" {
		t.Fatalf("deleted summary = %#v", summary.CodeIndex.Deleted)
	}

	store, err = Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, ".wolf/buglog.json")
	if err != nil {
		t.Fatalf("get ignored code file: %v", err)
	}

	if !found || file.DeletedAtUTC == "" || file.StaleReason != "ignored" {
		t.Fatalf("file = %#v, found = %v", file, found)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: ".wolf/buglog.json"})
	if err != nil {
		t.Fatalf("query ignored chunks: %v", err)
	}

	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v, want no active ignored chunks", chunks)
	}
}

func TestRefreshRepositoryHonorsGitIgnore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	writeFile(t, filepath.Join(root, ".gitignore"), []byte("ignored/\n"))

	err := os.MkdirAll(filepath.Join(root, "ignored"), 0o700)
	if err != nil {
		t.Fatalf("create ignored dir: %v", err)
	}

	writeFile(
		t,
		filepath.Join(root, "ignored", "generated.py"),
		[]byte("def noisy():\n    return 1\n"),
	)
	writeFile(t, filepath.Join(root, "tracked.py"), []byte("def kept():\n    return 1\n"))

	_, err = RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	_, found, err := store.GetCodeFile(ctx, "ignored/generated.py")
	if err != nil {
		t.Fatalf("get ignored file: %v", err)
	}

	if found {
		t.Fatalf("ignored/generated.py should not be indexed")
	}

	file, found, err := store.GetCodeFile(ctx, "tracked.py")
	if err != nil {
		t.Fatalf("get tracked file: %v", err)
	}

	if !found || file.Language != "python" {
		t.Fatalf("tracked file = %#v, found = %v", file, found)
	}
}

func TestRefreshRepositoryHonorsGitIgnoreInAllIgnoredRepo(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")

	writeFile(t, filepath.Join(root, ".gitignore"), []byte("*\n"))
	writeFile(
		t,
		filepath.Join(root, "generated.py"),
		[]byte("def generated():\n    return 1\n"),
	)

	summary, err := RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}
	if summary.CodeIndex.FilesIndexed != 0 {
		t.Fatalf(
			"files indexed = %d, want no indexed ignored files",
			summary.CodeIndex.FilesIndexed,
		)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	_, found, err := store.GetCodeFile(ctx, "generated.py")
	if err != nil {
		t.Fatalf("get ignored file: %v", err)
	}
	if found {
		t.Fatal("generated.py should not be indexed in all-ignored repo")
	}
}

func TestRefreshRepositoryMarksOversizedSourcesInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	largeYAML := append([]byte("items:\n"), bytes.Repeat([]byte("  - value\n"), 140000)...)
	writeFile(t, filepath.Join(root, "large.yaml"), largeYAML)

	_, err := RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, "large.yaml")
	if err != nil {
		t.Fatalf("get large file: %v", err)
	}

	if !found || file.DeletedAtUTC == "" || file.StaleReason != "too_large" {
		t.Fatalf("large file = %#v, found = %v", file, found)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: "large.yaml"})
	if err != nil {
		t.Fatalf("query large file chunks: %v", err)
	}

	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v, want no active oversized chunks", chunks)
	}
}

func TestRefreshRepositoryMarksOverlongSourcesInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	writeFile(
		t,
		filepath.Join(root, "long.md"),
		bytes.Repeat([]byte("plain prose line\n"), 5001),
	)

	_, err := RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, "long.md")
	if err != nil {
		t.Fatalf("get long file: %v", err)
	}

	if !found || file.DeletedAtUTC == "" || file.StaleReason != "too_many_lines" {
		t.Fatalf("long file = %#v, found = %v", file, found)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: "long.md"})
	if err != nil {
		t.Fatalf("query long file chunks: %v", err)
	}

	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v, want no active overlong chunks", chunks)
	}
}

func TestRefreshRepositoryMarksDenseSourcesInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init", "--initial-branch", "main")
	runCodeIntelGit(t, root, "config", "user.email", "test@example.com")
	runCodeIntelGit(t, root, "config", "user.name", "Test User")

	var denseJSON strings.Builder
	denseJSON.WriteString("[\n")

	for index := range 2500 {
		_, err := fmt.Fprintf(&denseJSON, `  {"id": %d, "name": "item-%d"}`, index, index)
		if err != nil {
			t.Fatalf("write dense JSON entry: %v", err)
		}

		if index < 2499 {
			denseJSON.WriteString(",")
		}

		denseJSON.WriteString("\n")
	}

	denseJSON.WriteString("]\n")
	writeFile(t, filepath.Join(root, "dense.json"), []byte(denseJSON.String()))

	_, err := RefreshRepository(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("refresh repository: %v", err)
	}

	store, err := Open(ctx, DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	file, found, err := store.GetCodeFile(ctx, "dense.json")
	if err != nil {
		t.Fatalf("get dense file: %v", err)
	}

	if !found || file.DeletedAtUTC == "" || file.StaleReason != "too_many_chunks" {
		t.Fatalf("dense file = %#v, found = %v", file, found)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: "dense.json"})
	if err != nil {
		t.Fatalf("query dense file chunks: %v", err)
	}

	if len(chunks) != 0 {
		t.Fatalf("chunks = %#v, want no active dense chunks", chunks)
	}
}

func TestOpenMigratesColumnsBeforeIndexes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := DefaultDBPath(root)

	err := os.MkdirAll(filepath.Dir(dbPath), 0o700)
	if err != nil {
		t.Fatalf("create db dir: %v", err)
	}

	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}

	_, err = database.ExecContext(ctx, `CREATE TABLE code_chunks (
		chunk_id TEXT PRIMARY KEY,
		path TEXT NOT NULL,
		language TEXT NOT NULL,
		node_kind TEXT NOT NULL,
		symbol_kind TEXT,
		symbol_name TEXT,
		symbol_path TEXT,
		parent_chunk_id TEXT,
		start_byte INTEGER NOT NULL,
		end_byte INTEGER NOT NULL,
		start_line INTEGER NOT NULL,
		end_line INTEGER NOT NULL,
		content_hash TEXT NOT NULL,
		search_text TEXT NOT NULL,
		raw_text TEXT NOT NULL
	)`)
	if err != nil {
		t.Fatalf("create legacy code_chunks table: %v", err)
	}

	err = database.Close()
	if err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	for _, column := range []string{"normalized_hash", "minhash_sig"} {
		found, err := testColumnExists(ctx, store.Database(), "code_chunks", column)
		if err != nil {
			t.Fatalf("check migrated column %s: %v", column, err)
		}

		if !found {
			t.Fatalf("column %s was not migrated", column)
		}
	}
}

func TestOpenMigratesProxyTransformEvidencePath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := DefaultDBPath(root)

	err := os.MkdirAll(filepath.Dir(dbPath), 0o700)
	if err != nil {
		t.Fatalf("create db dir: %v", err)
	}

	database, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}

	_, err = database.ExecContext(ctx, `CREATE TABLE proxy_transforms (
		event_id TEXT NOT NULL,
		ordinal INTEGER NOT NULL,
		name TEXT NOT NULL,
		reason TEXT,
		input_hash TEXT,
		output_hash TEXT,
		policy_id TEXT,
		decision TEXT,
		input_tokens INTEGER NOT NULL DEFAULT 0,
		output_tokens INTEGER NOT NULL DEFAULT 0,
		bytes_removed INTEGER NOT NULL DEFAULT 0,
		findings_count INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY(event_id, ordinal)
	)`)
	if err != nil {
		t.Fatalf("create legacy proxy_transforms table: %v", err)
	}

	err = database.Close()
	if err != nil {
		t.Fatalf("close raw db: %v", err)
	}

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer store.Close()

	found, err := testColumnExists(
		ctx,
		store.Database(),
		"proxy_transforms",
		"evidence_path",
	)
	if err != nil {
		t.Fatalf("check migrated evidence_path column: %v", err)
	}
	if !found {
		t.Fatal("proxy_transforms.evidence_path was not migrated")
	}
}

func TestOpenCreatesProxyEvidencePathOnlyOnTransforms(t *testing.T) {
	t.Parallel()

	store, err := Open(context.Background(), DefaultDBPath(t.TempDir()))
	if err != nil {
		t.Fatalf("open fresh store: %v", err)
	}
	defer store.Close()

	transformColumn, err := testColumnExists(
		context.Background(),
		store.Database(),
		"proxy_transforms",
		"evidence_path",
	)
	if err != nil {
		t.Fatalf("check proxy_transforms evidence_path column: %v", err)
	}
	if !transformColumn {
		t.Fatal("proxy_transforms.evidence_path was not created")
	}

	eventColumn, err := testColumnExists(
		context.Background(),
		store.Database(),
		"proxy_events",
		"evidence_path",
	)
	if err != nil {
		t.Fatalf("check proxy_events evidence_path column: %v", err)
	}
	if eventColumn {
		t.Fatal("proxy_events.evidence_path should not exist")
	}
}

func testColumnExists(
	ctx context.Context,
	database *sql.DB,
	table string,
	name string,
) (bool, error) {
	rows, err := database.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, fmt.Errorf("query table info for %s: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal any
			pk         int
		)

		err = rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &pk)
		if err != nil {
			return false, fmt.Errorf("scan table info for %s: %w", table, err)
		}

		if columnName == name {
			return true, nil
		}
	}

	err = rows.Err()
	if err != nil {
		return false, fmt.Errorf("iterate table info for %s: %w", table, err)
	}

	return false, nil
}

func TestStoreIngestsHookUsageAnalytics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	ingester := NewTraceIngester(store)

	inlineErr3 := ingester.IngestHookTrace(ctx, hookTracePayload(t))
	if inlineErr3 != nil {
		t.Fatalf("ingest hook trace: %v", inlineErr3)
	}

	usage, err := store.HookUsage(ctx, HookUsageQuery{
		RiskCategory: "bypass",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("query hook usage: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("hook usage = %#v", usage)
	}

	assertHookUsageSummary(t, usage[0])

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertStats(t, stats, Stats{
		HookEvents:    1,
		HookDecisions: 1,
		HookTargets:   1,
		FtsRows:       3,
	})
}

func TestStoreIngestsHookUsageAcrossAgentProviders(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)

	for _, provider := range []string{"codex", "claude", "gemini"} {
		err := ingester.IngestHookTrace(ctx, hookTracePayloadForProvider(t, provider))
		if err != nil {
			t.Fatalf("ingest %s hook trace: %v", provider, err)
		}

		usage, queryErr := store.HookUsage(ctx, HookUsageQuery{
			Provider: provider,
			Status:   "blocked",
			Limit:    10,
		})
		if queryErr != nil {
			t.Fatalf("query %s hook usage: %v", provider, queryErr)
		}

		if len(usage) != 1 ||
			usage[0].Provider != provider ||
			usage[0].BlockedCount != 1 ||
			usage[0].LastTraceID == "" ||
			usage[0].LastTrackingID == "" {
			t.Fatalf("%s usage = %#v", provider, usage)
		}
	}
}

func assertHookUsageSummary(t *testing.T, usage HookUsageSummary) {
	t.Helper()

	got := []any{
		usage.EventCount,
		usage.BlockedCount,
		usage.PolicyID,
		usage.OperationKind,
		usage.TargetKind,
		usage.LastTrackingID,
	}

	want := []any{
		1,
		1,
		"git.wrapper_required",
		"git_status",
		"source_file",
		"deny-hook-a",
	}
	if !stringAnySlicesEqual(got, want) {
		t.Fatalf("hook usage summary = %#v", usage)
	}
}

func TestHookUsageLastIDsComeFromLatestEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	older := hookTracePayloadWithIDs(
		t,
		"hook-z",
		"deny-z",
		"2026-01-01T00:01:00Z",
	)

	newer := hookTracePayloadWithIDs(
		t,
		"hook-a",
		"deny-a",
		"2026-01-01T00:02:00Z",
	)

	inlineErr4 := ingester.IngestHookTrace(ctx, older)
	if inlineErr4 != nil {
		t.Fatalf("ingest older hook trace: %v", inlineErr4)
	}

	inlineErr5 := ingester.IngestHookTrace(ctx, newer)
	if inlineErr5 != nil {
		t.Fatalf("ingest newer hook trace: %v", inlineErr5)
	}

	usage, err := store.HookUsage(ctx, HookUsageQuery{
		RiskCategory: "bypass",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("query hook usage: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("hook usage = %#v", usage)
	}

	if usage[0].EventCount != 2 ||
		usage[0].LastSeenUTC != "2026-01-01T00:02:00Z" ||
		usage[0].LastTraceID != "hook-a" ||
		usage[0].LastTrackingID != "deny-a" {
		t.Fatalf("latest IDs not tied to newest event: %#v", usage[0])
	}
}

func TestStoreRecordsHookReviews(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr6 := NewTraceIngester(
		store,
	).IngestHookTrace(ctx, hookTracePayload(t))
	if inlineErr6 != nil {
		t.Fatalf("ingest hook trace: %v", inlineErr6)
	}

	inlineErr7 := store.RecordHookReview(ctx, HookReview{
		TraceID:       "hook-trace-a",
		TrackingID:    "deny-hook-a",
		Disposition:   "false_positive",
		Reviewer:      "admin",
		Notes:         "memory path should be allowed",
		RecordedAtUTC: "2026-01-01T00:03:00Z",
	})
	if inlineErr7 != nil {
		t.Fatalf("record hook review: %v", inlineErr7)
	}

	reviews, err := store.HookReviews(ctx, HookReviewQuery{
		Disposition: "false_positive",
	})
	if err != nil {
		t.Fatalf("query hook reviews: %v", err)
	}

	if len(reviews) != 1 || reviews[0].TrackingID != "deny-hook-a" ||
		reviews[0].Notes != "memory path should be allowed" {
		t.Fatalf("reviews = %#v", reviews)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.HookReviews != 1 || stats.FtsRows != 4 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStoreIngestsSARIFResultsWithCELProvenance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	payload := sarifPayload(t)

	inlineErr8 := NewTraceIngester(
		store,
	).IngestSARIF(ctx, "policy.sarif", payload)
	if inlineErr8 != nil {
		t.Fatalf("ingest SARIF: %v", inlineErr8)
	}

	results, err := store.SARIFResults(ctx, SARIFResultQuery{
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Path:     "pkg/app.py",
	})
	if err != nil {
		t.Fatalf("query SARIF results: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("SARIF results = %#v", results)
	}

	result := results[0]
	if result.EvaluatorKind != "cel" ||
		result.CELExpression != "finding.policy_id == 'python.unused_imports'" {
		t.Fatalf("SARIF CEL provenance = %#v", result)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.SARIFRuns != 1 || stats.SARIFResults != 1 || stats.FtsRows != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestDecodeSARIFRunsMergesRuleResultAndFindingMetadata(t *testing.T) {
	t.Parallel()

	run, err := DecodeSARIFRun("policy.sarif", mergedMetadataSARIFPayload())
	if err != nil {
		t.Fatalf("decode SARIF run: %v", err)
	}

	assertDecodedSARIFRunMetadata(t, run)

	if len(run.Results) != 1 {
		t.Fatalf("results = %#v", run.Results)
	}

	assertDecodedSARIFResultMetadata(t, run.Results[0])
}

func mergedMetadataSARIFPayload() []byte {
	return []byte(`{
			"version":"2.1.0",
			"runs":[{
				"automationDetails":{"id":"policy","guid":"run-guid"},
			"baselineGuid":"base-guid",
			"properties":{"scope":"staged"},
			"tool":{"driver":{"name":"coding-ethos","rules":[{
				"id":"R1",
				"properties":{
					"policy_id":"rule.policy",
					"skill_id":"rule-skill",
					"source_tool":"ruff",
					"ethos_ids":["static-analysis"],
					"advice":"rule advice"
				}
			}]}},
			"results":[{
				"ruleId":"R1",
				"level":"error",
				"message":{"text":"result message"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"pkg/app.py"},
						"region":{"startLine":7,"startColumn":3}
					}
				}],
				"partialFingerprints":{"primaryLocationLineHash":"line-hash"},
				"properties":{
					"finding":{
						"id":"finding-1",
						"policy_id":"finding.policy",
						"skill_id":"finding-skill",
						"evaluator_kind":"cel",
						"search_text":"finding search",
						"principle_ids":["no-conditional-imports"]
					},
					"agent_remediation":[{
						"id":"rem-1",
						"policy_id":"finding.policy",
						"skill_id":"finding-skill",
						"message":"Move import",
						"advice":"Use module scope",
						"file":"pkg/app.py"
					}],
					"policy_id":"result.policy",
					"skill_id":"result-skill",
					"implementation":"cel",
					"cel_expression":"diagnostic.code == 'PLC0415'",
					"policy_source":"coding_ethos.yml:principles.3",
					"ast_language":"python",
					"ast_node_kind":"import_statement",
					"ast_symbol_kind":"import",
					"ast_symbol_name":"plugin",
					"ast_symbol_path":"plugin",
					"proxy_event_id":"proxy-event-1",
					"proxy_session_id":"proxy-session-1",
					"proxy_event_kind":"provider_call",
					"proxy_direction":"outbound",
					"proxy_payload_kind":"prompt",
					"proxy_trace_id":"trace-1",
					"proxy_tracking_id":"track-1",
					"proxy_transform":"dlp-inspection",
					"ethos_ids":["conditional-imports"]
				}
				}]
			}]
		}`)
}

func assertDecodedSARIFRunMetadata(t *testing.T, run SARIFRun) {
	t.Helper()

	got := []any{
		run.SourcePath,
		run.Category,
		run.ToolName,
		run.AutomationID,
		run.RunGUID,
		run.BaselineGUID,
	}

	want := []any{
		"policy.sarif",
		"staged",
		"coding-ethos",
		"policy",
		"run-guid",
		"base-guid",
	}
	if !stringAnySlicesEqual(got, want) {
		t.Fatalf("run metadata = %#v", run)
	}
}

func assertDecodedSARIFResultMetadata(t *testing.T, result SARIFResultReference) {
	t.Helper()

	got := []any{
		result.FindingID,
		result.RemediationID,
		result.PolicyID,
		result.SkillID,
		result.EvaluatorKind,
		result.CELExpression,
		result.PolicySource,
		result.Path,
		result.StartLine,
		result.StartColumn,
		result.Fingerprint,
		result.ASTLanguage,
		result.ASTSymbolPath,
		result.ProxyEventID,
		result.ProxySessionID,
		result.ProxyDirection,
		result.ProxyPayloadKind,
		result.ProxyTraceID,
		result.ProxyTrackingID,
		result.ProxyTransform,
		strings.Contains(result.SearchText, "Move import"),
		strings.Contains(result.SearchText, "Use module scope"),
		containsJoined(result.PrincipleIDs, "no-conditional-imports"),
		containsJoined(result.PrincipleIDs, "conditional-imports"),
	}

	want := []any{
		"finding-1",
		"rem-1",
		"result.policy",
		"result-skill",
		"cel",
		"diagnostic.code == 'PLC0415'",
		"coding_ethos.yml:principles.3",
		"pkg/app.py",
		7,
		3,
		"line-hash",
		"python",
		"plugin",
		"proxy-event-1",
		"proxy-session-1",
		"outbound",
		"prompt",
		"trace-1",
		"track-1",
		"dlp-inspection",
		true,
		true,
		true,
		true,
	}
	if !stringAnySlicesEqual(got, want) {
		t.Fatalf("result metadata = %#v", result)
	}
}

func TestDecodeSARIFRunsRejectsMalformedLogs(t *testing.T) {
	t.Parallel()

	_, inlineErrAutoA := DecodeSARIFRuns("bad.sarif", []byte("{"))
	if inlineErrAutoA == nil {
		t.Fatal("expected malformed SARIF decode error")
	}

	_, inlineErrAutoB := DecodeSARIFRuns(
		"empty.sarif",
		[]byte(`{"version":"2.1.0","runs":[]}`),
	)
	if inlineErrAutoB == nil {
		t.Fatal("expected no-runs SARIF error")
	}
}

func TestStoreQueriesMigratedSARIFResultsWithNullASTColumns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "code-intel.db")
	store := openTestStoreAt(t, ctx, path)
	rawDatabase := openRawSQLite(t, path)

	_, inlineErrA := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO sarif_runs(
			sarif_run_id, source_path, category, tool_name, raw_json
		) VALUES ('old-run', 'old.sarif', 'policy', 'coding-ethos', '{}')`,
	)
	if inlineErrA != nil {
		t.Fatalf("insert old SARIF run: %v", inlineErrA)
	}

	_, inlineErrB := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO sarif_results(
			sarif_result_id, sarif_run_id, ordinal, rule_id, message,
			level, fingerprint, finding_id, remediation_id, policy_id, skill_id,
			principle_ids, path, start_line, start_column, evaluator_kind,
			cel_policy_id, cel_expression, policy_source, search_text, raw_json
		) VALUES (
			'old-result', 'old-run', 0, 'old.rule', 'old message',
			'warning', '', '', '', '', '',
			'', 'pkg/app.py', 1, 1, '',
			'', '', '', 'old message', '{}'
		)`,
	)
	if inlineErrB != nil {
		t.Fatalf("insert old SARIF result: %v", inlineErrB)
	}

	results, err := store.SARIFResults(ctx, SARIFResultQuery{RunID: "old-run"})
	if err != nil {
		t.Fatalf("query old SARIF results: %v", err)
	}

	if len(results) != 1 || results[0].ASTLanguage != "" ||
		results[0].LinkedChunkID != "" {
		t.Fatalf("SARIF results = %#v", results)
	}
}

func TestStoreRecordsRemediationOutcomesAndEmbeddingMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	recordRemediationOutcomeFixture(t, ctx, store)
	assertRecordedRemediationOutcome(t, ctx, store)
	assertRecordedEmbeddingMetadata(t, ctx, store)

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertStats(t, stats, Stats{
		RemediationOutcomes: 1,
		EmbeddingRecords:    1,
		FtsRows:             6,
	})
}

func recordRemediationOutcomeFixture(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	ingester := NewTraceIngester(store)

	err := ingester.IngestLintTrace(
		ctx,
		lintTracePayload(t, "trace-a", "2026-01-01T00:00:00Z"),
	)
	if err != nil {
		t.Fatalf("ingest source trace: %v", err)
	}

	err = ingester.IngestLintTrace(
		ctx,
		lintTracePayload(t, "trace-b", "2026-01-01T00:01:00Z"),
	)
	if err != nil {
		t.Fatalf("ingest follow-up trace: %v", err)
	}

	err = store.RecordRemediationOutcome(ctx, RemediationOutcome{
		RemediationID:   "rem-1",
		FindingID:       "finding-1",
		SourceTraceID:   "trace-a",
		FollowupTraceID: "trace-b",
		PolicyID:        "python.unused_imports",
		SkillID:         "lint-remediation",
		Path:            "pkg/app.py",
		Provider:        "codex",
		Tool:            "Edit",
		Outcome:         "fixed",
		AttemptOrdinal:  1,
	})
	if err != nil {
		t.Fatalf("record outcome: %v", err)
	}

	err = store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:      vectorBackendName,
		Collection:   "remediations",
		ModelID:      "voyage-code-3",
		RecordKind:   "remediation_outcome",
		RecordID:     "rem-1",
		Dimension:    1024,
		PolicyID:     "python.unused_imports",
		SkillID:      "lint-remediation",
		Path:         "pkg/app.py",
		BackendRowID: "sqlite-vec-row-1",
	})
	if err != nil {
		t.Fatalf("record embedding: %v", err)
	}
}

func assertRecordedRemediationOutcome(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	outcomes, err := store.RemediationOutcomes(ctx, RemediationOutcomeQuery{
		Outcome: "fixed",
	})
	if err != nil {
		t.Fatalf("query outcomes: %v", err)
	}

	if len(outcomes) != 1 || outcomes[0].RemediationID != "rem-1" {
		t.Fatalf("outcomes = %#v", outcomes)
	}

	effectiveness, err := store.RemediationEffectiveness(ctx, RemediationOutcomeQuery{})
	if err != nil {
		t.Fatalf("effectiveness: %v", err)
	}

	if len(effectiveness) != 1 || effectiveness[0].Fixed != 1 ||
		effectiveness[0].Total != 1 {
		t.Fatalf("effectiveness = %#v", effectiveness)
	}
}

func assertRecordedEmbeddingMetadata(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	embeddingRecords, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		Backend: vectorBackendName,
		ModelID: "voyage-code-3",
	})
	if err != nil {
		t.Fatalf("embedding records: %v", err)
	}

	if len(embeddingRecords) != 1 ||
		embeddingRecords[0].BackendRowID != "sqlite-vec-row-1" {
		t.Fatalf("embedding records = %#v", embeddingRecords)
	}
}

func TestStoreReturnsEmbeddingCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr13 := NewTraceIngester(
		store,
	).IngestSARIF(ctx, "policy.sarif", sarifPayload(t))
	if inlineErr13 != nil {
		t.Fatalf("ingest SARIF: %v", inlineErr13)
	}

	inlineErr14 := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-1",
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
	})
	if inlineErr14 != nil {
		t.Fatalf("record outcome: %v", inlineErr14)
	}

	candidates, err := store.EmbeddingCandidates(ctx, EmbeddingCandidateQuery{
		PolicyID: "python.unused_imports",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("embedding candidates: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v", candidates)
	}

	for _, candidate := range candidates {
		if candidate.Text == "" ||
			candidate.Metadata["policy_id"] != "python.unused_imports" {
			t.Fatalf("candidate = %#v", candidate)
		}
	}
}

func TestStoreQueriesOutcomeWithoutTraceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr15 := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-no-trace",
		RemediationID: "rem-no-trace",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "attempted",
	})
	if inlineErr15 != nil {
		t.Fatalf("record outcome: %v", inlineErr15)
	}

	outcomes, err := store.RemediationOutcomes(ctx, RemediationOutcomeQuery{
		PolicyID: "python.unused_imports",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("query outcomes: %v", err)
	}

	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %#v", outcomes)
	}

	if outcomes[0].SourceTraceID != "" || outcomes[0].FollowupTraceID != "" {
		t.Fatalf("trace IDs = %#v", outcomes[0])
	}
}

func TestStoreIngestsAllSARIFRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr16 := NewTraceIngester(
		store,
	).IngestSARIF(ctx, "multi.sarif", multiRunSARIFPayload())
	if inlineErr16 != nil {
		t.Fatalf("ingest SARIF: %v", inlineErr16)
	}

	first, err := store.SARIFResults(
		ctx,
		SARIFResultQuery{PolicyID: "policy.first", Limit: 10},
	)
	if err != nil {
		t.Fatalf("query first SARIF results: %v", err)
	}

	second, err := store.SARIFResults(
		ctx,
		SARIFResultQuery{PolicyID: "policy.second", Limit: 10},
	)
	if err != nil {
		t.Fatalf("query second SARIF results: %v", err)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("first = %#v; second = %#v", first, second)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.SARIFRuns != 2 || stats.SARIFResults != 2 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestASTIndexerStoresCodeChunksAsSearchableEmbeddingCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.go")
	writeFile(t, sourcePath, []byte(`package pkg

func BuildMessage(name string) string {
	return "hello " + name
}

type Worker struct{}

func (worker Worker) Run() string {
	return BuildMessage("agent")
}
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	assertIndexedSummary(t, summary)

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/app.go",
		SymbolKind: "function",
		SymbolName: "BuildMessage",
	})
	if err != nil {
		t.Fatalf("query code chunks: %v", err)
	}

	assertBuildMessageChunk(t, chunks)

	candidates, err := store.EmbeddingCandidates(ctx, EmbeddingCandidateQuery{
		RecordKind: codeChunkRecordKind,
		Path:       "pkg/app.go",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("embedding candidates: %v", err)
	}

	if len(candidates) < 3 {
		t.Fatalf("candidates = %#v", candidates)
	}

	if candidates[0].Metadata["record_kind"] != codeChunkRecordKind {
		t.Fatalf("candidate = %#v", candidates[0])
	}

	searchResults, err := store.Search(ctx, SearchQuery{Text: "BuildMessage", Limit: 5})
	if err != nil {
		t.Fatalf("search code chunks: %v", err)
	}

	if len(searchResults) == 0 || searchResults[0].Kind != codeChunkRecordKind {
		t.Fatalf("search results = %#v", searchResults)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertMinimumStats(t, stats, Stats{
		Files:      1,
		CodeChunks: 3,
		FtsRows:    3,
	})
}

func TestASTIndexerInvalidatesStaleCodeChunkEmbeddingRecords(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.go")
	writeFile(t, sourcePath, []byte(`package pkg

func BuildMessage(name string) string {
	return "hello " + name
}
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/app.go",
		SymbolName: "BuildMessage",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query original chunk: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("original chunks = %#v", chunks)
	}

	err = store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:      vectorBackendName,
		Collection:   "code_chunks",
		ModelID:      "voyage-code-3",
		RecordKind:   codeChunkRecordKind,
		RecordID:     chunks[0].ID,
		Path:         chunks[0].Path,
		ContentHash:  chunks[0].ContentHash,
		Dimension:    1024,
		BackendRowID: "sqlite-vec-row-code",
	})
	if err != nil {
		t.Fatalf("record code chunk embedding: %v", err)
	}

	originalRecords, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		RecordKind: codeChunkRecordKind,
		RecordID:   chunks[0].ID,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query original embedding records: %v", err)
	}

	if len(originalRecords) != 1 {
		t.Fatalf("original embedding records = %#v", originalRecords)
	}

	writeFile(t, sourcePath, []byte(`package pkg

func BuildMessage(name string) string {
	return "hi " + name
}
`))

	_, err = NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("reindex code: %v", err)
	}

	records, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		RecordKind: codeChunkRecordKind,
		RecordID:   chunks[0].ID,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query stale embedding records: %v", err)
	}

	if len(records) != 0 {
		t.Fatalf("stale embedding records = %#v", records)
	}

	searchResults, err := store.Search(ctx, SearchQuery{
		Text:  "code_chunk",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("search stale embedding FTS: %v", err)
	}

	for _, result := range searchResults {
		if result.Kind == "embedding_record" && result.RecordID == originalRecords[0].ID {
			t.Fatalf("stale embedding FTS result = %#v", result)
		}
	}
}

func TestASTIndexerReindexesSameSizeContentWithPreservedModTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.go")
	originalSource := []byte(`package pkg

func BuildMessage(name string) string {
	return "hello " + name
}
`)
	updatedSource := []byte(`package pkg

func BuildMessage(name string) string {
	return "hullo " + name
}
`)
	if len(originalSource) != len(updatedSource) {
		t.Fatalf(
			"test sources must be same size: %d != %d",
			len(originalSource),
			len(updatedSource),
		)
	}

	writeFile(t, sourcePath, originalSource)
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index original code: %v", err)
	}

	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatalf("stat original source: %v", err)
	}

	originalChunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/app.go",
		SymbolName: "BuildMessage",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query original chunk: %v", err)
	}

	if len(originalChunks) != 1 {
		t.Fatalf("original chunks = %#v", originalChunks)
	}

	err = store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:     vectorBackendName,
		Collection:  "code_chunks",
		ModelID:     "voyage-code-3",
		RecordKind:  codeChunkRecordKind,
		RecordID:    originalChunks[0].ID,
		Path:        originalChunks[0].Path,
		ContentHash: originalChunks[0].ContentHash,
		Dimension:   1024,
	})
	if err != nil {
		t.Fatalf("record original chunk embedding: %v", err)
	}

	writeFile(t, sourcePath, updatedSource)
	err = os.Chtimes(sourcePath, sourceInfo.ModTime(), sourceInfo.ModTime())
	if err != nil {
		t.Fatalf("preserve source mtime: %v", err)
	}

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("reindex same-size preserved-mtime code: %v", err)
	}

	if summary.FilesIndexed != 1 || len(summary.Skipped) != 0 {
		t.Fatalf("same-size preserved-mtime summary = %#v", summary)
	}

	updatedChunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/app.go",
		SymbolName: "BuildMessage",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query updated chunk: %v", err)
	}

	if len(updatedChunks) != 1 {
		t.Fatalf("updated chunks = %#v", updatedChunks)
	}

	if updatedChunks[0].ContentHash == originalChunks[0].ContentHash ||
		!strings.Contains(updatedChunks[0].RawText, `"hullo "`) {
		t.Fatalf("updated chunk = %#v, original = %#v", updatedChunks[0], originalChunks[0])
	}

	records, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		RecordKind: codeChunkRecordKind,
		RecordID:   originalChunks[0].ID,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query stale embedding records: %v", err)
	}

	if len(records) != 0 {
		t.Fatalf("stale embedding records = %#v", records)
	}
}

func TestStoreBuildsDirectoryAnatomyMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "pkg", "app.go"), []byte(`package pkg

func BuildMessage(name string) string {
	return "hello " + name
}

type Worker struct{}
`))
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(`def run_worker():
    return "ok"
`))
	writeFile(t, filepath.Join(root, "pkg", "sub", "deep.go"), []byte(`package sub

func Hidden() {}
`))
	writeFile(t, filepath.Join(root, "pkg", "aaa", "nested.go"), []byte(`package aaa

func NestedA() {}

func NestedB() {}
`))
	writeFile(
		t,
		filepath.Join(root, "pkg", "aaa", "deeper", "nested.go"),
		[]byte(`package deeper

func TooDeep() {}
`),
	)
	writeFile(t, filepath.Join(root, "pkg", "zz.go"), []byte(`package pkg

func LastDirect() {}
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	anatomy, err := store.DirectoryAnatomy(ctx, DirectoryAnatomyQuery{
		Path:           "pkg",
		SymbolsPerFile: 2,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("directory anatomy: %v", err)
	}

	if anatomy.Path != "pkg" || len(anatomy.Files) != 3 {
		t.Fatalf("anatomy = %#v", anatomy)
	}

	app := anatomyFileByPath(anatomy.Files, "pkg/app.go")
	if app == nil ||
		app.Language != "go" ||
		app.EstimatedTokens == 0 ||
		app.SymbolCount == 0 ||
		!anatomyHasSymbol(*app, "BuildMessage") {
		t.Fatalf("app anatomy = %#v", app)
	}

	if anatomyFileByPath(anatomy.Files, "pkg/sub/deep.go") != nil {
		t.Fatalf("anatomy included nested file: %#v", anatomy.Files)
	}

	limited, err := store.DirectoryAnatomy(ctx, DirectoryAnatomyQuery{
		Path:           "pkg",
		SymbolsPerFile: 1,
		Limit:          3,
	})
	if err != nil {
		t.Fatalf("limited directory anatomy: %v", err)
	}

	worker := anatomyFileByPath(limited.Files, "pkg/worker.py")
	if worker == nil || !anatomyHasSymbol(*worker, "run_worker") {
		t.Fatalf("worker anatomy = %#v", worker)
	}

	if anatomyFileByPath(limited.Files, "pkg/aaa/nested.go") != nil {
		t.Fatalf("limited anatomy included nested file: %#v", limited.Files)
	}

	recursive, err := store.DirectoryAnatomy(ctx, DirectoryAnatomyQuery{
		Path:           "pkg",
		IncludeNested:  true,
		MaxDepth:       2,
		SymbolsPerFile: 1,
		Limit:          10,
	})
	if err != nil {
		t.Fatalf("recursive directory anatomy: %v", err)
	}

	deep := anatomyFileByPath(recursive.Files, "pkg/sub/deep.go")
	if deep == nil || !anatomyHasSymbol(*deep, "Hidden") {
		t.Fatalf("recursive anatomy missed nested file: %#v", recursive.Files)
	}

	if anatomyFileByPath(recursive.Files, "pkg/aaa/deeper/nested.go") != nil {
		t.Fatalf("recursive anatomy exceeded depth: %#v", recursive.Files)
	}

	output, err := store.EnrichDirectoryListing(
		ctx,
		DirectoryAnatomyQuery{
			Path:           "pkg",
			SymbolsPerFile: 1,
			Limit:          10,
		},
		"app.go\nworker.py\nsub\n",
	)
	if err != nil {
		t.Fatalf("enrich directory listing: %v", err)
	}

	if !strings.HasPrefix(output.Text, "app.go\nworker.py\nsub\n") ||
		!strings.Contains(output.Text, "coding_ethos_anatomy:") ||
		output.Record.Name != DirectoryAnatomyTransformName ||
		output.Record.Decision != "inject" {
		t.Fatalf("enriched listing = %#v", output)
	}
}

func TestASTIndexerStoresParentEdgesAndContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(`import os

def helper():
    return "ok"

def a():
    return "a"

def load_a_config():
    return "config"

class Worker:
    def run(self):
        return helper()

    def stop(self):
        return "stopped"
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	assertIndexedSummary(t, summary)

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/worker.py",
		SymbolPath: "Worker.run",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query run chunk: %v", err)
	}

	assertWorkerRunChunk(t, chunks)

	context, err := store.CodeContext(ctx, CodeContextQuery{
		Path:       "pkg/worker.py",
		Root:       root,
		SymbolPath: "Worker.run",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("code context: %v", err)
	}

	assertWorkerRunContext(t, context)
	assertStaleCodeContextRefusesChangedSource(t, ctx, root, store)
	assertWorkerLineAndConfigContext(t, ctx, store)
	assertWorkerImportEdge(t, ctx, store)

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertMinimumStats(t, stats, Stats{CodeEdges: 1})
}

func TestASTIndexerReturnsCompactCodeContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"ok\"\n\n"+
			"class Worker:\n"+
			"    def run(self):\n"+
			"        return helper()\n",
	))
	writeFile(t, filepath.Join(root, ".codex", "skills", "generated", "SKILL.md"), []byte(
		"# Generated Skill\n\n"+
			"Use generated agent context.\n",
	))
	writeFile(
		t,
		filepath.Join(root, ".venv", "lib", "python", "site-packages", "pkg.py"),
		[]byte(
			"def lib():\n"+
				"    return \"ignored\"\n",
		),
	)
	writeFile(t, filepath.Join(root, "coding-ethos", "go", "internal", "tool.go"), []byte(
		"package internal\n\n"+
			"func GeneratedTool() {}\n",
	))
	writeFile(t, filepath.Join(root, "ruff.toml"), []byte(
		"line-length = 100\n",
	))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	context, err := store.CompactCodeContext(ctx, CompactCodeContextQuery{
		Path:  "pkg/worker.py",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("compact context: %v", err)
	}

	if !context.IndexFresh ||
		len(context.RepoMap) != 1 ||
		len(context.Symbols) == 0 ||
		len(context.Chunks) == 0 ||
		context.Symbols[0].TokenSize == 0 {
		t.Fatalf("compact context = %#v", context)
	}
}

func TestASTIndexerReturnsGlobalRepoMap(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	runCodeIntelGit(t, root, "init")
	writeFile(t, filepath.Join(root, "cmd", "main.go"), []byte(`package main

import "fmt"

func main() { fmt.Println("x"); fmt.Println("y") }
`))
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"ok\"\n\n"+
			"class Worker:\n"+
			"    def run(self):\n"+
			"        return helper()\n",
	))
	writeFile(t, filepath.Join(root, ".codex", "skills", "generated", "SKILL.md"), []byte(
		"# Generated Skill\n\n"+
			"Use generated agent context.\n",
	))
	writeFile(
		t,
		filepath.Join(root, ".venv", "lib", "python", "site-packages", "pkg.py"),
		[]byte(
			"def lib():\n"+
				"    return \"ignored\"\n",
		),
	)
	writeFile(t, filepath.Join(root, "coding-ethos", "go", "internal", "tool.go"), []byte(
		"package internal\n\n"+
			"func GeneratedTool() {}\n",
	))
	writeFile(t, filepath.Join(root, "ignored", "cache.py"), []byte(
		"def ignored_cache():\n"+
			"    return \"ignored\"\n",
	))
	writeFile(t, filepath.Join(root, "ruff.toml"), []byte(
		"line-length = 100\n",
	))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"."})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}
	writeFile(t, filepath.Join(root, ".gitignore"), []byte("ignored/\n"))

	repoMap, err := store.GlobalRepoMap(ctx, RepoMapQuery{
		Root:           root,
		Limit:          10,
		SymbolsPerFile: 2,
	})
	if err != nil {
		t.Fatalf("global repo map: %v", err)
	}

	rendered := RenderRepoMapTOON(repoMap)
	if len(repoMap.Files) != 2 ||
		!strings.Contains(rendered, "coding_ethos_repo_map:") ||
		!strings.Contains(rendered, "pkg/worker.py") ||
		!strings.Contains(rendered, "def helper():") ||
		strings.Contains(rendered, ".codex/skills/generated/SKILL.md") ||
		strings.Contains(rendered, ".venv/lib/python/site-packages/pkg.py") ||
		strings.Contains(rendered, "coding-ethos/go/internal/tool.go") ||
		strings.Contains(rendered, "ignored/cache.py") ||
		strings.Contains(rendered, "ruff.toml") ||
		strings.Contains(rendered, `fmt.Println("x");`) {
		t.Fatalf("repo map = %#v\n%s", repoMap, rendered)
	}
}

func TestASTDerivedContextRefusesStaleSource(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"ok\"\n\n"+
			"class Worker:\n"+
			"    def run(self):\n"+
			"        return helper()\n",
	))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(
		"def helper():\n"+
			"    return \"changed\"\n\n"+
			"class Worker:\n"+
			"    def run(self):\n"+
			"        return helper()\n",
	))

	assertStaleASTContextRefusal(t, "repo map", func() error {
		_, repoMapErr := store.RepoMap(ctx, CompactCodeContextQuery{
			Path:  "pkg/worker.py",
			Root:  root,
			Limit: 10,
		})

		return repoMapErr
	})

	assertStaleASTContextRefusal(t, "compact context", func() error {
		_, compactErr := store.CompactCodeContext(ctx, CompactCodeContextQuery{
			Path:  "pkg/worker.py",
			Root:  root,
			Limit: 10,
		})

		return compactErr
	})

	assertStaleASTContextRefusal(t, "directory anatomy", func() error {
		_, anatomyErr := store.DirectoryAnatomy(ctx, DirectoryAnatomyQuery{
			Path:  "pkg",
			Root:  root,
			Limit: 10,
		})

		return anatomyErr
	})
}

func TestASTDerivedContextPreservesWhitespacePaths(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	source := []byte(
		"def helper():\n" +
			"    return \"ok\"\n",
	)
	writeFile(t, filepath.Join(root, " worker.py"), source)
	dbPath := filepath.Join(root, ".coding-ethos", "code-intel.db")
	store := openTestStoreAt(
		t,
		ctx,
		dbPath,
	)
	rawDatabase := openRawSQLite(t, dbPath)

	_, err := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES (?, 'python', ?, ?, 2, '2026-01-01T00:00:00Z')`,
		" worker.py",
		astfacts.ContentHash(source),
		len(source),
	)
	if err != nil {
		t.Fatalf("insert whitespace code file: %v", err)
	}

	_, err = rawDatabase.ExecContext(
		ctx,
		`INSERT INTO code_chunks(
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, parent_symbol_path, parent_chunk_id, start_byte,
			end_byte, start_line, end_line, content_hash, search_text, raw_text
		) VALUES (
			'chunk-space', ' worker.py', 'python', 'function_definition',
			'function', 'helper', 'helper', '', '', 0, ?, 1, 2, ?, 'helper', ?
		)`,
		len(source),
		astfacts.ContentHash(source),
		string(source),
	)
	if err != nil {
		t.Fatalf("insert whitespace code chunk: %v", err)
	}

	repoMap, err := store.RepoMap(ctx, CompactCodeContextQuery{
		Root:  root,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("repo map for whitespace path: %v", err)
	}

	if len(repoMap) != 1 || repoMap[0].Path != " worker.py" {
		t.Fatalf("repo map = %#v", repoMap)
	}
}

func TestASTIndexerIndexesOnlyDirectoryChildren(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "pkg", "app.go"), []byte(`package pkg

func Direct() {}
`))
	writeFile(t, filepath.Join(root, "pkg", "sub", "deep.go"), []byte(`package sub

func Nested() {}
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(store).IndexDirectoryChildren(ctx, root, "pkg")
	if err != nil {
		t.Fatalf("index directory children: %v", err)
	}

	if summary.FilesIndexed != 1 || summary.ChunksIndexed == 0 {
		t.Fatalf("summary = %#v", summary)
	}

	files, err := store.CodeFilesByPath(ctx)
	if err != nil {
		t.Fatalf("code files by path: %v", err)
	}

	if _, found := files["pkg/app.go"]; !found {
		t.Fatalf("direct file missing: %#v", files)
	}

	if _, found := files["pkg/sub/deep.go"]; found {
		t.Fatalf("nested file was indexed: %#v", files)
	}
}

func TestASTIndexerDirectoryTreeHonorsMaxDepth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "pkg", "app.go"), []byte(`package pkg

func Direct() {}
`))
	writeFile(t, filepath.Join(root, "pkg", "sub", "deep.go"), []byte(`package sub

func Nested() {}
`))
	writeFile(
		t,
		filepath.Join(root, "pkg", "sub", "deeper", "hidden.go"),
		[]byte(`package deeper

func Hidden() {}
`),
	)

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(store).IndexDirectoryTree(ctx, root, "pkg", 2)
	if err != nil {
		t.Fatalf("index directory tree: %v", err)
	}

	if summary.FilesIndexed != 2 || summary.ChunksIndexed == 0 {
		t.Fatalf("summary = %#v", summary)
	}

	files, err := store.CodeFilesByPath(ctx)
	if err != nil {
		t.Fatalf("code files by path: %v", err)
	}

	for _, path := range []string{"pkg/app.go", "pkg/sub/deep.go"} {
		if _, found := files[path]; !found {
			t.Fatalf("indexed file %q missing: %#v", path, files)
		}
	}

	if _, found := files["pkg/sub/deeper/hidden.go"]; found {
		t.Fatalf("too-deep file was indexed: %#v", files)
	}
}

func TestASTIndexerDirectoryChildrenMarksConfiguredExcludesInactive(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()

	writeFile(t, filepath.Join(root, "pkg", "app.go"), []byte(`package pkg

func Keep() {}
`))
	writeFile(t, filepath.Join(root, "pkg", "generated.go"), []byte(`package pkg

func Generated() {}
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)
	indexer := NewASTIndexer(store)

	_, err := indexer.IndexDirectoryChildren(ctx, root, "pkg")
	if err != nil {
		t.Fatalf("index directory children: %v", err)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/generated.go",
		SymbolName: "Generated",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query generated chunk: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("generated chunks = %#v", chunks)
	}

	err = store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:     vectorBackendName,
		Collection:  "code_chunks",
		ModelID:     "voyage-code-3",
		RecordKind:  codeChunkRecordKind,
		RecordID:    chunks[0].ID,
		Path:        chunks[0].Path,
		ContentHash: chunks[0].ContentHash,
		Dimension:   1024,
	})
	if err != nil {
		t.Fatalf("record generated embedding: %v", err)
	}

	writeFile(t, filepath.Join(root, "repo_config.yaml"), []byte(
		"code_intel:\n"+
			"  exclude_paths:\n"+
			"    - \"pkg\"\n",
	))

	summary, err := indexer.IndexDirectoryChildren(ctx, root, "pkg")
	if err != nil {
		t.Fatalf("refresh directory children: %v", err)
	}

	if !slices.Contains(summary.Deleted, "pkg/generated.go") {
		t.Fatalf("summary = %#v", summary)
	}

	files, err := store.CodeFilesByPath(ctx)
	if err != nil {
		t.Fatalf("code files by path: %v", err)
	}

	generated, found := files["pkg/generated.go"]
	if !found || generated.DeletedAtUTC == "" || generated.StaleReason != "ignored" {
		t.Fatalf("generated file = %#v, found = %t", generated, found)
	}

	keep, found := files["pkg/app.go"]
	if !found || keep.DeletedAtUTC == "" {
		t.Fatalf("keep file = %#v, found = %t", keep, found)
	}

	records, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		RecordKind: codeChunkRecordKind,
		RecordID:   chunks[0].ID,
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query generated embedding records: %v", err)
	}

	if len(records) != 0 {
		t.Fatalf("generated embedding records = %#v", records)
	}

	status, err := store.IndexStatus(ctx, evidence.VectorStats{}, EmbeddingRecordQuery{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "voyage-code-3",
	})
	if err != nil {
		t.Fatalf("index status after ignored file: %v", err)
	}

	if status.ReadyRecords != 0 || status.MissingVectors != 0 {
		t.Fatalf("ignored code chunks counted as ready records: %#v", status)
	}
}

func assertWorkerLineAndConfigContext(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	lineContext, err := store.CodeContext(ctx, CodeContextQuery{
		Path:  "pkg/worker.py",
		Line:  14,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("line code context: %v", err)
	}

	if lineContext.Chunk.SymbolPath != "Worker.run" {
		t.Fatalf("line context = %#v", lineContext.Chunk)
	}

	configContext, err := store.CodeContext(ctx, CodeContextQuery{
		Path:       "pkg/worker.py",
		SymbolPath: "load_a_config",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("config code context: %v", err)
	}

	assertNoSubstringReferenceEdge(t, configContext)
}

func assertWorkerImportEdge(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	edges, err := store.CodeEdges(ctx, CodeEdgeQuery{
		Path:       "pkg/worker.py",
		Kind:       "imports",
		TargetName: "os",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query import edges: %v", err)
	}

	if len(edges) != 1 {
		t.Fatalf("import edges = %#v", edges)
	}
}

func assertIndexedSummary(t *testing.T, summary CodeIndexSummary) {
	t.Helper()

	if summary.FilesIndexed != 1 || summary.ChunksIndexed < 3 {
		t.Fatalf("summary = %#v", summary)
	}
}

func assertBuildMessageChunk(t *testing.T, chunks []CodeChunk) {
	t.Helper()

	if len(chunks) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}

	if chunks[0].Language != "go" || chunks[0].SearchText == "" ||
		!strings.Contains(chunks[0].RawText, "BuildMessage") {
		t.Fatalf("chunk = %#v", chunks[0])
	}
}

func anatomyFileByPath(
	files []DirectoryAnatomyFile,
	path string,
) *DirectoryAnatomyFile {
	for index := range files {
		if files[index].Path == path {
			return &files[index]
		}
	}

	return nil
}

func anatomyHasSymbol(file DirectoryAnatomyFile, symbolName string) bool {
	for _, symbol := range file.Symbols {
		if symbol.Name == symbolName || symbol.SymbolPath == symbolName {
			return true
		}
	}

	return false
}

func assertWorkerRunChunk(t *testing.T, chunks []CodeChunk) {
	t.Helper()

	if len(chunks) != 1 || chunks[0].ParentSymbolPath != "Worker" ||
		chunks[0].ParentChunkID == "" {
		t.Fatalf("run chunks = %#v", chunks)
	}
}

func assertWorkerRunContext(t *testing.T, context CodeContext) {
	t.Helper()

	if context.Parent == nil || context.Parent.SymbolPath != "Worker" {
		t.Fatalf("context parent = %#v", context.Parent)
	}

	if len(context.OutgoingEdges) == 0 {
		t.Fatalf("context outgoing edges = %#v", context.OutgoingEdges)
	}

	if len(context.Siblings) != 1 || context.Siblings[0].SymbolPath != "Worker.stop" {
		t.Fatalf("context siblings = %#v", context.Siblings)
	}

	if !codeEdgesContainTarget(context.OutgoingEdges, "helper") {
		t.Fatalf(
			"context outgoing edges missing helper reference: %#v",
			context.OutgoingEdges,
		)
	}
}

func assertStaleCodeContextRefusesChangedSource(
	t *testing.T,
	ctx context.Context,
	root string,
	store *Store,
) {
	t.Helper()

	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(`import os

def helper():
    return "changed"

class Worker:
    def run(self):
        return helper()
`))

	_, err := store.CodeContext(ctx, CodeContextQuery{
		Path:       "pkg/worker.py",
		Root:       root,
		SymbolPath: "Worker.run",
		Limit:      10,
	})
	if err == nil {
		t.Fatal("expected stale code context refusal")
	}

	if !strings.Contains(err.Error(), "stale code context") {
		t.Fatalf("error = %v", err)
	}
}

func assertStaleASTContextRefusal(
	t *testing.T,
	label string,
	query func() error,
) {
	t.Helper()

	err := query()
	if err == nil {
		t.Fatalf("expected stale %s refusal", label)
	}

	if !strings.Contains(err.Error(), "stale code context") {
		t.Fatalf("%s error = %v", label, err)
	}
}

func assertNoSubstringReferenceEdge(t *testing.T, context CodeContext) {
	t.Helper()

	if codeEdgesContainTarget(context.OutgoingEdges, "a") {
		t.Fatalf(
			"substring reference edge should not be emitted: %#v",
			context.OutgoingEdges,
		)
	}
}

func codeEdgesContainTarget(edges []CodeEdge, target string) bool {
	for _, edge := range edges {
		if edge.TargetName == target {
			return true
		}
	}

	return false
}

func TestStoreQueriesMigratedCodeChunksWithNullParentSymbolPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "code-intel.db")
	store := openTestStoreAt(t, ctx, path)
	rawDatabase := openRawSQLite(t, path)

	_, inlineErrC := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES ('pkg/app.py', 'python', 'hash-file', 10, 1, '2026-01-01T00:00:00Z')`,
	)
	if inlineErrC != nil {
		t.Fatalf("insert old code file: %v", inlineErrC)
	}

	_, inlineErrD := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO code_chunks(
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, parent_symbol_path, parent_chunk_id, start_byte,
			end_byte, start_line, end_line, content_hash, search_text, raw_text
		) VALUES (
			'chunk-old', 'pkg/app.py', 'python', 'module', 'module', 'app',
			'app', NULL, '', 0, 10, 1, 1, 'hash-chunk', 'app', 'value = 1'
		)`,
	)
	if inlineErrD != nil {
		t.Fatalf("insert old code chunk: %v", inlineErrD)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: "pkg/app.py"})
	if err != nil {
		t.Fatalf("query old code chunks: %v", err)
	}

	if len(chunks) != 1 || chunks[0].ParentSymbolPath != "" {
		t.Fatalf("code chunks = %#v", chunks)
	}

	context, err := store.CodeContext(ctx, CodeContextQuery{ChunkID: "chunk-old"})
	if err != nil {
		t.Fatalf("query old code context: %v", err)
	}

	if context.Chunk.ParentSymbolPath != "" {
		t.Fatalf("code context = %#v", context)
	}
}

func TestSARIFIngestLinksASTBackedResultsToCodeChunks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(`def helper():
    return "ok"
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, inlineErrE := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if inlineErrE != nil {
		t.Fatalf("index code: %v", inlineErrE)
	}

	err := store.IngestSARIFRun(ctx, SARIFRun{
		ID:       "sarif-run-1",
		ToolName: "coding-ethos",
		Results: []SARIFResultReference{{
			ID:            "sarif-result-1",
			RuleID:        "filesystem.line_limits",
			Message:       "Large source files must not keep growing.",
			PolicyID:      "filesystem.line_limits",
			SkillID:       "agent-operating-discipline",
			Path:          "pkg/worker.py",
			ASTLanguage:   "python",
			ASTNodeKind:   "function_definition",
			ASTSymbolKind: "function",
			ASTSymbolName: "helper",
			ASTSymbolPath: "helper",
			SearchText:    "helper line limit",
		}},
	})
	if err != nil {
		t.Fatalf("ingest SARIF run: %v", err)
	}

	results, err := store.SARIFResults(ctx, SARIFResultQuery{RunID: "sarif-run-1"})
	if err != nil {
		t.Fatalf("SARIF results: %v", err)
	}

	if len(results) != 1 || results[0].LinkedChunkID == "" {
		t.Fatalf("SARIF results = %#v", results)
	}

	context, err := store.CodeContext(
		ctx,
		CodeContextQuery{ChunkID: results[0].LinkedChunkID},
	)
	if err != nil {
		t.Fatalf("code context: %v", err)
	}

	if len(context.FindingLinks) != 1 ||
		context.FindingLinks[0].FindingID != "sarif-result-1" {
		t.Fatalf("finding links = %#v", context.FindingLinks)
	}
}

func TestSQLiteVectorIndexSearchesEmbeddings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	index, err := NewSQLiteVectorIndex(ctx, filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatalf("open vector index: %v", err)
	}

	t.Cleanup(func() {
		closeErr := index.Close()
		if closeErr != nil {
			t.Fatalf("close vector index: %v", closeErr)
		}
	})

	seedSQLiteVectorIndex(t, ctx, index)
	assertSQLiteVectorSearch(t, ctx, index)
	assertSQLiteVectorMutation(t, ctx, index)
	assertSQLiteVectorValidation(t, ctx, index)
}

func seedSQLiteVectorIndex(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	for _, record := range sqliteVectorSeedRecords() {
		err := index.UpsertEmbedding(ctx, record)
		if err != nil {
			t.Fatalf("upsert vector %q: %v", record.ID, err)
		}
	}
}

func sqliteVectorSeedRecords() []evidence.VectorRecord {
	return []evidence.VectorRecord{
		{
			ID:         "near",
			Collection: "remediations",
			ModelID:    "test-model",
			Vector:     []float32{1, 0, 0},
			Dimension:  3,
			Metadata:   map[string]string{"policy_id": "python.unused_imports"},
		},
		{
			ID:         "far",
			Collection: "remediations",
			ModelID:    "test-model",
			Vector:     []float32{0, 1, 0},
			Dimension:  3,
			Metadata:   map[string]string{"policy_id": "python.unused_imports"},
		},
	}
}

func assertSQLiteVectorSearch(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	matches, err := index.Search(ctx, evidence.VectorQuery{
		Collection: "remediations",
		ModelID:    "test-model",
		Vector:     []float32{1, 0, 0},
		Filters:    map[string]string{"policy_id": "python.unused_imports"},
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("search vectors: %v", err)
	}

	if len(matches) != 1 || matches[0].ID != "near" {
		t.Fatalf("matches = %#v", matches)
	}

	assertSQLiteVectorRows(t, ctx, index, 2, "vector stats")
}

func assertSQLiteVectorMutation(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	err := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         "near",
		Collection: "remediations",
		ModelID:    "test-model",
		Vector:     []float32{0, 0, 1, 0},
		Dimension:  4,
		Metadata:   map[string]string{"policy_id": "python.unused_imports"},
	})
	if err != nil {
		t.Fatalf("replace vector with new dimension: %v", err)
	}

	err = index.DeleteEmbedding(ctx, "far", "test-model")
	if err != nil {
		t.Fatalf("delete vector: %v", err)
	}

	err = index.DeleteEmbedding(ctx, "missing", "test-model")
	if err != nil {
		t.Fatalf("delete missing vector: %v", err)
	}

	assertSQLiteVectorRows(t, ctx, index, 1, "vector stats after delete")

	err = index.Rebuild(ctx, "remediations")
	if err != nil {
		t.Fatalf("rebuild vectors: %v", err)
	}

	assertSQLiteVectorRows(t, ctx, index, 0, "vector stats after rebuild")
}

func assertSQLiteVectorRows(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
	expected int,
	label string,
) {
	t.Helper()

	stats, err := index.Stats(ctx)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	if stats.Backend != vectorBackendName || stats.Rows != expected {
		t.Fatalf("%s = %#v", label, stats)
	}
}

func assertSQLiteVectorValidation(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	_, err := NewSQLiteVectorIndex(ctx, "")
	if err == nil {
		t.Fatal("NewSQLiteVectorIndex(empty) returned nil error")
	}

	err = index.UpsertEmbedding(ctx, evidence.VectorRecord{})
	if err == nil {
		t.Fatal("UpsertEmbedding(empty) returned nil error")
	}

	_, err = index.Search(ctx, evidence.VectorQuery{})
	if err == nil {
		t.Fatal("Search(empty) returned nil error")
	}
}

func TestHybridSearchCombinesFTSVectorAndOutcomeBoost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	index, err := NewSQLiteVectorIndex(ctx, filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatalf("open vector index: %v", err)
	}

	t.Cleanup(func() {
		closeErr := index.Close()
		if closeErr != nil {
			t.Fatalf("close vector index: %v", closeErr)
		}
	})

	inlineErr22 := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-1",
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
		SearchText:    "Remove unused import and rerun ruff.",
	})
	if inlineErr22 != nil {
		t.Fatalf("record outcome: %v", inlineErr22)
	}

	inlineErr23 := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         "rem-1",
		Collection: "remediations",
		ModelID:    "test-model",
		Vector:     []float32{1, 0, 0},
		Dimension:  3,
		Metadata: map[string]string{
			"record_kind": "remediation_outcome",
			"record_id":   "rem-1",
			"policy_id":   "python.unused_imports",
			"skill_id":    "lint-remediation",
			"path":        "pkg/app.py",
			"outcome":     "fixed",
		},
	})
	if inlineErr23 != nil {
		t.Fatalf("upsert vector: %v", inlineErr23)
	}

	results, err := store.HybridSearch(ctx, index, HybridSearchQuery{
		Text:       "unused",
		Collection: "remediations",
		ModelID:    "test-model",
		PolicyID:   "python.unused_imports",
		Vector:     []float32{1, 0, 0},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("hybrid results = %#v", results)
	}

	if results[0].Source != "fts+vector" || results[0].Outcome != "fixed" ||
		results[0].Score <= 2 {
		t.Fatalf("hybrid result = %#v", results[0])
	}
}

func TestHybridSearchReturnsVectorBackedCodeChunks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "worker.py")
	writeFile(t, sourcePath, []byte(`class Worker:
    def run(self):
        return "ok"
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, inlineErrF := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if inlineErrF != nil {
		t.Fatalf("index code: %v", inlineErrF)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/worker.py",
		SymbolName: "Worker",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query chunk: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}

	index, err := NewSQLiteVectorIndex(ctx, filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatalf("open vector index: %v", err)
	}

	t.Cleanup(func() {
		closeErr := index.Close()
		if closeErr != nil {
			t.Fatalf("close vector index: %v", closeErr)
		}
	})

	inlineErr24 := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         chunks[0].ID,
		Collection: "code_chunks",
		ModelID:    "test-model",
		Vector:     []float32{0, 1, 0},
		Dimension:  3,
		Metadata: map[string]string{
			"record_kind": codeChunkRecordKind,
			"record_id":   chunks[0].ID,
			"path":        chunks[0].Path,
			"message":     chunks[0].SymbolPath,
		},
	})
	if inlineErr24 != nil {
		t.Fatalf("upsert vector: %v", inlineErr24)
	}

	results, err := store.HybridSearch(ctx, index, HybridSearchQuery{
		Text:       "Worker",
		Collection: "code_chunks",
		ModelID:    "test-model",
		Path:       "pkg/worker.py",
		Vector:     []float32{0, 1, 0},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}

	if len(results) == 0 || results[0].Kind != codeChunkRecordKind ||
		results[0].Source != "fts+vector" {
		t.Fatalf("hybrid results = %#v", results)
	}

	writeFile(t, filepath.Join(root, "repo_config.yaml"), []byte(
		"code_intel:\n"+
			"  exclude_paths:\n"+
			"    - \"pkg\"\n",
	))

	_, err = NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("refresh excluded code: %v", err)
	}

	results, err = store.HybridSearch(ctx, index, HybridSearchQuery{
		Text:       "Worker",
		Collection: "code_chunks",
		ModelID:    "test-model",
		Path:       "pkg/worker.py",
		Vector:     []float32{0, 1, 0},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("hybrid search after exclude: %v", err)
	}

	if len(results) != 0 {
		t.Fatalf("stale vector-backed code chunks = %#v", results)
	}
}

func TestASTIndexerSupportsShellAndYAMLChunks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "scripts", "check.sh"), []byte(`#!/usr/bin/env bash

run_check() {
  echo ok
}
`))
	writeFile(t, filepath.Join(root, "config.yml"), []byte(`linters:
  ruff: true
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(
		store,
	).IndexPaths(ctx, root, []string{"scripts", "config.yml"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	if summary.FilesIndexed != 2 || summary.ChunksIndexed < 2 {
		t.Fatalf("summary = %#v", summary)
	}

	shellChunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Language:   "shell",
		SymbolName: "run_check",
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("query shell chunks: %v", err)
	}

	if len(shellChunks) != 1 || shellChunks[0].SymbolKind != "function" {
		t.Fatalf("shell chunks = %#v", shellChunks)
	}

	yamlChunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "config.yml",
		Language:   "yaml",
		SymbolName: "linters",
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("query yaml chunks: %v", err)
	}

	if len(yamlChunks) != 1 || yamlChunks[0].SymbolKind != "config_entry" {
		t.Fatalf("yaml chunks = %#v", yamlChunks)
	}
}

func TestVectorFactoryDefaultPathAndIndexStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	root := t.TempDir()
	assertDefaultVectorPath(t, root)
	assertUnsupportedVectorBackendFails(t, ctx, root)

	index := openTestVectorIndex(t, ctx, root)
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	replaceVectorStatusCodeChunk(t, ctx, store)

	stats, err := index.Stats(ctx)
	if err != nil {
		t.Fatalf("vector stats: %v", err)
	}

	status, err := store.IndexStatus(ctx, stats, EmbeddingRecordQuery{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "test-model",
	})
	if err != nil {
		t.Fatalf("index status: %v", err)
	}

	assertVectorStatusBeforeEmbedding(t, status)

	inlineErr26 := store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "test-model",
		InputKind:  "text",
		RecordKind: codeChunkRecordKind,
		RecordID:   "chunk-1",
		Dimension:  3,
		Path:       "pkg/app.py",
	})
	if inlineErr26 != nil {
		t.Fatalf("upsert embedding record: %v", inlineErr26)
	}

	status, err = store.IndexStatus(ctx, stats, EmbeddingRecordQuery{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "test-model",
	})
	if err != nil {
		t.Fatalf("index status after embedding: %v", err)
	}

	assertVectorStatusAfterEmbedding(t, status)
}

func assertDefaultVectorPath(t *testing.T, root string) {
	t.Helper()

	got := DefaultVectorPath(root)

	want := filepath.Join(root, ".coding-ethos", "code-intel-vectors.db")
	if got != want {
		t.Fatalf("DefaultVectorPath() = %q", got)
	}
}

func assertUnsupportedVectorBackendFails(
	t *testing.T,
	ctx context.Context,
	root string,
) {
	t.Helper()

	_, err := NewVectorIndex(
		ctx,
		VectorBackendConfig{Backend: "unknown", URI: filepath.Join(root, "bad.db")},
	)
	if err == nil {
		t.Fatal("unsupported vector backend should fail")
	}
}

func openTestVectorIndex(
	t *testing.T,
	ctx context.Context,
	root string,
) evidence.VectorIndex {
	t.Helper()

	index, err := NewVectorIndex(
		ctx,
		VectorBackendConfig{URI: filepath.Join(root, "vectors.db")},
	)
	if err != nil {
		t.Fatalf("new vector index: %v", err)
	}

	t.Cleanup(func() {
		if closer, ok := index.(interface{ Close() error }); ok {
			closeErr := closer.Close()
			if closeErr != nil {
				t.Fatalf("close vector index: %v", closeErr)
			}
		}
	})

	return index
}

func replaceVectorStatusCodeChunk(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	err := store.ReplaceCodeFileChunks(ctx, CodeFile{
		Path:        "pkg/app.py",
		Language:    "python",
		ContentHash: "hash-file",
		SizeBytes:   20,
		LineCount:   3,
	}, []CodeChunk{{
		ID:          "chunk-1",
		Path:        "pkg/app.py",
		Language:    "python",
		NodeKind:    "function_definition",
		SymbolKind:  "function",
		SymbolName:  "run",
		SymbolPath:  "run",
		ContentHash: "hash-chunk",
		SearchText:  "run function",
		RawText:     "def run(): pass",
		StartLine:   1,
		EndLine:     1,
	}})
	if err != nil {
		t.Fatalf("replace chunks: %v", err)
	}
}

func assertVectorStatusBeforeEmbedding(t *testing.T, status IndexStatus) {
	t.Helper()

	if status.ReadyRecords == 0 || status.MissingVectors == 0 || status.Fresh {
		t.Fatalf("status before embedding = %#v", status)
	}
}

func assertVectorStatusAfterEmbedding(t *testing.T, status IndexStatus) {
	t.Helper()

	if status.EmbeddingRecords != 1 || status.MissingVectors != 0 || !status.Fresh {
		t.Fatalf("status after embedding = %#v", status)
	}
}

func assertStats(t *testing.T, got, want Stats) {
	t.Helper()

	for _, check := range expectedStatsFields(got, want) {
		if check.want == 0 {
			continue
		}

		if check.got != check.want {
			t.Fatalf("stats = %#v, want %s=%d", got, check.name, check.want)
		}
	}
}

type statFieldCheck struct {
	name string
	got  int
	want int
}

func expectedStatsFields(got, want Stats) []statFieldCheck {
	return []statFieldCheck{
		{"traces", got.Traces, want.Traces},
		{"hook_events", got.HookEvents, want.HookEvents},
		{"hook_decisions", got.HookDecisions, want.HookDecisions},
		{"hook_targets", got.HookTargets, want.HookTargets},
		{"findings", got.Findings, want.Findings},
		{"files", got.Files, want.Files},
		{"code_chunks", got.CodeChunks, want.CodeChunks},
		{"code_edges", got.CodeEdges, want.CodeEdges},
		{"remediations", got.Remediations, want.Remediations},
		{"remediation_events", got.RemediationEvents, want.RemediationEvents},
		{"sarif_runs", got.SARIFRuns, want.SARIFRuns},
		{"sarif_results", got.SARIFResults, want.SARIFResults},
		{"remediation_outcomes", got.RemediationOutcomes, want.RemediationOutcomes},
		{"embedding_records", got.EmbeddingRecords, want.EmbeddingRecords},
		{"fts_rows", got.FtsRows, want.FtsRows},
	}
}

func assertMinimumStats(t *testing.T, got, want Stats) {
	t.Helper()

	if got.Files < want.Files ||
		got.CodeChunks < want.CodeChunks ||
		got.CodeEdges < want.CodeEdges ||
		got.FtsRows < want.FtsRows {
		t.Fatalf("stats = %#v, want minimum %#v", got, want)
	}
}

func containsJoined(items []string, item string) bool {
	return strings.Contains(strings.Join(items, ","), item)
}

func stringAnySlicesEqual(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}

	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	return openTestStoreAt(t, ctx, filepath.Join(t.TempDir(), "code-intel.db"))
}

func openTestStoreAt(t *testing.T, ctx context.Context, path string) *Store {
	t.Helper()

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	t.Cleanup(func() {
		err := store.Close()
		if err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	return store
}

func openRawSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Fatalf("close raw sqlite: %v", closeErr)
		}
	})

	return database
}

func lintTracePayload(t *testing.T, traceID, recordedAt string) []byte {
	t.Helper()

	diagnostic := diagnostics.Diagnostic{
		Tool:     "ruff",
		Code:     "F401",
		File:     "pkg/app.py",
		Line:     4,
		Severity: "error",
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Message:  "unused import",
		Advice:   "Remove unused imports.",
	}
	findings := evidence.FromDiagnostics([]diagnostics.Diagnostic{diagnostic})
	remediations := agentmsg.FromDiagnostics([]diagnostics.Diagnostic{diagnostic})
	record := lint.TraceRecord{
		SchemaVersion:      evidence.SchemaVersion,
		TraceID:            traceID,
		RecordedAtUTC:      recordedAt,
		RepoRoot:           "/repo",
		Result:             lint.Result{Scope: "tool:ruff", Status: "blocked"},
		Findings:           findings,
		AgentRemediation:   remediations,
		RemediationSummary: agentmsg.Summarize(remediations),
		RemediationEvents: evidence.RemediationEvents(
			remediations,
			findings,
			traceID,
			"suggested",
		),
	}

	return mustJSON(t, record)
}

func sarifPayload(t *testing.T) []byte {
	t.Helper()

	output, err := hookoutput.FormatLintResultSARIF(lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			Code:     "F401",
			File:     "pkg/app.py",
			Line:     4,
			Severity: "error",
			PolicyID: "python.unused_imports",
			SkillID:  "lint-remediation",
			Message:  "unused import",
			Advice:   "Remove unused imports.",
			Metadata: map[string]any{
				"implementation": "cel",
				"when":           "finding.policy_id == 'python.unused_imports'",
				"policy_source":  "coding_ethos.yml",
				"source_tool":    "ruff",
			},
		}},
	})
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	return []byte(output)
}

func hookTracePayload(t *testing.T) []byte {
	t.Helper()

	return hookTracePayloadWithIDs(
		t,
		"hook-trace-a",
		"deny-hook-a",
		"2026-01-01T00:02:00Z",
	)
}

func runCodeIntelGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	_ = runCodeIntelGitOutput(t, dir, args...)
}

func runCodeIntelGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	command := realgit.Command(context.Background(), false, args...)
	command.Dir = dir
	command.Env = cleanCodeIntelGitEnv()

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}

	return string(output)
}

func cleanCodeIntelGitEnv() []string {
	env := realgit.CleanGitLocalEnv(os.Environ())

	return append(
		env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"XDG_CONFIG_HOME="+os.DevNull,
	)
}

func hookTracePayloadForProvider(t *testing.T, provider string) []byte {
	t.Helper()

	payload := map[string]any{}

	err := json.Unmarshal(
		hookTracePayloadWithIDs(
			t,
			"hook-trace-"+provider,
			"deny-hook-"+provider,
			"2026-01-01T00:02:00Z",
		),
		&payload,
	)
	if err != nil {
		t.Fatalf("decode hook payload: %v", err)
	}

	payload["provider"] = provider

	return mustJSON(t, payload)
}

func hookTracePayloadWithCommand(
	t *testing.T,
	traceID string,
	command string,
	status string,
	blocked bool,
) []byte {
	t.Helper()

	payload := map[string]any{}

	err := json.Unmarshal(
		hookTracePayloadWithIDs(t, traceID, "tracking-"+traceID, "2026-01-01T00:03:00Z"),
		&payload,
	)
	if err != nil {
		t.Fatalf("decode hook payload: %v", err)
	}

	payload["command"] = map[string]any{
		"sha256":       strings.Repeat("f", 64),
		"shape_sha256": strings.Repeat("0", 64),
		"preview":      command,
	}
	payload["status"] = status
	payload["operation_kind"] = "shell_command"
	payload["risk_category"] = "allowed"
	payload["decisions"] = []map[string]any{}
	payload["findings"] = []evidence.Finding{}
	payload["agent_remediation"] = []agentmsg.Remediation{}
	payload["remediation_events"] = []evidence.RemediationEvent{}
	payload["output_shape"] = map[string]any{
		"blocked": blocked,
	}

	return mustJSON(t, payload)
}

func hookTracePayloadWithIDs(
	t *testing.T,
	traceID string,
	trackingID string,
	recordedAtUTC string,
) []byte {
	t.Helper()

	finding := evidence.FromDiagnostic(diagnostics.Diagnostic{
		Tool:     "hook",
		File:     "pkg/app.py",
		PolicyID: "shell.github_admin",
		SkillID:  "safe-git-workflow",
		Message:  "admin bypass",
	})
	remediation := agentmsg.Remediation{
		ID:       "rem-hook",
		PolicyID: "shell.github_admin",
		SkillID:  "safe-git-workflow",
		Message:  "Use the normal review path.",
	}
	event := evidence.RemediationEventFromRemediation(
		remediation,
		finding.ID,
		traceID,
		"suggested",
	)

	return mustJSON(t, map[string]any{
		"schema_version":  evidence.SchemaVersion,
		"trace_id":        traceID,
		"tracking_id":     trackingID,
		"recorded_at_utc": recordedAtUTC,
		"provider":        "codex",
		"event":           "PreToolUse",
		"tool":            "Bash",
		"cwd":             "/repo",
		"command": map[string]any{
			"sha256":       strings.Repeat("a", 64),
			"shape_sha256": strings.Repeat("b", 64),
			"preview":      "git status --short",
		},
		"files":             []string{"pkg/app.py"},
		"operation_kind":    "git_status",
		"target_kind":       "source_file",
		"risk_category":     "bypass",
		"target_set_sha256": strings.Repeat("c", 64),
		"runtime_ms":        12,
		"status":            "blocked",
		"decisions": []map[string]any{
			{
				"policy_id":       "git.wrapper_required",
				"decision":        "block",
				"severity":        "block",
				"skill_id":        "safe-git-workflow",
				"implementation":  "cel",
				"message_hash":    strings.Repeat("d", 64),
				"suggestion_hash": strings.Repeat("e", 64),
			},
		},
		"findings":           []evidence.Finding{finding},
		"agent_remediation":  []agentmsg.Remediation{remediation},
		"remediation_events": []evidence.RemediationEvent{event},
		"output_shape": map[string]any{
			"blocked":           true,
			"has_updated_input": true,
		},
	})
}

func multiRunSARIFPayload() []byte {
	return []byte(`{
		"version":"2.1.0",
		"runs":[
			{
				"tool":{"driver":{"name":"first-tool","rules":[
					{"id":"R1","properties":{"policy_id":"policy.first","skill_id":"skill-a"}}
				]}},
				"results":[
					{
						"ruleId":"R1",
						"level":"error",
						"message":{"text":"first result"},
							"locations":[{
								"physicalLocation":{
									"artifactLocation":{"uri":"pkg/first.py"},
									"region":{"startLine":2}
								}
							}]
					}
				]
			},
			{
				"tool":{"driver":{"name":"second-tool","rules":[
					{"id":"R2","properties":{"policy_id":"policy.second","skill_id":"skill-b"}}
				]}},
				"results":[
					{
						"ruleId":"R2",
						"level":"warning",
						"message":{"text":"second result"},
							"locations":[{
								"physicalLocation":{
									"artifactLocation":{"uri":"pkg/second.py"},
									"region":{"startLine":4}
								}
							}]
					}
				]
			}
		]
	}`)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}

	return payload
}

func writeFile(t *testing.T, path string, payload []byte) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	err = os.WriteFile(path, payload, 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
