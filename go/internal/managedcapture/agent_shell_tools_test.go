// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package managedcapture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAgentShellToolExecutableCopiesExecutableIntoTrustedRunDir(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".coding-ethos", "cache", "agent-shell", "run-test")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}
	realGit := filepath.Join(runDir, "real-git")
	if err := os.WriteFile(realGit, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write real git bind: %v", err)
	}

	toolDir := filepath.Join(root, "build", "toolchain", "go-bin")
	if err := os.MkdirAll(toolDir, 0o755); err != nil {
		t.Fatalf("create tool dir: %v", err)
	}
	tool := filepath.Join(toolDir, "golangci-lint")
	payload := []byte("#!/bin/sh\necho lint\n")
	if err := os.WriteFile(tool, payload, 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_REAL_GIT", realGit)

	got, err := agentShellToolExecutable(tool)
	if err != nil {
		t.Fatalf("agent shell tool executable: %v", err)
	}

	wantDir := filepath.Join(root, ".coding-ethos", "state", "agent-shell-tools")
	if !pathInsideOrSame(wantDir, got) {
		t.Fatalf("copied tool path = %q, want inside %q", got, wantDir)
	}
	if !strings.HasPrefix(filepath.Base(got), "tool-") {
		t.Fatalf("copied tool base = %q, want tool prefix", filepath.Base(got))
	}
	if got == tool {
		t.Fatal("tool path was not copied into active sandbox run dir")
	}

	copied, err := os.ReadFile(got)
	if err != nil {
		t.Fatalf("read copied tool: %v", err)
	}
	if string(copied) != string(payload) {
		t.Fatalf("copied payload = %q, want %q", copied, payload)
	}

	again, err := agentShellToolExecutable(tool)
	if err != nil {
		t.Fatalf("agent shell tool executable second call: %v", err)
	}
	if again != got {
		t.Fatalf("second copied tool path = %q, want reused %q", again, got)
	}
}

func TestAgentShellToolExecutableRefreshesSameSizeChangedExecutable(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".coding-ethos", "cache", "agent-shell", "run-test")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}
	realGit := filepath.Join(runDir, "real-git")
	if err := os.WriteFile(realGit, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write real git bind: %v", err)
	}

	tool := filepath.Join(root, "tool")
	first := []byte("#!/bin/sh\necho one\n")
	second := []byte("#!/bin/sh\necho two\n")
	if len(first) != len(second) {
		t.Fatal("test payloads must have equal size")
	}
	if err := os.WriteFile(tool, first, 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_REAL_GIT", realGit)

	copiedPath, err := agentShellToolExecutable(tool)
	if err != nil {
		t.Fatalf("agent shell tool executable: %v", err)
	}
	if err := os.WriteFile(tool, second, 0o755); err != nil {
		t.Fatalf("rewrite tool: %v", err)
	}

	refreshedPath, err := agentShellToolExecutable(tool)
	if err != nil {
		t.Fatalf("agent shell tool executable after rewrite: %v", err)
	}
	if refreshedPath != copiedPath {
		t.Fatalf("refreshed path = %q, want same cache path %q", refreshedPath, copiedPath)
	}

	copied, err := os.ReadFile(refreshedPath)
	if err != nil {
		t.Fatalf("read refreshed tool: %v", err)
	}
	if string(copied) != string(second) {
		t.Fatalf("refreshed payload = %q, want %q", copied, second)
	}
}

func TestAgentShellToolExecutableRejectsUntrustedRunDir(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, "cache", "agent-shell", "run-test")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatalf("create run dir: %v", err)
	}
	realGit := filepath.Join(runDir, "real-git")
	if err := os.WriteFile(realGit, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatalf("write real git bind: %v", err)
	}

	tool := filepath.Join(root, "tool")
	if err := os.WriteFile(tool, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write tool: %v", err)
	}

	t.Setenv("CODING_ETHOS_AGENT_SHELL_SANDBOX", "1")
	t.Setenv("CODING_ETHOS_REAL_GIT", realGit)

	got, err := agentShellToolExecutable(tool)
	if err != nil {
		t.Fatalf("agent shell tool executable: %v", err)
	}
	if got != tool {
		t.Fatalf("tool path = %q, want original %q", got, tool)
	}
}
