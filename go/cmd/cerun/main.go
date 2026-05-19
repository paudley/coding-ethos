// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const cerunMissingRuntimeExitCode = 127

func main() {
	execguard.Enter("cerun")

	runner, err := siblingRunner()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cerun: %s\n", err)
		os.Exit(cerunMissingRuntimeExitCode)
	}

	command := safeexec.CommandContext(
		context.Background(),
		runner,
		append([]string{"agent-shell"}, os.Args[1:]...)...,
	)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	err = command.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cerun: exec %s: %s\n", runner, err)
		os.Exit(cerunExitCode(err))
	}
}

func cerunExitCode(err error) int {
	if err == nil {
		return 0
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return cerunMissingRuntimeExitCode
	}

	return processstatus.ExitCode(err, 1)
}

func siblingRunner() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}

	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}

	return filepath.Join(filepath.Dir(executable), "coding-ethos-run"), nil
}
