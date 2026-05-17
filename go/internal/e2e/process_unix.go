// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package e2e

import (
	"errors"
	"os/exec"
	"syscall"
)

func configureCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateCommandProcessGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}

	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err != nil && !errors.Is(err, syscall.ESRCH) {
		return
	}
}
