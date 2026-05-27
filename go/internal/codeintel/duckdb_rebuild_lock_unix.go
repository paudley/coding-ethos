//go:build unix

// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"errors"
	"fmt"
	"syscall"
)

func duckDBRebuildLockPIDStale(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return false, nil
	}

	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}

	return false, fmt.Errorf("inspect code-intel rebuild lock pid %d: %w", pid, err)
}
