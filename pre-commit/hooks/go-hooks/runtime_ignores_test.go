// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import "testing"

func TestRuntimeIgnoreFindingsSkipsEmptyPaths(t *testing.T) {
	t.Parallel()

	findings := runtimeIgnoreFindings([]string{"", "   "})
	if len(findings) != 0 {
		t.Fatalf("runtimeIgnoreFindings() = %#v, want no findings", findings)
	}
}
