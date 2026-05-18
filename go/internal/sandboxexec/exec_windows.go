// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package sandboxexec

import (
	"fmt"
	"os"
)

type sandboxExitError struct {
	code int
}

func (err sandboxExitError) Error() string {
	return fmt.Sprintf("sandboxed command exited with status %d", err.code)
}

func (err sandboxExitError) ExitCode() int {
	return err.code
}

func execSandboxedCommand(options options) error {
	process, err := os.StartProcess(
		options.commandArgv[0],
		options.commandArgv,
		&os.ProcAttr{
			Dir:   options.paths.cwd,
			Env:   sandboxExecEnv(os.Environ()),
			Files: []*os.File{os.Stdin, os.Stdout, os.Stderr},
		},
	)
	if err != nil {
		return fmt.Errorf("start sandboxed command: %w", err)
	}

	state, waitErr := process.Wait()
	if waitErr != nil {
		return fmt.Errorf("wait for sandboxed command: %w", waitErr)
	}

	if !state.Success() {
		return sandboxExitError{code: state.ExitCode()}
	}

	return nil
}
