// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package toolprotocol defines explicit contracts between managed tools.
package toolprotocol

const (
	// ActionlintTool identifies the managed actionlint process.
	ActionlintTool = "actionlint"
	// ShellcheckTool identifies actionlint's ShellCheck dependency.
	ShellcheckTool = "shellcheck"

	// ActionlintShellcheckEnv marks ShellCheck requests made by managed actionlint.
	ActionlintShellcheckEnv = "CODE_ETHOS_ACTIONLINT_SHELLCHECK_PROTOCOL"
	// ActionlintShellcheckJSONStdinV1 identifies the raw JSON-on-stdin protocol.
	ActionlintShellcheckJSONStdinV1 = "json-stdin-v1"
)

// ActionlintShellcheckEnvironment returns the managed actionlint marker entry.
func ActionlintShellcheckEnvironment() string {
	return ActionlintShellcheckEnv + "=" + ActionlintShellcheckJSONStdinV1
}

// IsActionlintShellcheckJSONStdin reports whether a request matches the raw protocol.
func IsActionlintShellcheckJSONStdin(marker, tool string, args []string) bool {
	if marker != ActionlintShellcheckJSONStdinV1 ||
		tool != ShellcheckTool ||
		len(args) == 0 ||
		args[len(args)-1] != "-" {
		return false
	}

	for index, arg := range args {
		if (arg == "-f" || arg == "--format") &&
			index+1 < len(args) &&
			args[index+1] == "json" {
			return true
		}

		if arg == "-f=json" || arg == "--format=json" {
			return true
		}
	}

	return false
}
