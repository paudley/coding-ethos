// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"path/filepath"
	"testing"
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
