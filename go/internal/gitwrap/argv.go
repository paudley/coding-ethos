// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import "strings"

type ParsedArgv struct {
	Operation string
	Argv      []string
}

func ParseArgv(argv []string) ParsedArgv {
	normalized := normalizeArgv(argv)

	return ParsedArgv{
		Argv:      normalized,
		Operation: gitOperation(normalized),
	}
}

func normalizeArgv(argv []string) []string {
	normalized := append([]string(nil), argv...)
	if len(normalized) == 0 {
		return []string{gitExecutableName}
	}

	if normalized[0] == gitExecutableName {
		return normalized
	}

	return append([]string{gitExecutableName}, normalized...)
}

func gitOperation(argv []string) string {
	if len(argv) < 2 || argv[0] != gitExecutableName {
		return ""
	}

	for idx := 1; idx < len(argv); idx++ {
		arg := argv[idx]
		if arg == "--" {
			return ""
		}

		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			return arg
		}

		if skipNextGitGlobalArg(arg) && idx+1 < len(argv) {
			idx++
		}
	}

	return ""
}

func skipNextGitGlobalArg(arg string) bool {
	switch arg {
	case "-C",
		"-c",
		"--git-dir",
		"--work-tree",
		"--namespace",
		"--exec-path",
		"--config-env":
		return true
	default:
		return false
	}
}
