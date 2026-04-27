// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "strings"

func shellFields(command string) []string {
	fields := []string{}
	var builder strings.Builder
	var quote rune
	escaped := false
	for _, char := range command {
		if escaped {
			builder.WriteRune(char)
			escaped = false
			continue
		}
		if char == '\\' && quote != '\'' {
			escaped = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
				continue
			}
			builder.WriteRune(char)
			continue
		}
		switch {
		case char == '\'' || char == '"':
			quote = char
		case char == ' ' || char == '\t' || char == '\n':
			if builder.Len() > 0 {
				fields = append(fields, builder.String())
				builder.Reset()
			}
		case char == ';':
			if builder.Len() > 0 {
				fields = append(fields, builder.String())
				builder.Reset()
			}
			fields = append(fields, string(char))
		default:
			builder.WriteRune(char)
		}
	}
	if builder.Len() > 0 {
		fields = append(fields, builder.String())
	}
	return fields
}
