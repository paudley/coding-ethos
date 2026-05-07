// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

type runtimeExecutor interface {
	runTool(paths runtimePaths, tool string, args ...string)
	execTool(paths runtimePaths, tool string, args ...string)
	execPath(path string, args ...string)
	execExternal(path string, args ...string)
}

type defaultRuntimeExecutor struct{}

func requirePolicyBundle(paths runtimePaths) {
	requireRuntimeFile(paths.PolicyBundle, "compiled policy bundle")
}

func requireRuntimeFile(path, description string) {
	info, err := statPathWithRoot(path)
	if err != nil || info.IsDir() {
		runtimeFailure(fmt.Sprintf("missing %s: %s", description, path))
	}
}

func requireRuntimeBinary(path, description string) {
	info, err := statPathWithRoot(path)
	if err != nil || info.IsDir() || info.Mode()&0o111 == 0 {
		runtimeFailure(
			fmt.Sprintf("missing or non-executable %s: %s", description, path),
		)
	}
}

func statPathWithRoot(path string) (os.FileInfo, error) {
	rootPath := filepath.Dir(path)

	root, err := os.OpenRoot(rootPath)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", rootPath, err)
	}
	defer root.Close()

	info, err := root.Stat(filepath.Base(path))
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}

	return info, nil
}

func runtimeFailure(problem string) {
	fmt.Fprintln(os.Stderr, "FATAL: coding-ethos hook runtime is missing or invalid")
	fmt.Fprintln(os.Stderr, "This is not caused by the files being committed.")
	fmt.Fprintf(os.Stderr, "problem: %s\n", problem)
	fmt.Fprintln(
		os.Stderr,
		"action: run make build, or ask an admin to repair the coding-ethos checkout.",
	)
	os.Exit(exitMissing)
}

func gitOutput(realGit, dir string, args ...string) (string, error) {
	command := safeexec.CommandContext(context.Background(), realGit, args...)
	if dir != "" {
		command.Dir = dir
	}

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return strings.TrimSpace(string(output)), nil
}

func runtimeRunTool(paths runtimePaths, tool string, args ...string) {
	paths.executor().runTool(paths, tool, args...)
}

func runtimeExecTool(paths runtimePaths, tool string, args ...string) {
	paths.executor().execTool(paths, tool, args...)
}

func runtimeExecPath(paths runtimePaths, path string, args ...string) {
	paths.executor().execPath(path, args...)
}

func runtimeExecExternal(paths runtimePaths, path string, args ...string) {
	paths.executor().execExternal(path, args...)
}

func (defaultRuntimeExecutor) runTool(paths runtimePaths, tool string, args ...string) {
	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	command := safeexec.CommandContext(context.Background(), toolPath, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	command.Stdin = os.Stdin

	err := command.Run()
	if err != nil {
		exitErr(err)
	}
}

func (executor defaultRuntimeExecutor) execTool(
	paths runtimePaths,
	tool string,
	args ...string,
) {
	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	executor.execPath(toolPath, args...)
}

func (executor defaultRuntimeExecutor) execPath(path string, args ...string) {
	executor.execExternal(path, args...)
}

func (defaultRuntimeExecutor) execExternal(path string, args ...string) {
	command := safeexec.CommandContext(context.Background(), path, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	command.Stdin = os.Stdin

	err := command.Run()
	if err != nil {
		exitErr(err)
	}

	os.Exit(0)
}
