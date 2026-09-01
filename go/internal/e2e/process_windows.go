// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package e2e

import (
	"errors"
	"os"
	"os/exec"
)

func configureCommandProcessGroup(_ *exec.Cmd) {}

func configureCommandCancellation(cmd *exec.Cmd) {
	cmd.WaitDelay = commandWaitDelay
}

func terminateCommandProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}

	err := cmd.Process.Kill()
	if errors.Is(err, os.ErrProcessDone) {
		return os.ErrProcessDone
	}

	return err
}
