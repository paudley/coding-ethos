// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.uber.org/zap"

	"blackcat.ca/coding-ethos/go/internal/agenthookscli"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/codeintelcli"
	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/githookcli"
	"blackcat.ca/coding-ethos/go/internal/hookcli"
	"blackcat.ca/coding-ethos/go/internal/hooklogcli"
	"blackcat.ca/coding-ethos/go/internal/lintcli"
	"blackcat.ca/coding-ethos/go/internal/mcpcli"
	"blackcat.ca/coding-ethos/go/internal/outputcli"
	"blackcat.ca/coding-ethos/go/internal/policycli"
	"blackcat.ca/coding-ethos/go/internal/policygitcli"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
	"blackcat.ca/coding-ethos/go/internal/sandbox"
	"blackcat.ca/coding-ethos/go/internal/toolchaincli"
)

const (
	linuxGOOS              = "linux"
	agentShellCacheDirMode = 0o700
	agentShellAssetMode    = 0o700
	agentShellFileMode     = 0o600
	agentShellInjectedEnv  = 3
)

var (
	errExecutablePathDirectory = apperror.StaticError("executable path is a directory")
	errExecutablePathNoExecBit = apperror.StaticError(
		"executable path has no executable bit",
	)
)

type runtimeExecutor interface {
	// runLint runs policy-lint and returns a status for callers that continue.
	runLint(args ...string) int
	// execLint runs policy-lint and exits with its status.
	execLint(args ...string)
	// execAgentHook runs provider hook processing against stdin/stdout/stderr.
	execAgentHook(args ...string)
	// execAgentShell runs the approved agent shell command boundary.
	execAgentShell(paths runtimePaths, command string)
	// runInternalTool runs a bundled command and returns control to the caller.
	runInternalTool(tool string, args ...string)
	// execInternalTool runs a bundled command and exits with its status.
	execInternalTool(tool string, args ...string)
	// runTool runs a managed runtime tool and returns control to the caller.
	runTool(paths runtimePaths, tool string, args ...string)
	// execTool runs a managed runtime tool and exits with its status.
	execTool(paths runtimePaths, tool string, args ...string)
	// execPath executes a resolved path and exits with its status.
	execPath(path string, args ...string)
	// execExternal executes an external binary without managed path resolution.
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

func (defaultRuntimeExecutor) execAgentShell(paths runtimePaths, command string) {
	shellPath := "/usr/bin/env"
	args := []string{"bash", "-lc", command}
	executable := shellPath
	execArgs := args
	cwd := paths.InvocationCWD
	processEnv := os.Environ()

	if runtime.GOOS == linuxGOOS {
		plan, agentEnv, clean, err := agentShellSandboxPlan(paths, shellPath, args)
		if err != nil {
			exitErr(err)
		}

		defer clean()
		defer func() {
			closeErr := plan.Close()
			if closeErr != nil {
				exitErr(closeErr)
			}
		}()

		executable = plan.Executable
		execArgs = plan.Args
		processEnv = agentEnv
	} else {
		debuglog.Debug(
			"agent-shell.sandbox.unavailable",
			zap.String("goos", runtime.GOOS),
			zap.String("sandbox_profile", agentShellSandboxProfile(runtime.GOOS)),
			zap.Bool("sandbox_enforced", agentShellSandboxEnforced(runtime.GOOS)),
		)
	}

	process := safeexec.CommandContext(context.Background(), executable, execArgs...)
	process.Dir = paths.InvocationCWD
	process.Stdin = os.Stdin
	process.Stdout = os.Stdout
	process.Stderr = os.Stderr
	process.Env = processEnv

	if runtime.GOOS == linuxGOOS {
		process.SysProcAttr = sandbox.SysProcAttr(nil, sandbox.Evidence{
			Enabled:           true,
			NamespaceEnforced: true,
			ProcessIsolated:   true,
			NetworkIsolated:   false,
			RequiresNetwork:   true,
		})
	}

	argv := append([]string{executable}, execArgs...)
	startedAt := debuglog.ProcessEnter(
		argv,
		cwd,
		zap.String("runtime_tool", "agent-shell"),
		zap.String("consumer_root", paths.Root),
		zap.String("strategic_intent", agentShellStrategicIntent()),
	)
	err := process.Run()
	debuglog.ProcessExit(
		startedAt,
		argv,
		cwd,
		runtimeCommandExitCode(err),
		err,
		zap.String("runtime_tool", "agent-shell"),
		zap.String("consumer_root", paths.Root),
		zap.String("strategic_intent", agentShellStrategicIntent()),
	)

	if err != nil {
		exitErr(err)
	}

	requestRuntimeExit(0)
}

func agentShellSandboxPlan(
	paths runtimePaths,
	executable string,
	args []string,
) (sandbox.Plan, []string, func(), error) {
	realGitPath, err := agentShellRealGitPath(paths)
	if err != nil {
		return sandbox.Plan{}, nil, func() {}, err
	}

	gitWrapper, realGitBind, cleanup, err := agentShellGitWrapper(paths, realGitPath)
	if err != nil {
		return sandbox.Plan{}, nil, cleanup, err
	}

	agentWriteDirs := []string{
		filepath.Join(paths.Root, sandbox.SandboxTempWritePath),
		filepath.Join(paths.Root, ".coding-ethos", "cache"),
		filepath.Join(paths.Root, ".coding-ethos", "state"),
		filepath.Join(paths.Root, ".coding-ethos", "lint-runs"),
	}

	agentWritePaths, err := agentShellWorktreeWritePaths(paths.Root)
	if err != nil {
		cleanup()

		return sandbox.Plan{}, nil, func() {}, err
	}

	agentWritePaths = append(agentWritePaths, agentWriteDirs...)
	for _, dir := range agentWriteDirs {
		err = os.MkdirAll(dir, agentShellCacheDirMode)
		if err != nil {
			cleanup()

			return sandbox.Plan{}, nil, func() {}, fmt.Errorf(
				"create agent-shell write directory %s: %w",
				dir,
				err,
			)
		}
	}

	plan, err := sandbox.BuildPlan(sandbox.Request{
		Cwd:         paths.InvocationCWD,
		RepoRoot:    paths.Root,
		Executable:  executable,
		WrapperPath: filepath.Join(paths.BinDir, "coding-ethos-sandbox"),
		Args:        args,
		Tool:        "agent-shell",
		Capabilities: sandbox.Capabilities{
			SandboxProfile:  "agent-shell",
			StrategicIntent: agentShellStrategicIntent(),
			WritePaths:      append([]string{paths.GitCommonDir}, agentWritePaths...),
			AllowGitWrites:  true,
			RequiresGit:     true,
			RequiresNetwork: true,
			Tags:            []string{"agent-shell", "path-git-wrapper"},
		},
	})
	if err != nil {
		cleanup()

		return sandbox.Plan{}, nil, func() {}, fmt.Errorf(
			"build agent-shell sandbox: %w",
			err,
		)
	}

	return plan, agentShellProcessEnv(gitWrapper, realGitBind), cleanup, nil
}

func agentShellWorktreeWritePaths(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read agent-shell worktree root %s: %w", root, err)
	}

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if protectedAgentShellWorktreeEntry(name) {
			continue
		}

		paths = append(paths, filepath.Join(root, name))
	}

	return paths, nil
}

func protectedAgentShellWorktreeEntry(name string) bool {
	return name == ".git" ||
		name == ".coding-ethos" ||
		name == ".code-ethos"
}

func agentShellProcessEnv(gitWrapper, realGitBind string) []string {
	env := os.Environ()
	wrapperDir := filepath.Dir(gitWrapper)
	pathValue := wrapperDir + string(os.PathListSeparator) + os.Getenv("PATH")

	filtered := make([]string, 0, len(env)+agentShellInjectedEnv)
	for _, item := range env {
		if strings.HasPrefix(item, "PATH=") ||
			strings.HasPrefix(item, realgit.Env+"=") ||
			strings.HasPrefix(item, "CODING_ETHOS_AGENT_SHELL_SANDBOX=") {
			continue
		}

		filtered = append(filtered, item)
	}

	return append(
		filtered,
		"PATH="+pathValue,
		realgit.Env+"="+realGitBind,
		"CODING_ETHOS_AGENT_SHELL_SANDBOX=1",
	)
}

func agentShellRealGitPath(paths runtimePaths) (string, error) {
	if realgit.UsableCandidate(paths.RunBinary, paths.RealGit) {
		return paths.RealGit, nil
	}

	for _, candidate := range []string{
		"/usr/bin/git",
		"/bin/git",
		"/usr/local/bin/git",
		"/opt/homebrew/bin/git",
	} {
		if realgit.UsableCandidate(paths.RunBinary, candidate) {
			return candidate, nil
		}
	}

	return "", apperror.StaticError(
		"agent-shell could not resolve a non-wrapper host git executable",
	)
}

func agentShellGitWrapper(
	paths runtimePaths,
	realGitPath string,
) (string, string, func(), error) {
	tempRoot := filepath.Join(paths.Root, ".coding-ethos", "cache", "agent-shell")

	err := os.MkdirAll(tempRoot, agentShellCacheDirMode)
	if err != nil {
		return "", "", func() {}, fmt.Errorf(
			"create agent-shell sandbox cache %s: %w",
			tempRoot,
			err,
		)
	}

	tempDir, err := os.MkdirTemp(tempRoot, "run-")
	if err != nil {
		return "", "", func() {}, fmt.Errorf("create agent-shell sandbox assets: %w", err)
	}

	cleanup := func() { _ = os.RemoveAll(tempDir) }
	realGitBind := filepath.Join(tempDir, "real-git")
	wrapper := filepath.Join(tempDir, "git")

	err = copyExecutableFile(realGitPath, realGitBind, agentShellAssetMode)
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf("create real git bind target: %w", err)
	}

	script := "#!/usr/bin/env bash\n" +
		"set -euo pipefail\n" +
		"export " + realgit.Env + "=" + shellSingleQuote(realGitBind) + "\n" +
		"export CODING_ETHOS_AGENT_SHELL_SANDBOX=1\n" +
		"exec " + shellSingleQuote(paths.RunBinary) + " policy-git \"$@\"\n"

	err = writeExecutableFile(wrapper, []byte(script))
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf("write agent-shell git wrapper: %w", err)
	}

	err = validateExecutablePath(wrapper)
	if err != nil {
		cleanup()

		return "", "", func() {}, fmt.Errorf(
			"agent-shell git wrapper is not executable: %w",
			err,
		)
	}

	return wrapper, realGitBind, cleanup, nil
}

func copyExecutableFile(source, destination string, mode os.FileMode) error {
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open source executable %s: %w", source, err)
	}

	defer func() {
		_ = input.Close()
	}()

	output, err := os.OpenFile(destination, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("open destination executable %s: %w", destination, err)
	}

	_, copyErr := io.Copy(output, input)
	closeErr := output.Close()

	if copyErr != nil {
		return fmt.Errorf("copy executable %s to %s: %w", source, destination, copyErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close destination executable %s: %w", destination, closeErr)
	}

	return nil
}

func agentShellSandboxProfile(goos string) string {
	if goos == linuxGOOS {
		return "agent-shell"
	}

	return "none"
}

func agentShellSandboxEnforced(goos string) bool {
	return goos == linuxGOOS
}

func validateExecutablePath(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat executable path %s: %w", path, err)
	}

	if info.IsDir() {
		return fmt.Errorf("%w: %s", errExecutablePathDirectory, path)
	}

	if info.Mode().Perm()&0o111 == 0 {
		return fmt.Errorf("%w: %s", errExecutablePathNoExecBit, path)
	}

	return nil
}

func writeExecutableFile(path string, content []byte) error {
	file, err := os.OpenFile(
		path,
		os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		agentShellFileMode,
	)
	if err != nil {
		return fmt.Errorf("create executable file %s: %w", path, err)
	}

	_, writeErr := file.Write(content)
	closeErr := file.Close()

	if writeErr != nil {
		return fmt.Errorf("write executable file %s: %w", path, writeErr)
	}

	if closeErr != nil {
		return fmt.Errorf("close executable file %s: %w", path, closeErr)
	}

	err = os.Chmod(path, agentShellAssetMode)
	if err != nil {
		return fmt.Errorf("mark executable file %s executable: %w", path, err)
	}

	return nil
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
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
		"coding-ethos-output":      runOutputCLI,
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

func runOutputCLI(args []string) int {
	err := outputcli.Run(context.Background(), args)
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
	return processstatus.ExitCode(err, 1)
}
