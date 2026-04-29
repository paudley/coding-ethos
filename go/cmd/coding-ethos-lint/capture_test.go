// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRunCapturedToolLogsRuffTrace(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("shell fixture uses POSIX sh")
	}

	repo := t.TempDir()
	tool := filepath.Join(repo, "ruff-fixture")
	if err := os.WriteFile(
		tool,
		[]byte("#!/usr/bin/env sh\nprintf '%s\\n' 'pkg/app.py:4:8: F401 unused import'\nexit 1\n"),
		0o700,
	); err != nil {
		t.Fatalf("write fixture tool: %v", err)
	}

	exitCode := runCapturedTool("ruff", tool, repo, []string{"check", "pkg/app.py"})
	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1", exitCode)
	}

	matches, err := filepath.Glob(filepath.Join(repo, ".coding-ethos", "lint-runs", "*.json"))
	if err != nil {
		t.Fatalf("glob traces: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("trace files = %#v", matches)
	}

	content, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read trace: %v", err)
	}
	for _, want := range []string{
		`"scope": "tool:ruff"`,
		`"source_tool": "ruff"`,
		`"code": "F401"`,
		`"message": "unused import"`,
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("trace missing %q:\n%s", want, content)
		}
	}
}
