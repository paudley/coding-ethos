// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const malformedShellPolicyID = "shell.malformed_command"

func malformedShellRouteFor(event Event) gitWrapperRoute {
	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
		return gitWrapperRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return gitWrapperRoute{}
	}

	if _, err := shellparse.Commands(command); err == nil {
		return gitWrapperRoute{}
	}

	return gitWrapperRoute{
		BlockPolicyID: malformedShellPolicyID,
		Reason:        "Malformed shell command text is ambiguous and forbidden.",
		Block:         true,
	}
}
