// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"path/filepath"
	"slices"
	"strings"
)

const (
	agentHookCapabilitiesCommand = "capabilities"
	golangciLintAutofixTool      = "golangci-lint-autofix"
	golangciLintFormatTool       = "golangci-lint-format"
	golangciLintTool             = "golangci-lint"
	injectedRootArgCount         = 2
)

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

func codeIntelArgs(root, stateRoot string, args []string) []string {
	args = withoutFlags(args, "--state-root")
	if len(args) == 0 {
		return args
	}

	next := []string{args[0]}
	if !hasFlag(args, "--root") {
		next = append(next, "--root", root)
	}

	if !hasFlag(args, "--db") && !sameCleanPath(root, stateRoot) {
		next = append(
			next,
			"--db",
			filepath.Join(stateRoot, ".coding-ethos", "code-intel.duckdb"),
		)
	}

	next = append(next, args[1:]...)

	return next
}

func outputArgs(root string, args []string) []string {
	if len(args) == 0 || hasFlag(args, "--root") {
		return args
	}

	next := make([]string, 0, injectedRootArgCount+len(args))
	next = append(next, args[0], "--root", root)
	next = append(next, args[1:]...)

	return next
}

func webGuidanceArgs(root string, args []string) []string {
	if len(args) == 0 || hasFlag(args, "--root") {
		return args
	}

	next := make([]string, 0, injectedRootArgCount+len(args))
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

	captureTool := policyToolCaptureTool(toolName, toolArgs)
	lintArgs := make([]string, 0, managedLintBaseArgCount+len(toolArgs))
	lintArgs = append(lintArgs,
		"--bundle", paths.PolicyBundle,
		"--managed-capture-tool", captureTool,
		"--ethos-root", paths.EthosRoot,
		"--consumer-root", paths.Root,
		"--invocation-cwd", paths.InvocationCWD,
	)
	lintArgs = append(lintArgs, "--")
	lintArgs = append(lintArgs, toolArgs...)

	return lintArgs
}

func policyToolCaptureTool(toolName string, toolArgs []string) string {
	if toolName != golangciLintTool {
		return toolName
	}

	if golangciLintArgsRequestFormat(toolArgs) {
		return golangciLintFormatTool
	}

	if golangciLintArgsRequestFix(toolArgs) {
		return golangciLintAutofixTool
	}

	return toolName
}

func golangciLintArgsRequestFormat(args []string) bool {
	return firstGolangciLintCommand(args) == "fmt"
}

func golangciLintArgsRequestFix(args []string) bool {
	return slices.Contains(args, "--fix")
}

func firstGolangciLintCommand(args []string) string {
	for _, arg := range args {
		if strings.TrimSpace(arg) == "" || strings.HasPrefix(arg, "-") {
			continue
		}

		return arg
	}

	return ""
}

func withDefaultHookCommand(paths runtimePaths, args []string) []string {
	if hasFlag(args, "--hook-command") {
		return args
	}

	next := append([]string(nil), args...)
	next = append(next, "--hook-command", paths.RunBinary+" agent-hook")

	return next
}

func agentHooksArgs(paths runtimePaths, args []string) []string {
	if len(args) == 0 || args[0] != agentHookCapabilitiesCommand {
		return withDefaultHookCommand(paths, args)
	}

	next := append([]string(nil), args...)
	if !hasFlag(args, "--ethos-root") {
		next = append(next, "--ethos-root", paths.EthosRoot)
	}

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
	return flagValue(args, "--root", fallback)
}

func flagValue(args []string, name, fallback string) string {
	for index, arg := range args {
		if arg == name && index+1 < len(args) {
			return args[index+1]
		}

		if value, ok := strings.CutPrefix(arg, name+"="); ok {
			return value
		}
	}

	return fallback
}

func withoutFlags(args []string, names ...string) []string {
	filtered := make([]string, 0, len(args))

	for index := 0; index < len(args); index++ {
		arg := args[index]
		removed := false

		for _, name := range names {
			if arg == name {
				index++
				removed = true

				break
			}

			if strings.HasPrefix(arg, name+"=") {
				removed = true

				break
			}
		}

		if !removed {
			filtered = append(filtered, arg)
		}
	}

	return filtered
}

func (paths runtimePaths) withCommandRoots(args []string) runtimePaths {
	if len(args) == 0 {
		return paths
	}

	var (
		defaultStateRoot = paths.StateRoot
		repoRoot         string
	)

	switch args[0] {
	case "agent-hook", "mcp":
		repoRoot = flagValue(args[1:], "--repo-root", paths.Root)
	case "agent-hooks":
		repoRoot = flagValue(args[1:], "--repo-root", paths.Root)
		defaultStateRoot = flagValue(args[1:], "--root", paths.StateRoot)
	case "code-intel":
		repoRoot = flagValue(args[1:], "--root", paths.Root)
	case "runtime-policy":
		repoRoot = flagValue(args[1:], "--repo", paths.Root)
	default:
		return paths
	}

	rest := args[1:]

	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot != "" {
		paths.Root = filepath.Clean(repoRoot)
	}

	stateRoot := strings.TrimSpace(flagValue(rest, "--state-root", defaultStateRoot))
	if stateRoot != "" {
		paths.StateRoot = filepath.Clean(stateRoot)
	}

	return paths
}

func shouldLogRuntimeCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}

	if args[0] == "cutover" || args[0] == "lfs-hook" {
		return false
	}

	return len(args) < 2 ||
		args[0] != "agent-hooks" ||
		args[1] != agentHookCapabilitiesCommand
}
