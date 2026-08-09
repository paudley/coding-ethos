// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package repoignore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Repair strips ignores broad enough to hide tracked coding-ethos
// configuration, and leaves a deliberate memories ignore in place. Memories may
// be kept as local state shared between checkouts, and silently rewriting a
// tracked .gitignore to overturn that choice is not repair.
func TestRepairGitignoreStripsBroadIgnoresAndKeepsNarrowMemoryIgnores(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	err := os.WriteFile(
		path,
		[]byte(
			"build/\n.coding-ethos/**\n**/.coding-ethos/*\n.coding-ethos/memories/*.yaml\n",
		),
		0o644,
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
		".coding-ethos/**",
		"**/.coding-ethos/*",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("repaired gitignore still hides config with %q:\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, ".coding-ethos/memories/*.yaml") {
		t.Fatalf("repair discarded the deliberate memories ignore:\n%s", text)
	}
	for _, required := range RuntimePaths() {
		if !strings.Contains(text, required) {
			t.Fatalf("repaired gitignore missing %q:\n%s", required, text)
		}
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat repaired gitignore: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("repaired gitignore mode = %o, want 644", info.Mode().Perm())
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
