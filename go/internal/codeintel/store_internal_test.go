// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import "testing"

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
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			if got := sqliteStoreDSN(test.path); got != test.want {
				t.Fatalf("sqliteStoreDSN(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}
