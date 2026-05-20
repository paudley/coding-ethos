// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package testlock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProcessStateScopeCreatesSanitizedLock(t *testing.T) {
	release := ProcessStateScope(t, "path with/slash")
	release()

	lockPath := filepath.Join(os.TempDir(), "coding-ethos-path-with-slash.lock")
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stat lock path: %v", err)
	}
}

func TestSanitizeLockName(t *testing.T) {
	t.Parallel()

	got := sanitizeLockName(`path with\slash`)
	want := "path-with-slash"
	if got != want {
		t.Fatalf("sanitizeLockName() = %q, want %q", got, want)
	}
}
