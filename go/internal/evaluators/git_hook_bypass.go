// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateGitHookBypass(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if hasRawHookBypass(context.Command) {
		decision := policy.NewDecision(blockDecision, policyDef)

		decision.Evidence = map[string]any{
			"command": context.Command,
		}
		if len(context.Argv) > 0 {
			decision.Evidence["argv"] = append([]string(nil), context.Argv...)
		}

		return []policy.Decision{decision}, nil
	}

	if len(context.Argv) == 0 {
		return nil, nil
	}

	if !isGit(context.Argv) {
		return nil, nil
	}

	if !isCommitOrPush(context.Argv) {
		return nil, nil
	}

	if !hasHookBypass(context.Argv) {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"argv": append([]string(nil), context.Argv...),
	}

	return []policy.Decision{decision}, nil
}

func isCommitOrPush(argv []string) bool {
	operation := gitSubcommand(argv)

	return operation == "commit" || operation == "push"
}

func hasHookBypass(argv []string) bool {
	argv = stripLeadingAssignments(argv)
	for _, arg := range argv[2:] {
		if arg == "--no-verify" {
			return true
		}

		if arg == "-n" && isCommit(argv) {
			return true
		}

		if isShortCommitNoVerifyFlag(arg) && isCommit(argv) {
			return true
		}
	}

	return false
}

func isShortCommitNoVerifyFlag(arg string) bool {
	return strings.HasPrefix(arg, "-") &&
		!strings.HasPrefix(arg, "--") &&
		strings.Contains(arg, "n")
}

func isCommit(argv []string) bool {
	return gitSubcommand(argv) == "commit"
}

func hasRawHookBypass(command string) bool {
	if command == "" {
		return false
	}

	lower := strings.ToLower(command)
	gitOperation := strings.Contains(lower, "git commit") ||
		strings.Contains(lower, "git push")

	return strings.Contains(lower, "export skip=") ||
		strings.Contains(command, "SKIP=") && gitOperation ||
		strings.Contains(lower, "git_verify=false") ||
		strings.Contains(lower, "git_verify=0") ||
		strings.Contains(lower, "git_verify=no") ||
		strings.Contains(lower, "--no-verify") && gitOperation ||
		strings.Contains(lower, "git commit -n")
}
