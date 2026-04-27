// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import "strings"

type parsedArgv struct {
	Argv      []string
	Operation string
}

func parseArgv(argv []string) parsedArgv {
	normalized := normalizeArgv(argv)
	return parsedArgv{
		Argv:      normalized,
		Operation: gitOperation(normalized),
	}
}

func normalizeArgv(argv []string) []string {
	normalized := append([]string(nil), argv...)
	if len(normalized) == 0 {
		return []string{"git"}
	}
	if normalized[0] == "git" {
		return normalized
	}
	return append([]string{"git"}, normalized...)
}

func gitOperation(argv []string) string {
	if len(argv) < 2 || argv[0] != "git" {
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
	case "-C", "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path", "--config-env":
		return true
	default:
		return false
	}
}
