// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package e2e

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"golang.org/x/sys/windows"
)

func configureCommandProcessGroup(_ *exec.Cmd) {}

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

	if windowsProcessExited(cmd.Process.Pid) {
		return os.ErrProcessDone
	}

	terminationContext, cancel := context.WithTimeout(
		context.Background(),
		commandWaitDelay,
	)
	defer cancel()

	command := exec.CommandContext(
		terminationContext,
		"taskkill.exe",
		"/PID", strconv.Itoa(cmd.Process.Pid),
		"/T",
		"/F",
	)
	err := command.Run()
	if err == nil {
		return nil
	}
	if windowsProcessExited(cmd.Process.Pid) {
		return os.ErrProcessDone
	}

	return fmt.Errorf("terminate command process tree %d: %w", cmd.Process.Pid, err)
}

func windowsProcessExited(pid int) bool {
	handle, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
		return true
	}
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	status, err := windows.WaitForSingleObject(handle, 0)

	return err == nil && status == windows.WAIT_OBJECT_0
}
