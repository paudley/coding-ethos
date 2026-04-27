// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators_test

import (
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/evaluators"
)

func TestBlockedAdminFilesFindsBasenamesAndDirs(t *testing.T) {
	t.Parallel()

	blocked := BlockedAdminFiles([]string{
		"pyproject.toml",
		"src/app.py",
		"pre-commit/hooks/run.sh",
		"docs/notes.md",
	})
	if len(blocked) != 2 {
		t.Fatalf("blocked count mismatch: %#v", blocked)
	}

	if blocked[0] != "pyproject.toml" || blocked[1] != "pre-commit/hooks/run.sh" {
		t.Fatalf("blocked files mismatch: %#v", blocked)
	}
}
