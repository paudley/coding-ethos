// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

func runShellcheck(_ Config, paths []string) int {
	files := toolchainFiles("shellcheck", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runManagedPolicyTool(
		"shellcheck",
		managedPolicyToolArgsForFiles("shellcheck", files),
	)
}

func runShfmt(_ Config, paths []string) int {
	files := toolchainFiles("shfmt", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runManagedPolicyTool("shfmt", managedPolicyToolArgsForFiles("shfmt", files))
}

func runYamllint(_ Config, paths []string) int {
	files := toolchainFiles("yamllint", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runManagedPolicyTool(
		"yamllint",
		managedPolicyToolArgsForFiles("yamllint", files),
	)
}

func isShellFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == extShell || ext == extBash {
		return true
	}

	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return false
	}

	firstLine, _, _ := strings.Cut(string(data), "\n")

	return strings.HasPrefix(firstLine, "#!") &&
		(strings.Contains(firstLine, "bash") || strings.Contains(firstLine, "sh"))
}
