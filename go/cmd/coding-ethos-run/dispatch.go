// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

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
		{Command: "mcp", Handler: runMCPHandler},
	}
}

func runAgentHookHandler(paths runtimePaths, rest []string) error {
	runAgentHook(paths, rest)

	return nil
}

func runAgentHooksHandler(paths runtimePaths, rest []string) error {
	runAgentHooksCommand(paths, rest)

	return nil
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
	requirePolicyBundle(paths)
	installGitWrapperShim(paths)
	installLintToolShims(paths)
	persistAgentEnvironment(paths)
	_ = os.Setenv("CODING_ETHOS_GIT_SHIM_DIR", paths.BinDir)
	runtimeExecTool(
		paths,
		"coding-ethos-hook",
		append([]string{"--bundle", paths.PolicyBundle, "--json"}, rest...)...)
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
	requirePolicyBundle(paths)
	runtimeExecTool(paths, "coding-ethos-mcp", append([]string{
		"--bundle", paths.PolicyBundle,
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--invocation-cwd", paths.InvocationCWD,
	}, rest...)...)
}
