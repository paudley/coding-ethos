// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
)

func Execute(realGit string, argv []string) error {
	if realGit == "" {
		realGit = "git"
	}
	normalized := normalizeArgv(argv)
	cmd := exec.Command(realGit, normalized[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return ExitCodeError{Code: exitError.ExitCode()}
		}
		return fmt.Errorf("execute git: %w", err)
	}
	return nil
}

type ExitCodeError struct {
	Code int
}

func (err ExitCodeError) Error() string {
	return fmt.Sprintf("git exited with status %d", err.Code)
}
