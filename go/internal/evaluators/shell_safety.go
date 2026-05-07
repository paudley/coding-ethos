// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func EvaluateShellMalformedCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if strings.TrimSpace(context.Command) == "" {
		return nil, nil
	}

	_, inlineErrAutoA := shellparse.Commands(context.Command)
	if inlineErrAutoA == nil {
		return nil, nil
	}

	return blockShellDecision(policyDef, context.Command), nil
}

func blockShellDecision(policyDef policy.Policy, command string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)

	decision.Evidence = map[string]any{"command": command}

	commands, inlineErrAutoB := shellparse.Commands(command)
	if inlineErrAutoB == nil {
		decision.Evidence["shell_commands"] = shellDecisionEvidence(commands)
	}

	return []policy.Decision{decision}
}

func shellDecisionEvidence(commands []shellparse.Command) []map[string]any {
	items := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		items = append(items, map[string]any{
			"argv":                     append([]string(nil), command.Argv...),
			"background":               command.Background,
			"column":                   command.Column,
			"has_command_substitution": command.HasCommandSubstitution,
			"has_dynamic_expansion":    command.HasDynamicExpansion,
			"has_heredoc":              command.HasHeredoc,
			"has_process_substitution": command.HasProcessSubstitution,
			"is_function_declaration":  command.IsFunctionDeclaration,
			"line":                     command.Line,
			"name":                     command.Name,
			"redirects":                append([]string(nil), command.Redirects...),
		})
	}

	return items
}
