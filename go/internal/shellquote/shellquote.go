// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package shellquote formats shell command fragments for human handoff text.
package shellquote

import "strings"

// Arg returns a single POSIX-shell-safe argument for display or handoff text.
func Arg(value string) string {
	if value == "" {
		return "''"
	}

	if !strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}();<>|&") {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

// Command joins arguments into a shell command string for display.
func Command(argv ...string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, Arg(arg))
	}

	return strings.Join(parts, " ")
}
