// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package celexpr_test

import (
	"os"
	"path/filepath"
	"testing"

	. "blackcat.ca/coding-ethos/go/internal/celexpr"
)

func TestProposedSymbolChangesDetectsRenames(t *testing.T) {
	t.Parallel()

	repo := t.TempDir()
	file := filepath.Join(repo, "src", "app.py")

	err := os.MkdirAll(filepath.Dir(file), 0o700)
	if err != nil {
		t.Fatalf("create source dir: %v", err)
	}

	current := "def old_name():\n    return 1\n"

	err = os.WriteFile(file, []byte(current), 0o600)
	if err != nil {
		t.Fatalf("write source file: %v", err)
	}

	activation := Activation(ActivationInput{
		Cwd:        repo,
		Files:      []string{"src/app.py"},
		Tool:       "Edit",
		OldContent: "def old_name():",
		Content:    "def new_name():",
	})

	changes, found := activation["proposed_symbol_changes"].([]ProposedSymbolChangeInput)

	if !found {
		t.Fatalf("proposed_symbol_changes missing")
	}

	var renameFound bool

	for _, change := range changes {
		if change.Action == "renamed" {
			renameFound = true

			if change.SymbolName != "new_name" {
				t.Errorf("expected renamed symbol name 'new_name', got %q", change.SymbolName)
			}

			if change.SymbolPath != "new_name" {
				t.Errorf("expected renamed symbol path 'new_name', got %q", change.SymbolPath)
			}
		}
	}

	if !renameFound {
		t.Errorf("expected to find a 'renamed' action in changes: %#v", changes)
	}
}
