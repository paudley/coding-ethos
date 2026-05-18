// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package sandboxexec

import (
	"fmt"
	"os"
	"syscall"
)

func execSandboxedCommand(options options) error {
	// #nosec G204 -- this helper's reviewed purpose is to exec the explicit
	// managed command argv after applying sandbox policy.
	err := syscall.Exec(
		options.commandArgv[0],
		options.commandArgv,
		sandboxExecEnv(os.Environ()),
	)
	if err != nil {
		return fmt.Errorf("exec sandboxed command: %w", err)
	}

	return nil
}
