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
	if redirectsReadFile(command.Redirects) {
		return true
	}

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
	operandsOnly := false

	for _, arg := range args {
		if arg == "--" {
			operandsOnly = true

			continue
		}

		if (!operandsOnly && shellOptionOrEmpty(arg)) || arg == "-" {
			continue
		}

		return true
	}

	return false
}

func sedReadsFileOperand(args []string) bool {
	scriptSeen := false
	operandsOnly := false

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			operandsOnly = true

			continue
		}

		if !operandsOnly && sedFileOption(arg) {
			return true
		}

		if !operandsOnly && sedExpressionOptionFused(arg) {
			scriptSeen = true

			continue
		}

		if !operandsOnly && shellOptionOrEmpty(arg) {
			next, seen, readsFile := consumeSedOptionValue(args, index)
			index = next
			scriptSeen = scriptSeen || seen

			if readsFile {
				return true
			}

			continue
		}

		if !scriptSeen {
			scriptSeen = true

			continue
		}

		return arg != "-"
	}

	return false
}

func consumeSedOptionValue(args []string, index int) (int, bool, bool) {
	arg := args[index]
	if sedFileOption(arg) && index+1 < len(args) {
		return index + 1, false, args[index+1] != "-"
	}

	if sedExpressionOption(arg) && index+1 < len(args) {
		return index + 1, true, false
	}

	return index, false, false
}

func sedExpressionOption(arg string) bool {
	return arg == "-e" || arg == "--expression" ||
		sedExpressionOptionFused(arg)
}

func sedExpressionOptionFused(arg string) bool {
	return (strings.HasPrefix(arg, "-e") && arg != "-e") ||
		strings.HasPrefix(arg, "--expression=")
}

func sedFileOption(arg string) bool {
	return arg == "-f" || arg == "--file" ||
		strings.HasPrefix(arg, "-f") ||
		strings.HasPrefix(arg, "--file=")
}

func awkReadsFileOperand(args []string) bool {
	programSeen := false
	operandsOnly := false

	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			operandsOnly = true

			continue
		}

		if !operandsOnly && awkFileOption(arg) {
			return true
		}

		if !operandsOnly && awkOptionTakesValue(arg) && awkOptionValueFused(arg) {
			if awkProgramOption(arg) {
				programSeen = true
			}

			continue
		}

		if !operandsOnly && shellOptionOrEmpty(arg) {
			index, programSeen = consumeAwkOptionValue(args, index, programSeen)

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

func consumeAwkOptionValue(args []string, index int, programSeen bool) (int, bool) {
	arg := args[index]
	if !awkOptionTakesValue(arg) || index+1 >= len(args) {
		return index, programSeen
	}

	if awkProgramOption(arg) {
		programSeen = true
	}

	return index + 1, programSeen
}

func awkFileOption(arg string) bool {
	return arg == "-f" || arg == "--file" ||
		strings.HasPrefix(arg, "-f") ||
		strings.HasPrefix(arg, "--file=")
}

func awkProgramOption(arg string) bool {
	return arg == "-e" || arg == "--source" ||
		strings.HasPrefix(arg, "-e") ||
		strings.HasPrefix(arg, "--source=")
}

func awkOptionTakesValue(arg string) bool {
	return arg == "-F" || arg == "-v" ||
		arg == "--assign" ||
		strings.HasPrefix(arg, "-F") ||
		strings.HasPrefix(arg, "-v") ||
		strings.HasPrefix(arg, "--assign=") ||
		awkProgramOption(arg)
}

func awkOptionValueFused(arg string) bool {
	return (strings.HasPrefix(arg, "-F") && arg != "-F") ||
		(strings.HasPrefix(arg, "-v") && arg != "-v") ||
		strings.HasPrefix(arg, "--assign=") ||
		(strings.HasPrefix(arg, "-e") && arg != "-e") ||
		strings.HasPrefix(arg, "--source=")
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

func redirectsReadFile(redirects []string) bool {
	for _, redirect := range redirects {
		if strings.Contains(redirect, "<<") {
			continue
		}

		if strings.Contains(redirect, "<&") || strings.Contains(redirect, ">&") {
			continue
		}

		if strings.Contains(redirect, "<") {
			return true
		}
	}

	return false
}
