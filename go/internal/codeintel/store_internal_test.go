// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

func TestSQLiteStoreDSNRequestsImmediateTransactions(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{
			name: "plain path",
			path: "/tmp/code-intel.db",
			want: "/tmp/code-intel.db?_txlock=immediate",
		},
		{
			name: "existing query",
			path: "file:/tmp/code-intel.db?_pragma=busy_timeout(30000)",
			want: "file:/tmp/code-intel.db?_pragma=busy_timeout(30000)&_txlock=immediate",
		},
		{
			name: "existing immediate transaction parameter",
			path: "file:/tmp/code-intel.db?_txlock=immediate",
			want: "file:/tmp/code-intel.db?_txlock=immediate",
		},
		{
			name: "existing immediate transaction parameter after other query",
			path: "file:/tmp/code-intel.db?_pragma=busy_timeout(30000)&_txlock=immediate",
			want: "file:/tmp/code-intel.db?_pragma=busy_timeout(30000)&_txlock=immediate",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := sqliteStoreDSN(test.path); got != test.want {
				t.Fatalf("sqliteStoreDSN(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestExistingLintIndexPathsCleansDedupesAndSkipsDirectories(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	outside := t.TempDir()
	target := filepath.Join(root, "pkg", "app.py")
	err := os.MkdirAll(filepath.Dir(target), 0o700)
	if err != nil {
		t.Fatalf("create package dir: %v", err)
	}

	err = os.WriteFile(target, []byte("VALUE = 1\n"), 0o600)
	if err != nil {
		t.Fatalf("write target: %v", err)
	}

	outsideTarget := filepath.Join(outside, "outside.py")
	err = os.WriteFile(outsideTarget, []byte("VALUE = 2\n"), 0o600)
	if err != nil {
		t.Fatalf("write outside target: %v", err)
	}

	selected := existingLintIndexPaths(root, []string{
		"pkg/app.py",
		"./pkg/app.py",
		target,
		"pkg",
		"missing.py",
		outsideTarget,
		filepath.Join("..", filepath.Base(outside), "outside.py"),
	})
	want := []string{"pkg/app.py"}

	if len(selected) != len(want) || selected[0] != want[0] {
		t.Fatalf("existingLintIndexPaths() = %#v, want %#v", selected, want)
	}
}

func TestIngestLintTraceFileResolvesRelativeTraceFromRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	traceDir := filepath.Join(root, ".coding-ethos", "lint-runs")
	err := os.MkdirAll(traceDir, 0o700)
	if err != nil {
		t.Fatalf("create trace dir: %v", err)
	}

	tracePath := filepath.Join(traceDir, "relative-trace.json")
	payload, err := json.Marshal(lint.TraceRecord{
		SchemaVersion: evidence.SchemaVersion,
		TraceID:       "relative-trace",
		RecordedAtUTC: "2026-05-25T00:00:00Z",
		RepoRoot:      root,
		Result:        lint.Result{Scope: "tool:ruff", Status: "resolved"},
	})
	if err != nil {
		t.Fatalf("marshal lint trace: %v", err)
	}

	err = os.WriteFile(
		tracePath,
		payload,
		0o600,
	)
	if err != nil {
		t.Fatalf("write trace: %v", err)
	}

	err = IngestLintTraceFile(
		context.Background(),
		root,
		filepath.Join(".coding-ethos", "lint-runs", "relative-trace.json"),
	)
	if err != nil {
		t.Fatalf("ingest relative lint trace: %v", err)
	}

	store, err := OpenReadOnly(context.Background(), DefaultDBPath(root))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	stats, err := store.Stats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.Traces != 1 {
		t.Fatalf("stats = %#v, want one ingested trace", stats)
	}
}

func TestSQLiteReadOnlyStoreDSNUsesReadOnlyMode(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{
			name: "plain path",
			path: "/tmp/code-intel.db",
			want: "file:/tmp/code-intel.db?mode=ro&_pragma=busy_timeout(30000)",
		},
		{
			name: "existing query",
			path: "file:/tmp/code-intel.db?cache=shared",
			want: "file:/tmp/code-intel.db?cache=shared&mode=ro&_pragma=busy_timeout(30000)",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := sqliteReadOnlyStoreDSN(test.path); got != test.want {
				t.Fatalf(
					"sqliteReadOnlyStoreDSN(%q) = %q, want %q",
					test.path,
					got,
					test.want,
				)
			}
		})
	}
}

func TestSQLiteStoreStatPathStripsDSNParts(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name string
		path string
		want string
	}{
		{
			name: "plain path",
			path: "/tmp/code-intel.db",
			want: "/tmp/code-intel.db",
		},
		{
			name: "file dsn query",
			path: "file:/tmp/code-intel.db?cache=shared",
			want: "/tmp/code-intel.db",
		},
		{
			name: "file dsn fragment",
			path: "file:/tmp/code-intel.db#fragment",
			want: "/tmp/code-intel.db",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := sqliteStoreStatPath(test.path); got != test.want {
				t.Fatalf("sqliteStoreStatPath(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestOpenReadOnlyStatsFileDSNPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "code-intel.db")

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open writable store: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close writable store: %v", err)
	}

	readOnlyStore, err := OpenReadOnly(
		ctx,
		"file:"+filepath.ToSlash(dbPath)+"?cache=shared",
	)
	if err != nil {
		t.Fatalf("open read-only file dsn store: %v", err)
	}
	defer readOnlyStore.Close()
}
