// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package repoignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairGitignoreKeepsMemoriesTrackable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	err := os.WriteFile(
		path,
		[]byte("build/\n.coding-ethos/\n.coding-ethos/memories/*.yaml\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	changed, err := RepairGitignore(root)
	if err != nil {
		t.Fatalf("repair gitignore: %v", err)
	}
	if !changed {
		t.Fatalf("RepairGitignore changed = false, want true")
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read repaired gitignore: %v", err)
	}
	text := string(payload)
	for _, forbidden := range []string{
		"\n.coding-ethos/\n",
		".coding-ethos/memories/*.yaml",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("repaired gitignore still blocks memories with %q:\n%s", forbidden, text)
		}
	}
	for _, required := range RuntimePaths() {
		if !strings.Contains(text, required) {
			t.Fatalf("repaired gitignore missing %q:\n%s", required, text)
		}
	}
}

func TestRepairGitignoreIsIdempotent(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	changed, err := RepairGitignore(root)
	if err != nil {
		t.Fatalf("initial repair gitignore: %v", err)
	}
	if !changed {
		t.Fatalf("initial RepairGitignore changed = false, want true")
	}

	changed, err = RepairGitignore(root)
	if err != nil {
		t.Fatalf("second repair gitignore: %v", err)
	}
	if changed {
		t.Fatalf("second RepairGitignore changed = true, want false")
	}
}
