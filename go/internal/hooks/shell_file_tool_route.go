// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const shellFileToolPolicyID = "shell.file_tool_emulation"

func shellFileToolRouteFor(event Event) InspectionRoute {
	if event.HookEventName != eventPreToolUse || event.ToolName != toolBash {
		return InspectionRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return InspectionRoute{}
	}

	commands, err := shellparse.Commands(command)
	if err != nil {
		return InspectionRoute{}
	}

	if slices.ContainsFunc(commands, shellCommandEmulatesFileTool) {
		return InspectionRoute{
			Block:         true,
			BlockPolicyID: shellFileToolPolicyID,
			Reason: "Shell file access must use provider file tools instead of " +
				"Bash file-tool emulation.",
		}
	}

	return InspectionRoute{}
}

func shellCommandEmulatesFileTool(command shellparse.Command) bool {
	name := filepath.Base(command.Name)
	switch name {
	case "cat":
		return commandHasFileOperand(command.Argv[1:])
	case "sed":
		return sedReadsFileOperand(command.Argv[1:])
	case "awk":
		return awkReadsFileOperand(command.Argv[1:])
	case "tee":
		return commandHasFileOperand(command.Argv[1:])
	case "echo", "printf":
		return redirectsWriteFile(command.Redirects)
	default:
		return false
	}
}

func commandHasFileOperand(args []string) bool {
	for _, arg := range args {
		if shellOptionOrEmpty(arg) || arg == "-" {
			continue
		}

		return true
	}

	return false
}

func sedReadsFileOperand(args []string) bool {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if shellOptionOrEmpty(arg) {
			if sedOptionTakesValue(arg) && index+1 < len(args) {
				index++
			}

			continue
		}

		if index == 0 {
			continue
		}

		return arg != "-"
	}

	return false
}

func sedOptionTakesValue(arg string) bool {
	return arg == "-e" || arg == "--expression" ||
		arg == "-f" || arg == "--file"
}

func awkReadsFileOperand(args []string) bool {
	programSeen := false

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if shellOptionOrEmpty(arg) {
			if awkOptionTakesValue(arg) && index+1 < len(args) {
				index++
			}

			continue
		}

		if !programSeen {
			programSeen = true

			continue
		}

		return arg != "-"
	}

	return false
}

func awkOptionTakesValue(arg string) bool {
	return arg == "-f" || arg == "-F" || arg == "-v" ||
		arg == "--file" || arg == "--assign"
}

func shellOptionOrEmpty(arg string) bool {
	return arg == "" || strings.HasPrefix(arg, "-")
}

func redirectsWriteFile(redirects []string) bool {
	for _, redirect := range redirects {
		if strings.Contains(redirect, "<<") {
			continue
		}

		if strings.Contains(redirect, ">&") || strings.Contains(redirect, "<&") {
			continue
		}

		if strings.Contains(redirect, ">") {
			return true
		}
	}

	return false
}
