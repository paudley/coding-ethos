// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateShellDangerousCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "rm -rf") || strings.Contains(lower, "rm -fr"):
		return blockShellDecision(policyDef, command), nil
	case strings.Contains(lower, "curl ") && pipesToShell(lower):
		return blockShellDecision(policyDef, command), nil
	case strings.Contains(lower, "wget ") && pipesToShell(lower):
		return blockShellDecision(policyDef, command), nil
	case strings.Contains(lower, "chmod 777"):
		return blockShellDecision(policyDef, command), nil
	default:
		return nil, nil
	}
}

func EvaluateShellBackgroundGit(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	lower := strings.ToLower(command)
	if !strings.Contains(lower, "git commit") && !strings.Contains(lower, "git push") {
		return nil, nil
	}

	if strings.Contains(lower, "timeout ") || strings.Contains(lower, " &") ||
		strings.HasSuffix(strings.TrimSpace(lower), "&") {
		return blockShellDecision(policyDef, command), nil
	}

	return nil, nil
}

func pipesToShell(command string) bool {
	return strings.Contains(command, "| sh") ||
		strings.Contains(command, "| bash") ||
		strings.Contains(command, "| /bin/sh") ||
		strings.Contains(command, "| /bin/bash")
}

func blockShellDecision(policyDef policy.Policy, command string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{"command": command}

	return []policy.Decision{decision}
}
