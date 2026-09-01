// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/lintcapture"
	"blackcat.ca/coding-ethos/go/toolcatalog"
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

func TestSandboxCacheEnvIsolatesUVProjectUnderConsumerCache(t *testing.T) {
	consumerRoot := t.TempDir()
	installedEthosRoot := t.TempDir()
	hookProject := filepath.Join(installedEthosRoot, "pre-commit", "hooks")
	if err := os.MkdirAll(hookProject, 0o700); err != nil {
		t.Fatalf("create installed hook project: %v", err)
	}

	uvPath := filepath.Join(t.TempDir(), "managed-uv")
	if err := os.WriteFile(uvPath, []byte("fixture"), 0o700); err != nil {
		t.Fatalf("write managed uv fixture: %v", err)
	}
	t.Setenv("UV", uvPath)

	tool, found := toolcatalog.HookOwnedTool("pyupgrade")
	if !found {
		t.Fatal("missing pyupgrade tool")
	}

	request, exitCode, err := managedCaptureRequest(
		tool,
		lintcapture.RuntimeConfig{},
		Options{
			Tool:          "pyupgrade",
			EthosRoot:     installedEthosRoot,
			ConsumerRoot:  consumerRoot,
			InvocationCwd: consumerRoot,
			Args:          []string{"--version"},
		},
	)
	if err != nil {
		t.Fatalf("managedCaptureRequest exit=%d: %v", exitCode, err)
	}
	if request.UVProject != hookProject {
		t.Fatalf("UV project = %q, want %q", request.UVProject, hookProject)
	}

	environment, err := sandboxCacheEnv(context.Background(), request)
	if err != nil {
		t.Fatalf("sandboxCacheEnv: %v", err)
	}

	digest := sha256.Sum256([]byte(hookProject))
	want := filepath.Join(
		consumerRoot,
		".coding-ethos",
		"cache",
		"uv-project-env",
		hex.EncodeToString(digest[:]),
	)
	if environment.UVProjectEnvironment != want {
		t.Fatalf(
			"UV_PROJECT_ENVIRONMENT = %q, want %q",
			environment.UVProjectEnvironment,
			want,
		)
	}
	if strings.HasPrefix(
		environment.UVProjectEnvironment,
		hookProject+string(os.PathSeparator),
	) {
		t.Fatalf("UV project environment is inside installed hook project: %q", want)
	}
	if info, statErr := os.Stat(want); statErr != nil || !info.IsDir() {
		t.Fatalf("UV project environment is not usable: info=%v error=%v", info, statErr)
	}

	hostValue := filepath.Join(installedEthosRoot, "host-uv-project")
	processEnv := capturedProcessEnv(
		[]string{"UV_PROJECT_ENVIRONMENT=" + hostValue},
		environment,
		"pyupgrade",
	)
	for _, item := range processEnv {
		if item == "UV_PROJECT_ENVIRONMENT="+hostValue {
			t.Fatalf("captured process retained host UV project environment")
		}
		if item == "UV_PROJECT_ENVIRONMENT="+want {
			return
		}
	}

	t.Fatalf("captured process omitted UV project environment")
}
