// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentproxy

import (
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

// FileReadInvocation is the normalized shape of a conservative file-read
// command that can safely receive context-aware pagination.
type FileReadInvocation struct {
	Tool string
	Path string
}

// DetectFileReadInvocation recognizes static, single-target file read
// commands. The first supported surface is bare cat because its stdout maps
// directly to the file payload without formatter-specific prefixes.
func DetectFileReadInvocation(argv []string) (FileReadInvocation, bool) {
	if len(argv) == 0 {
		return FileReadInvocation{}, false
	}

	tool := filepath.Base(strings.TrimSpace(argv[0]))
	switch tool {
	case "cat":
		return detectCatFileReadInvocation(argv[1:])
	default:
		return FileReadInvocation{}, false
	}
}

// DetectShellFileReadInvocation recognizes simple shell commands whose stdout
// is a direct file payload suitable for proxy pagination.
func DetectShellFileReadInvocation(
	command shellparse.Command,
) (FileReadInvocation, bool) {
	if command.HasCommandSubstitution ||
		command.HasDynamicExpansion ||
		command.HasProcessSubstitution ||
		command.HasSubshell {
		return FileReadInvocation{}, false
	}

	return DetectFileReadInvocation(command.Argv)
}

func detectCatFileReadInvocation(args []string) (FileReadInvocation, bool) {
	targets := []string{}
	endOptions := false

	for _, arg := range args {
		switch {
		case endOptions:
			targets = append(targets, arg)
		case arg == "--":
			endOptions = true
		case arg == "-":
			return FileReadInvocation{}, false
		case strings.HasPrefix(arg, "-") && arg != "-":
			return FileReadInvocation{}, false
		default:
			targets = append(targets, arg)
		}
	}

	if len(targets) != 1 || targets[0] == "-" {
		return FileReadInvocation{}, false
	}

	return FileReadInvocation{Tool: "cat", Path: targets[0]}, true
}
