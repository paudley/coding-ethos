// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestExternalToolEnvRemovesGitHookLocalEnvironment(t *testing.T) {
	t.Setenv("GIT_DIR", "/tmp/wrong-git-dir")
	t.Setenv("GIT_INDEX_FILE", "/tmp/wrong-index")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "user.email")
	t.Setenv("GIT_CONFIG_VALUE_0", "test@example.com")
	t.Setenv(consumerRootEnv, "/tmp/repo")
	t.Setenv(hookGroupChildEnv, hookPlanBoolTrue)
	t.Setenv(hookGroupResultPathEnv, "/tmp/result.json")

	env := externalToolEnv([]string{"KEEP_EXTRA=1"})

	for _, item := range env {
		name, _, found := strings.Cut(item, "=")
		if found && externalToolEnvBlocked(name+"=value") {
			t.Fatalf("externalToolEnv leaked %s in %#v", name, env)
		}
	}

	if !slices.Contains(env, "KEEP_EXTRA=1") {
		t.Fatalf("externalToolEnv dropped explicit extra env: %#v", env)
	}

	if !slices.Contains(env, "GIT_OPTIONAL_LOCKS=0") {
		t.Fatalf("externalToolEnv did not disable optional git locks: %#v", env)
	}
}

func TestRunExternalToolCapturesStdoutAndStderrSeparately(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	dir := t.TempDir()
	tool := filepath.Join(dir, "tool")

	err := os.WriteFile(
		tool,
		[]byte("#!/usr/bin/env sh\necho stdout-json\necho stderr-text >&2\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	err = os.Chmod(tool, 0o700)
	if err != nil {
		t.Fatalf("chmod fixture: %v", err)
	}

	result := runExternalTool(externalToolRequest{
		Name:    "fixture",
		Dir:     dir,
		Command: []string{tool},
	})
	if result.Stdout != "stdout-json" || result.Stderr != "stderr-text" {
		t.Fatalf("streams = stdout %q stderr %q", result.Stdout, result.Stderr)
	}

	if result.Combined != "stdout-json\nstderr-text" {
		t.Fatalf("combined = %q", result.Combined)
	}
}
