// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"strings"
)

const codeIntelInjectedRootArgCount = 2

func runnerArgs(argv []string) []string {
	if len(argv) != 1 {
		return argv[1:]
	}

	hookName := filepath.Base(argv[0])
	switch {
	case isGitHookName(hookName):
		return []string{"git-hook", hookName}
	case isLFSHookName(hookName):
		return []string{"lfs-hook", hookName}
	default:
		return nil
	}
}

func codeIntelArgs(root string, args []string) []string {
	if len(args) == 0 || hasFlag(args, "--root") {
		return args
	}

	next := make([]string, 0, codeIntelInjectedRootArgCount+len(args))
	next = append(next, args[0], "--root", root)
	next = append(next, args[1:]...)

	return next
}

func policyToolLintArgs(
	paths runtimePaths,
	toolName string,
	toolArgs []string,
) []string {
	const managedLintBaseArgCount = 11

	lintArgs := make([]string, 0, managedLintBaseArgCount+len(toolArgs))
	lintArgs = append(lintArgs,
		"--bundle", paths.PolicyBundle,
		"--managed-capture-tool", toolName,
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--invocation-cwd", paths.InvocationCWD,
	)
	lintArgs = append(lintArgs, "--")
	lintArgs = append(lintArgs, toolArgs...)

	return lintArgs
}

func withDefaultHookCommand(paths runtimePaths, args []string) []string {
	if hasFlag(args, "--hook-command") {
		return args
	}

	next := append([]string(nil), args...)
	next = append(next, "--hook-command", paths.RunBinary+" agent-hook")

	return next
}

func hasFlag(args []string, name string) bool {
	for _, arg := range args {
		if arg == name || strings.HasPrefix(arg, name+"=") {
			return true
		}
	}

	return false
}

func rootFlagValue(args []string, fallback string) string {
	for index, arg := range args {
		if arg == "--root" && index+1 < len(args) {
			return args[index+1]
		}

		if value, ok := strings.CutPrefix(arg, "--root="); ok {
			return value
		}
	}

	return fallback
}
