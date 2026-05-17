// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"os"
	"path/filepath"
	"testing"
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
