// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"os"
	"path/filepath"
)

func run(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
		runtimeExecPath(paths.GitHookRunner)
	}

	command := args[0]
	rest := args[1:]

	switch command {
	case "agent-hook":
		runAgentHook(paths, rest)
	case "git-hook":
		return runGitHook(paths, rest)
	case "lfs-hook":
		return runLFSHook(paths, rest)
	case "agent-hooks":
		runAgentHooksCommand(paths, rest)
	case "cutover":
		return runCutover(paths, rest)
	case "policy-lint":
		requirePolicyBundle(paths)
		runtimeExecTool(
			paths,
			"coding-ethos-lint",
			append([]string{"--bundle", paths.PolicyBundle}, rest...)...)
	case "ci-sarif":
		requirePolicyBundle(paths)

		return runCISARIF(paths, rest)
	case "policy":
		runtimeExecTool(paths, "coding-ethos-policy", rest...)
	case "code-intel":
		runtimeExecTool(paths, "coding-ethos-code-intel", codeIntelArgs(paths.Root, rest)...)
	case "policy-tool":
		return runPolicyTool(paths, rest)
	case "policy-git":
		requirePolicyBundle(paths)
		installGitWrapperShim(paths)
		runtimeExecTool(
			paths,
			"coding-ethos-git",
			append([]string{"--bundle", paths.PolicyBundle}, rest...)...)
	case "mcp":
		runMCP(paths, rest)
	default:
		requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
		runtimeExecPath(paths.GitHookRunner, args...)
	}

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
		return errors.New("policy-tool requires a tool name")
	}

	requirePolicyBundle(paths)
	runtimeExecTool(
		paths,
		"coding-ethos-lint",
		policyToolLintArgs(paths, rest[0], rest[1:])...)

	return nil
}

func runMCP(paths runtimePaths, rest []string) {
	requirePolicyBundle(paths)
	requireRuntimeBinary(
		filepath.Join(paths.BinDir, "coding-ethos-lint"),
		"coding-ethos-lint",
	)
	runtimeExecTool(paths, "coding-ethos-mcp", append([]string{
		"--bundle", paths.PolicyBundle,
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--invocation-cwd", paths.InvocationCWD,
		"--lint-binary", filepath.Join(paths.BinDir, "coding-ethos-lint"),
	}, rest...)...)
}
