// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateGitHookBypass(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	if hasRawHookBypass(context.Command) {
		decision := policy.NewDecision("block", policyDef)
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
	if context.Argv[0] != "git" {
		return nil, nil
	}
	if !isCommitOrPush(context.Argv) {
		return nil, nil
	}
	if !hasHookBypass(context.Argv) {
		return nil, nil
	}

	decision := policy.NewDecision("block", policyDef)
	decision.Evidence = map[string]any{
		"argv": append([]string(nil), context.Argv...),
	}
	return []policy.Decision{decision}, nil
}

func isCommitOrPush(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	return argv[1] == "commit" || argv[1] == "push"
}

func hasHookBypass(argv []string) bool {
	for _, arg := range argv[2:] {
		if arg == "--no-verify" {
			return true
		}
		if arg == "-n" && isCommit(argv) {
			return true
		}
		if strings.HasPrefix(arg, "-") && strings.Contains(arg, "n") && isCommit(argv) {
			return true
		}
	}
	return false
}

func isCommit(argv []string) bool {
	return len(argv) >= 2 && argv[1] == "commit"
}

func hasRawHookBypass(command string) bool {
	if command == "" {
		return false
	}
	lower := strings.ToLower(command)
	return strings.Contains(command, "SKIP=") ||
		strings.Contains(command, "export SKIP=") ||
		strings.Contains(lower, "git_verify=false") ||
		strings.Contains(lower, "git_verify=0") ||
		strings.Contains(lower, "git_verify=no") ||
		strings.Contains(lower, "--no-verify") ||
		strings.Contains(lower, "git commit -n")
}
