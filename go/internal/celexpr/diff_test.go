// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/celexpr"
)

func TestParseDiffHunksUnquotesDiffPaths(t *testing.T) {
	t.Parallel()

	hunks := ParseDiffHunks(
		"diff --git a/path\\ with\\ spaces.py b/path\\ with\\ spaces.py\n"+
			"--- \"a/path with spaces.py\"\n"+
			"+++ \"b/path with spaces.py\"\n"+
			"@@ -1,0 +1 @@\n"+
			"+print('ok')\n",
		[]string{"path with spaces.py"},
	)
	if len(hunks) != 1 {
		t.Fatalf("hunks = %#v, want one parsed hunk", hunks)
	}

	if hunks[0].File != "path with spaces.py" {
		t.Fatalf("hunk file = %q, want unquoted path", hunks[0].File)
	}
}
