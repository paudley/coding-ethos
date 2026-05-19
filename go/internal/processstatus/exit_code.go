// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package processstatus normalizes process execution result metadata.
package processstatus

import (
	"errors"
	"os/exec"
)

// ExitCode returns the process exit code carried by err, or fallback for
// non-process errors. A nil error always maps to 0.
func ExitCode(err error, fallback int) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return fallback
}
