// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//go:build windows

package sandboxexec

import (
	"fmt"
	"os"
)

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
		return fmt.Errorf("sandboxed command exited unsuccessfully")
	}

	return nil
}
