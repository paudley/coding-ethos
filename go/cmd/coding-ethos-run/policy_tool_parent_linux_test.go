// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// The actionlint ShellCheck protocol only trusts a parent it can name, so
// resolution must return the real executable behind the parent process.
func TestPolicyToolParentExecutableResolvesRunningParent(t *testing.T) {
	t.Parallel()

	parentExecutable, err := policyToolParentExecutable()
	if err != nil {
		t.Fatalf("resolve parent executable: %v", err)
	}

	if !filepath.IsAbs(parentExecutable) {
		t.Fatalf("parent executable %q is not an absolute path", parentExecutable)
	}

	expected, err := os.Readlink(
		filepath.Join("/proc", strconv.Itoa(os.Getppid()), "exe"),
	)
	if err != nil {
		t.Fatalf("read parent executable link: %v", err)
	}

	if parentExecutable != expected {
		t.Fatalf("parent executable = %q, want %q", parentExecutable, expected)
	}
}
