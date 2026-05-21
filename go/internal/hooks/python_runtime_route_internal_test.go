// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

func TestRewritePythonRuntimeCommandUsesUVToml(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := os.Mkdir(filepath.Join(root, ".git"), 0o755)
	if err != nil {
		t.Fatalf("mkdir .git: %v", err)
	}

	err = os.WriteFile(filepath.Join(root, "uv.toml"), nil, 0o600)
	if err != nil {
		t.Fatalf("write uv config: %v", err)
	}

	if !pythonRuntimeAvailable(root) {
		t.Fatalf("uv runtime not available at %s", root)
	}

	if got := pythonRuntimeRoot(root); got != root {
		t.Fatalf("runtime root = %q, want %q", got, root)
	}

	rewritten, ok := rewritePythonRuntimeCommandChain("python script.py", root)
	if !ok {
		t.Fatalf("rewrite failed: %q", rewritten)
	}
}

func TestPythonRuntimeRootDoesNotInheritIgnoredParentWorktree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := realgit.Command(context.Background(), false, "-C", root, "init").
		Run(); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "uv.toml"), nil, 0o600); err != nil {
		t.Fatalf("write uv config: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".gitignore"),
		[]byte("sandbox-tmp/\n"),
		0o600,
	); err != nil {
		t.Fatalf("write gitignore: %v", err)
	}

	ignoredRoot := filepath.Join(root, "sandbox-tmp", "case")
	if err := os.MkdirAll(ignoredRoot, 0o755); err != nil {
		t.Fatalf("mkdir ignored root: %v", err)
	}

	if got := pythonRuntimeRoot(ignoredRoot); got != "" {
		t.Fatalf("runtime root = %q, want empty", got)
	}
}
