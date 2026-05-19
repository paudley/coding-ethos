// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/agenthookscli"
	"blackcat.ca/coding-ethos/go/internal/codeintelcli"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/githookcli"
	"blackcat.ca/coding-ethos/go/internal/hookcli"
	"blackcat.ca/coding-ethos/go/internal/hooklogcli"
	"blackcat.ca/coding-ethos/go/internal/lintcli"
	"blackcat.ca/coding-ethos/go/internal/mcpcli"
	"blackcat.ca/coding-ethos/go/internal/policycli"
	"blackcat.ca/coding-ethos/go/internal/policygitcli"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/toolchaincli"
)

type runtimeExecutor interface {
	runLint(args ...string) int
	execLint(args ...string)
	execAgentHook(args ...string)
	runInternalTool(tool string, args ...string)
	execInternalTool(tool string, args ...string)
	runTool(paths runtimePaths, tool string, args ...string)
	execTool(paths runtimePaths, tool string, args ...string)
	execPath(path string, args ...string)
	execExternal(path string, args ...string)
}

type defaultRuntimeExecutor struct{}

func (defaultRuntimeExecutor) runLint(args ...string) int {
	return lintcli.Run(args)
}

func (defaultRuntimeExecutor) execLint(args ...string) {
	requestRuntimeExit(lintcli.Run(args))
}

func (defaultRuntimeExecutor) execAgentHook(args ...string) {
	requestRuntimeExit(hookcli.Run(args, os.Stdin, os.Stdout, os.Stderr))
}

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
	requestRuntimeExit(exitMissing)
}

func gitOutput(realGit, dir string, args ...string) (string, error) {
	output, err := gitOutputRaw(realGit, dir, args...)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(output), nil
}

func gitOutputRaw(realGit, dir string, args ...string) (string, error) {
	command := realgit.CommandFor(context.Background(), realGit, false, args...)
	if dir != "" {
		command.Dir = dir
	}

	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}

func runtimeRunTool(paths runtimePaths, tool string, args ...string) {
	if isInternalRuntimeTool(tool) {
		paths.executor().runInternalTool(tool, args...)

		return
	}

	paths.executor().runTool(paths, tool, args...)
}

func runtimeExecTool(paths runtimePaths, tool string, args ...string) {
	if isInternalRuntimeTool(tool) {
		paths.executor().execInternalTool(tool, args...)

		return
	}

	paths.executor().execTool(paths, tool, args...)
}

func runtimeRunLint(paths runtimePaths, args ...string) int {
	return paths.executor().runLint(args...)
}

func runtimeExecLint(paths runtimePaths, args ...string) {
	paths.executor().execLint(args...)
}

func runtimeExecExternal(paths runtimePaths, path string, args ...string) {
	paths.executor().execExternal(path, args...)
}

func (defaultRuntimeExecutor) runTool(paths runtimePaths, tool string, args ...string) {
	requireExternalRuntimeTool(tool)

	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	command := safeexec.CommandContext(context.Background(), toolPath, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	command.Stdin = os.Stdin

	argv := append([]string{toolPath}, args...)
	startedAt := debuglog.ProcessEnter(
		argv,
		paths.InvocationCWD,
		zap.String("runtime_tool", tool),
	)
	err := command.Run()
	debuglog.ProcessExit(
		startedAt,
		argv,
		paths.InvocationCWD,
		runtimeCommandExitCode(err),
		err,
		zap.String("runtime_tool", tool),
	)
	if err != nil {
		exitErr(err)
	}
}

func (executor defaultRuntimeExecutor) execTool(
	paths runtimePaths,
	tool string,
	args ...string,
) {
	requireExternalRuntimeTool(tool)

	toolPath := filepath.Join(paths.BinDir, tool)
	requireRuntimeBinary(toolPath, tool)
	executor.execPath(toolPath, args...)
}

func requireExternalRuntimeTool(tool string) {
	if strings.HasPrefix(filepath.Base(tool), "coding-ethos-") {
		runtimeFailure(
			fmt.Sprintf("coding-ethos command %s is not registered for direct execution", tool),
		)
	}
}

func isInternalRuntimeTool(tool string) bool {
	_, ok := internalToolHandlers()[tool]

	return ok
}

func (defaultRuntimeExecutor) runInternalTool(tool string, args ...string) {
	handler, ok := internalToolHandlers()[tool]
	if !ok {
		runtimeFailure(
			fmt.Sprintf("coding-ethos command %s is not registered for direct execution", tool),
		)
	}

	code := handler(args)
	if code != 0 {
		requestRuntimeExit(code)
	}
}

func (defaultRuntimeExecutor) execInternalTool(tool string, args ...string) {
	handler, ok := internalToolHandlers()[tool]
	if !ok {
		runtimeFailure(
			fmt.Sprintf("coding-ethos command %s is not registered for direct execution", tool),
		)
	}

	requestRuntimeExit(handler(args))
}

type internalToolHandler func([]string) int

func internalToolHandlers() map[string]internalToolHandler {
	return map[string]internalToolHandler{
		"coding-ethos-agent-hooks": agenthookscli.Run,
		"coding-ethos-code-intel":  runCodeIntelCLI,
		"coding-ethos-git":         runPolicyGitCLI,
		"coding-ethos-git-hook":    githookcli.Run,
		"coding-ethos-hook":        runHookCLI,
		"coding-ethos-hook-log":    runHookLogCLI,
		"coding-ethos-mcp":         runMCPCLI,
		"coding-ethos-policy":      policycli.Run,
		"coding-ethos-toolchain":   toolchaincli.Run,
	}
}

func runCodeIntelCLI(args []string) int {
	err := codeintelcli.Run(context.Background(), args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)

		return 1
	}

	return 0
}

func runPolicyGitCLI(args []string) int {
	err := policygitcli.Run(args)
	if err != nil {
		exitErr(err)
	}

	return 0
}

func runHookCLI(args []string) int {
	return hookcli.Run(args, os.Stdin, os.Stdout, os.Stderr)
}

func runHookLogCLI(args []string) int {
	err := hooklogcli.Run(args, os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		exitErr(err)
	}

	return 0
}

func runMCPCLI(args []string) int {
	err := mcpcli.Run(args, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)

		return 1
	}

	return 0
}

func (executor defaultRuntimeExecutor) execPath(path string, args ...string) {
	executor.execExternal(path, args...)
}

func (defaultRuntimeExecutor) execExternal(path string, args ...string) {
	command := safeexec.CommandContext(context.Background(), path, args...)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	command.Stdin = os.Stdin

	argv := append([]string{path}, args...)
	startedAt := debuglog.ProcessEnter(argv, "", zap.Bool("runtime_external", true))
	err := command.Run()
	debuglog.ProcessExit(
		startedAt,
		argv,
		"",
		runtimeCommandExitCode(err),
		err,
		zap.Bool("runtime_external", true),
	)
	if err != nil {
		exitErr(err)
	}

	requestRuntimeExit(0)
}

func runtimeCommandExitCode(err error) int {
	if err == nil {
		return 0
	}

	return 1
}
