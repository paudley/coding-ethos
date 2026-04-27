// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateGitDestructiveCommand(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
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

func EvaluateGitMergeStrategyShortcut(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "merge") {
		return nil, nil
	}
	for idx, arg := range argv {
		if arg == "-X" && idx+1 < len(argv) && isTheirsOrOurs(argv[idx+1]) {
			return blockGitDecision(policyDef, argv), nil
		}
		if strings.HasPrefix(arg, "-X") && isTheirsOrOurs(strings.TrimPrefix(arg, "-X")) {
			return blockGitDecision(policyDef, argv), nil
		}
	}
	return nil, nil
}

func EvaluateGitForcePushProtectedBranch(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
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

func EvaluateGitCheckoutProtectedBranch(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
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

func EvaluateGitDestructiveWorktree(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "worktree") || len(argv) < 3 {
		return nil, nil
	}
	switch argv[2] {
	case "prune":
		return blockGitDecision(policyDef, argv), nil
	case "remove", "move":
		if hasArg(argv, "--force") || hasShortFlag(argv, "f") {
			return blockGitDecision(policyDef, argv), nil
		}
	}
	return nil, nil
}

func EvaluateGitChangeDirFlag(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGit(argv) {
		return nil, nil
	}
	for _, arg := range argv[1:] {
		if arg == "-C" {
			return blockGitDecision(policyDef, argv), nil
		}
	}
	return nil, nil
}

func EvaluateGitStashBlocked(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	argv := context.Argv
	if isGitSubcommand(argv, "stash") {
		return blockGitDecision(policyDef, argv), nil
	}
	return nil, nil
}

func blockGitDecision(policyDef policy.Policy, argv []string) []policy.Decision {
	decision := policy.NewDecision("block", policyDef)
	decision.Evidence = map[string]any{
		"argv": append([]string(nil), argv...),
	}
	return []policy.Decision{decision}
}

func isGit(argv []string) bool {
	return len(argv) > 0 && argv[0] == "git"
}

func isGitSubcommand(argv []string, subcommand string) bool {
	return isGit(argv) && gitSubcommand(argv) == subcommand
}

func gitSubcommand(argv []string) string {
	if len(argv) < 2 {
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

func skipNextGitGlobalArg(arg string) bool {
	switch arg {
	case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--config-env":
		return true
	default:
		return false
	}
}

func hasArg(argv []string, target string) bool {
	for _, arg := range argv {
		if arg == target {
			return true
		}
	}
	return false
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
	return hasArg(argv, "--force") || hasArg(argv, "--force-with-lease") || hasShortFlag(argv, "f")
}

func hasProtectedBranchArg(argv []string) bool {
	for _, arg := range argv[2:] {
		if isProtectedBranchRef(arg) {
			return true
		}
	}
	return false
}

func isProtectedBranchRef(value string) bool {
	switch value {
	case "main", "master", "origin/main", "origin/master", "remotes/origin/main", "remotes/origin/master":
		return true
	default:
		return false
	}
}
