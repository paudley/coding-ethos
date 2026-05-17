// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package testlock

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const lockFileMode = 0o600

// ProcessState serializes tests that temporarily mutate process-global state.
func ProcessState(t *testing.T, name string) {
	t.Helper()

	release := ProcessStateScope(t, name)
	t.Cleanup(release)
}

// ProcessStateScope serializes a single scoped mutation of process-global state.
func ProcessStateScope(t *testing.T, name string) func() {
	t.Helper()

	lockName := "coding-ethos-" + sanitizeLockName(name) + ".lock"
	lockPath := filepath.Join(os.TempDir(), lockName)

	lockDescriptor, err := syscall.Open(
		lockPath,
		syscall.O_CREAT|syscall.O_RDWR|syscall.O_CLOEXEC,
		lockFileMode,
	)
	if err != nil {
		t.Fatalf("open process-state test lock: %v", err)
	}

	err = syscall.Flock(lockDescriptor, syscall.LOCK_EX)
	if err != nil {
		closeErr := syscall.Close(lockDescriptor)
		if closeErr != nil {
			t.Fatalf("lock process-state test lock: %v; close lock: %v", err, closeErr)
		}

		t.Fatalf("lock process-state test lock: %v", err)
	}

	return func() {
		unlockErr := syscall.Flock(lockDescriptor, syscall.LOCK_UN)
		closeErr := syscall.Close(lockDescriptor)

		if unlockErr != nil {
			t.Fatalf("unlock process-state test lock: %v", unlockErr)
		}

		if closeErr != nil {
			t.Fatalf("close process-state test lock: %v", closeErr)
		}
	}
}

func sanitizeLockName(name string) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-")

	return replacer.Replace(name)
}
