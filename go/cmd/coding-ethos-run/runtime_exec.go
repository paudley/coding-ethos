// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var (
	runtimeRunTool      = runTool
	runtimeExecTool     = execTool
	runtimeExecPath     = execPath
	runtimeExecExternal = execExternal
)

func requirePolicyBundle(paths runtimePaths) {
	requireRuntimeFile(paths.PolicyBundle, "compiled policy bundle")
}

func requireRuntimeFile(path string, description string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		runtimeFailure(fmt.Sprintf("missing %s: %s", description, path))
	}
}

func requireRuntimeBinary(path string, description string) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		runtimeFailure(fmt.Sprintf("missing or non-executable %s: %s", description, path))
	}
}

func runtimeFailure(problem string) {
	fmt.Fprintln(os.Stderr, "FATAL: coding-ethos hook runtime is missing or invalid")
	fmt.Fprintln(os.Stderr, "This is not caused by the files being committed.")
	fmt.Fprintf(os.Stderr, "problem: %s\n", problem)
	fmt.Fprintln(os.Stderr, "action: run make build, or ask an admin to repair the coding-ethos checkout.")
	os.Exit(exitMissing)
}

func gitOutput(realGit string, dir string, args ...string) (string, error) {
	command := exec.Command(realGit, args...)
	if dir != "" {
		command.Dir = dir
	}
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(output)), nil
}

func runTool(paths runtimePaths, tool string, args ...string) {
	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	command := exec.Command(toolPath, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		exitErr(err)
	}
}

func execTool(paths runtimePaths, tool string, args ...string) {
	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	execPath(toolPath, args...)
}

func execPath(path string, args ...string) {
	execExternal(path, args...)
}

func execExternal(path string, args ...string) {
	command := exec.Command(path, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Stdin = os.Stdin
	if err := command.Run(); err != nil {
		exitErr(err)
	}
	os.Exit(0)
}
