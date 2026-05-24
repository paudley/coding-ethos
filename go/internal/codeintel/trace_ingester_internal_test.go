// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"reflect"
	"testing"
)

func TestRMIntentPathsHandlesInteractiveAndSentinel(t *testing.T) {
	t.Parallel()

	got := rmIntentPaths([]string{
		"--interactive",
		"pkg/keep.py",
		"--",
		"--literal.py",
		"pkg/delete.py",
	})
	want := []string{"pkg/keep.py", "--literal.py", "pkg/delete.py"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rmIntentPaths() = %#v, want %#v", got, want)
	}
}

func TestCleanRepoRelativeIntentPathRejectsAbsolutePaths(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"/tmp/delete.py",
		`C:\repo\delete.py`,
	} {
		if got, ok := cleanRepoRelativeIntentPath(path); ok {
			t.Fatalf("cleanRepoRelativeIntentPath(%q) = %q, true; want false", path, got)
		}
	}
}
