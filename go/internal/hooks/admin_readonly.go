// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func defaultAdminApprovedForCWD(cwd string) bool {
	return gitwrap.VerifyAdminApproved(cwd) == nil
}

func readOnlyInspectionEvent(event Event, adminApproved bool) bool {
	if event.HookEventName != eventPreToolUse ||
		event.ToolName != toolBash ||
		!adminApproved {
		return false
	}

	provider := event.Provider()
	if provider != "" && provider != providerCodex {
		return false
	}

	return ReadOnlyInspectionCommand(event.Command())
}

// ReadOnlyInspectionCommand reports whether commandText contains only read-only
// inspection steps allowed for admin-approved hook introspection.
func ReadOnlyInspectionCommand(commandText string) bool {
	commands, err := shellparse.Commands(commandText)
	if err != nil || len(commands) == 0 {
		return false
	}

	for _, command := range commands {
		if !isReadOnlyInspectionStep(command) {
			return false
		}
	}

	return true
}

func isReadOnlyInspectionStep(command shellparse.Command) bool {
	if command.Name == "" ||
		command.Background ||
		command.HasCommandSubstitution ||
		command.HasDynamicExpansion ||
		command.HasHeredoc ||
		command.HasProcessSubstitution ||
		command.HasSubshell ||
		command.IsFunctionDeclaration ||
		!readOnlyInspectionName(command.Name) ||
		!readOnlyInspectionRedirects(command.Redirects) {
		return false
	}

	return readOnlyInspectionArgs(command)
}

func readOnlyInspectionName(name string) bool {
	return slices.Contains([]string{
		"cut",
		"file",
		"find",
		"grep",
		"head",
		"jq",
		tokenGit,
		"ls",
		"nl",
		"pwd",
		"rg",
		"sort",
		"stat",
		"tail",
		"tr",
		"uniq",
		"wc",
		"yq",
	}, name)
}

func readOnlyInspectionRedirects(redirects []string) bool {
	for _, redirect := range redirects {
		trimmed := strings.TrimSpace(redirect)
		if strings.HasPrefix(trimmed, "<") {
			continue
		}

		if trimmed == "2>&1" {
			continue
		}

		if strings.HasSuffix(trimmed, "/dev/null") {
			continue
		}

		return false
	}

	return true
}

func readOnlyInspectionArgs(command shellparse.Command) bool {
	if command.Name == tokenGit {
		return readOnlyGitInspectionArgs(command.Argv[1:])
	}

	for _, arg := range command.Argv[1:] {
		if mutatingInspectionArg(command.Name, arg) {
			return false
		}
	}

	return true
}

func readOnlyGitInspectionArgs(args []string) bool {
	if len(args) == 0 {
		return false
	}

	index := 0
	for index < len(args) && strings.HasPrefix(args[index], "-") {
		switch args[index] {
		case "-C", "-c":
			index += 2
		default:
			return false
		}
	}

	if index >= len(args) {
		return false
	}

	return slices.Contains([]string{
		"branch",
		"diff",
		"ls-files",
		"log",
		"rev-parse",
		"show",
		"status",
	}, args[index])
}

func mutatingInspectionArg(name, arg string) bool {
	switch name {
	case "sed":
		return strings.HasPrefix(arg, "-i") ||
			strings.HasPrefix(arg, "--in-place")
	case "find":
		return arg == "-delete" || arg == "-exec" || arg == "-execdir" ||
			arg == "-ok" || arg == "-okdir"
	default:
		return false
	}
}
