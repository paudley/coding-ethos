// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
)

const agentShellBlockedExitCode = 2

func run(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		return apperror.StaticError("coding-ethos-run requires a command")
	}

	command := args[0]
	rest := args[1:]

	handler, found := runCommandHandler(command)
	if !found {
		return apperror.Wrapf(
			apperror.StaticError("unknown coding-ethos-run command"),
			"unknown coding-ethos-run command %q",
			command,
		)
	}

	return handler(paths, rest)
}

type runHandler func(runtimePaths, []string) error

type runCommandEntry struct {
	Handler runHandler
	Command string
}

func runCommandHandler(command string) (runHandler, bool) {
	for _, entry := range runCommandEntries() {
		if entry.Command == command {
			return entry.Handler, true
		}
	}

	return nil, false
}

func runCommandEntries() []runCommandEntry {
	return []runCommandEntry{
		{Command: "agent-hook", Handler: runAgentHookHandler},
		{Command: "agent-shell", Handler: runAgentShellHandler},
		{Command: "git-hook", Handler: runGitHook},
		{Command: "lfs-hook", Handler: runLFSHook},
		{Command: "agent-hooks", Handler: runAgentHooksHandler},
		{Command: "cutover", Handler: runCutover},
		{Command: "policy-lint", Handler: runPolicyLintHandler},
		{Command: "ci-sarif", Handler: runCISARIFHandler},
		{Command: "policy", Handler: runPolicyHandler},
		{Command: "code-intel", Handler: runCodeIntelHandler},
		{Command: "policy-tool", Handler: runPolicyTool},
		{Command: "policy-tool-group", Handler: runPolicyToolGroup},
		{Command: "policy-git", Handler: runPolicyGitHandler},
		{Command: "parent-install", Handler: runParentInstall},
		{Command: "parent-check", Handler: runParentCheck},
		{Command: "parent-lint", Handler: runParentLint},
		{Command: "mcp", Handler: runMCPHandler},
	}
}

func runAgentHookHandler(paths runtimePaths, rest []string) error {
	runAgentHook(paths, rest)

	return nil
}

func runAgentShellHandler(paths runtimePaths, rest []string) error {
	request, err := agentShellCommand(rest)
	if err != nil {
		return err
	}

	requirePolicyBundle(paths)
	installGitWrapperShim(paths)
	installLintToolShims(paths)

	command := request.Command
	if request.Rewrite {
		rewritten, err := rewriteAgentShellCommand(paths, command)
		if err != nil {
			return err
		}

		command = rewritten
	}

	paths.executor().execAgentShell(paths, command)

	return nil
}

func runAgentHooksHandler(paths runtimePaths, rest []string) error {
	runAgentHooksCommand(paths, rest)

	return nil
}

type agentShellRequest struct {
	Command string
	Rewrite bool
}

func agentShellCommand(args []string) (agentShellRequest, error) {
	rewrite := false
	if len(args) > 0 && args[0] == "--rewrite" {
		rewrite = true
		args = args[1:]
	}

	if len(args) < 2 || args[0] != "--" {
		return agentShellRequest{}, apperror.StaticError(
			"agent-shell requires [--rewrite] -- <command>",
		)
	}

	commandArgs := args[1:]
	if len(commandArgs) == 1 {
		command := strings.TrimSpace(commandArgs[0])
		if command == "" {
			return agentShellRequest{}, apperror.StaticError(
				"agent-shell command is empty",
			)
		}

		return agentShellRequest{Command: command, Rewrite: rewrite}, nil
	}

	return agentShellRequest{
		Command: shellCommand(commandArgs),
		Rewrite: rewrite,
	}, nil
}

func shellCommand(args []string) string {
	parts := make([]string, 0, len(args))
	for _, arg := range args {
		parts = append(parts, shellquote.Arg(arg))
	}

	return strings.Join(parts, " ")
}

func rewriteAgentShellCommand(paths runtimePaths, command string) (string, error) {
	bundleFile, err := os.Open(paths.PolicyBundle)
	if err != nil {
		return "", fmt.Errorf("open policy bundle: %w", err)
	}
	defer bundleFile.Close()

	bundle, err := policy.DecodeBundle(bundleFile)
	if err != nil {
		return "", fmt.Errorf("decode policy bundle: %w", err)
	}

	result, err := hooks.Run(bundle, hooks.Options{Event: hooks.Event{
		ProviderHint:  "claude",
		HookEventName: "PreToolUse",
		ToolName:      "Bash",
		Cwd:           paths.InvocationCWD,
		ToolInput: map[string]any{
			"command": command,
		},
	}})
	if err != nil {
		return "", fmt.Errorf("rewrite agent shell command: %w", err)
	}

	if result.Blocked() {
		fmt.Fprintln(os.Stderr, hooks.ProviderBlockMessage(result))
		requestRuntimeExit(agentShellBlockedExitCode)
	}

	if result.HookSpecificOutput == nil ||
		len(result.HookSpecificOutput.UpdatedInput) == 0 {
		return command, nil
	}

	rewritten, ok := result.HookSpecificOutput.UpdatedInput["command"].(string)
	if !ok || strings.TrimSpace(rewritten) == "" {
		return command, nil
	}

	return rewritten, nil
}

func runPolicyLintHandler(paths runtimePaths, rest []string) error {
	requirePolicyBundle(paths)
	runtimeExecLint(paths, append([]string{"--bundle", paths.PolicyBundle}, rest...)...)

	return nil
}

func runCISARIFHandler(paths runtimePaths, rest []string) error {
	requirePolicyBundle(paths)

	return runCISARIF(paths, rest)
}

func runPolicyHandler(paths runtimePaths, rest []string) error {
	runtimeExecTool(paths, "coding-ethos-policy", rest...)

	return nil
}

func runCodeIntelHandler(paths runtimePaths, rest []string) error {
	runtimeExecTool(
		paths,
		"coding-ethos-code-intel",
		codeIntelArgs(paths.Root, rest)...)

	return nil
}

func runPolicyGitHandler(paths runtimePaths, rest []string) error {
	requirePolicyBundle(paths)
	installGitWrapperShim(paths)
	runtimeExecTool(
		paths,
		"coding-ethos-git",
		append([]string{"--bundle", paths.PolicyBundle}, rest...)...)

	return nil
}

func runMCPHandler(paths runtimePaths, rest []string) error {
	runMCP(paths, rest)

	return nil
}

func runAgentHook(paths runtimePaths, rest []string) {
	bundlePath := hookPolicyBundlePath(paths)
	requireRuntimeFile(bundlePath, "compiled policy bundle")
	installGitWrapperShim(paths)
	installLintToolShims(paths)
	persistAgentEnvironment(paths)
	_ = os.Setenv("CODING_ETHOS_GIT_SHIM_DIR", paths.BinDir)
	paths.executor().execAgentHook(
		append([]string{"--bundle", bundlePath, "--json"}, rest...)...)
}

func runAgentHooksCommand(paths runtimePaths, rest []string) {
	installGitWrapperShim(paths)
	installLintToolShims(paths)
	_ = os.Setenv("CODE_ETHOS_CONSUMER_ROOT", rootFlagValue(rest, paths.Root))
	runtimeExecTool(
		paths,
		"coding-ethos-agent-hooks",
		withDefaultHookCommand(paths, rest)...)
}

func runPolicyTool(paths runtimePaths, rest []string) error {
	if len(rest) == 0 {
		return apperror.StaticError("policy-tool requires a tool name")
	}

	requirePolicyBundle(paths)
	runtimeExecLint(paths, policyToolLintArgs(paths, rest[0], rest[1:])...)

	return nil
}

func runMCP(paths runtimePaths, rest []string) {
	bundlePath := hookPolicyBundlePath(paths)
	requireRuntimeFile(bundlePath, "compiled policy bundle")
	runtimeExecTool(paths, "coding-ethos-mcp", append([]string{
		"--bundle", bundlePath,
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--invocation-cwd", paths.InvocationCWD,
	}, rest...)...)
}
