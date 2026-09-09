// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build !windows

package e2e

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

func configureCommandProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		return terminateCommandProcessGroup(cmd)
	}
	cmd.WaitDelay = commandWaitDelay
}

func terminateCommandProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}

	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}

	if err != nil {
		return fmt.Errorf("kill command process group %d: %w", cmd.Process.Pid, err)
	}

	return nil
}
