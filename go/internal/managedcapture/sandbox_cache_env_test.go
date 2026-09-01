// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/sandbox"
)

func TestSandboxCacheEnvCreatesConsumerGoPath(t *testing.T) {
	root := t.TempDir()

	environment, err := sandboxCacheEnv(context.Background(), captureRequest{
		Cwd:       root,
		TraceRoot: root,
		Tool:      "ruff",
	})
	if err != nil {
		t.Fatalf("sandboxCacheEnv: %v", err)
	}

	wantGoPath := filepath.Join(root, sandbox.SandboxGoPath)
	wantGoModCache := filepath.Join(root, sandbox.SandboxGoModCachePath)
	if environment.GoPath != wantGoPath {
		t.Fatalf("GOPATH = %q, want %q", environment.GoPath, wantGoPath)
	}
	if environment.GoModCache != wantGoModCache {
		t.Fatalf("GOMODCACHE = %q, want %q", environment.GoModCache, wantGoModCache)
	}

	for name, path := range map[string]string{
		"GOPATH":     environment.GoPath,
		"GOMODCACHE": environment.GoModCache,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil || !info.IsDir() {
			t.Fatalf("%s directory is not usable: info=%v error=%v", name, info, statErr)
		}
	}
}
