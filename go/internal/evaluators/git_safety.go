// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockDecision              = "block"
	gitSubcommandArgc          = 2
	gitWorktreeOperationArgc   = 3
	gitWorktreeSubcommandIndex = 2
)

func EvaluateGitDestructiveCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGit(argv) {
		return nil, nil
	}

	switch gitSubcommand(argv) {
	case "reset":
		if hasArg(argv, "--hard") {
			return blockGitDecision(policyDef, argv), nil
		}
	case "clean":
		if hasCleanForceDelete(argv) {
			return blockGitDecision(policyDef, argv), nil
		}
	case "checkout":
		if hasArg(argv, "--theirs") || hasArg(argv, "--ours") {
			return blockGitDecision(policyDef, argv), nil
		}
	case "restore":
		if hasArg(argv, "--") {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitMergeStrategyShortcut(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "merge") {
		return nil, nil
	}

	for idx, arg := range argv {
		if arg == "-X" && idx+1 < len(argv) && isTheirsOrOurs(argv[idx+1]) {
			return blockGitDecision(policyDef, argv), nil
		}

		if strings.HasPrefix(arg, "-X") &&
			isTheirsOrOurs(strings.TrimPrefix(arg, "-X")) {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitForcePushProtectedBranch(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "push") {
		return nil, nil
	}

	if !hasForcePush(argv) {
		return nil, nil
	}

	if hasProtectedBranchArg(argv) {
		return blockGitDecision(policyDef, argv), nil
	}

	return nil, nil
}

func EvaluateGitCheckoutProtectedBranch(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "checkout") && !isGitSubcommand(argv, "switch") {
		return nil, nil
	}

	for _, arg := range argv[2:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}

		if isProtectedBranchRef(arg) {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitDestructiveWorktree(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "worktree") || len(argv) < gitWorktreeOperationArgc {
		return nil, nil
	}

	switch argv[gitWorktreeSubcommandIndex] {
	case "prune":
		return blockGitDecision(policyDef, argv), nil
	case "remove", "move":
		if hasArg(argv, "--force") || hasShortFlag(argv, "f") {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitChangeDirFlag(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGit(argv) {
		return nil, nil
	}

	if slices.Contains(argv[1:], "-C") {
		return blockGitDecision(policyDef, argv), nil
	}

	return nil, nil
}

func EvaluateGitStashBlocked(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if isGitSubcommand(argv, "stash") {
		return blockGitDecision(policyDef, argv), nil
	}

	return nil, nil
}

func blockGitDecision(policyDef policy.Policy, argv []string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"argv": append([]string(nil), argv...),
	}

	return []policy.Decision{decision}
}

func isGit(argv []string) bool {
	normalized := stripLeadingAssignments(argv)

	return len(normalized) > 0 && normalized[0] == "git"
}

func isGitSubcommand(argv []string, subcommand string) bool {
	return isGit(argv) && gitSubcommand(argv) == subcommand
}

func gitSubcommand(argv []string) string {
	argv = stripLeadingAssignments(argv)
	if len(argv) < gitSubcommandArgc {
		return ""
	}

	for idx := 1; idx < len(argv); idx++ {
		arg := argv[idx]
		if arg == "--" {
			return ""
		}

		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			return arg
		}

		if skipNextGitGlobalArg(arg) && idx+1 < len(argv) {
			idx++
		}
	}

	return ""
}

func stripLeadingAssignments(argv []string) []string {
	for len(argv) > 0 && isShellAssignment(argv[0]) {
		argv = argv[1:]
	}

	return argv
}

func isShellAssignment(arg string) bool {
	name, value, ok := strings.Cut(arg, "=")
	if !ok || name == "" || value == "" {
		return false
	}

	for _, char := range name {
		if char != '_' && (char < 'A' || char > 'Z') &&
			(char < 'a' || char > 'z') &&
			(char < '0' || char > '9') {
			return false
		}
	}

	return true
}

func skipNextGitGlobalArg(arg string) bool {
	switch arg {
	case "-C",
		"-c",
		"--git-dir",
		"--work-tree",
		"--namespace",
		"--exec-path",
		"--config-env":
		return true
	default:
		return false
	}
}

func hasArg(argv []string, target string) bool {
	return slices.Contains(argv, target)
}

func hasShortFlag(argv []string, flag string) bool {
	for _, arg := range argv {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
			continue
		}

		if strings.Contains(strings.TrimPrefix(arg, "-"), flag) {
			return true
		}
	}

	return false
}

func hasCleanForceDelete(argv []string) bool {
	return hasArg(argv, "--force") && hasArg(argv, "-d") ||
		hasArg(argv, "-f") && hasArg(argv, "-d") ||
		hasShortFlag(argv, "f") && hasShortFlag(argv, "d")
}

func isTheirsOrOurs(value string) bool {
	return value == "theirs" || value == "ours"
}

func hasForcePush(argv []string) bool {
	return hasArg(argv, "--force") || hasArg(argv, "--force-with-lease") ||
		hasShortFlag(argv, "f")
}

func hasProtectedBranchArg(argv []string) bool {
	return slices.ContainsFunc(argv[2:], isProtectedBranchRef)
}

func isProtectedBranchRef(value string) bool {
	switch value {
	case "main",
		"master",
		"origin/main",
		"origin/master",
		"remotes/origin/main",
		"remotes/origin/master":
		return true
	default:
		return false
	}
}
