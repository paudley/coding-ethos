// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "strings"

func shellFields(command string) []string {
	fields := []string{}

	var (
		builder strings.Builder
		quote   rune
	)

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

		switch char {
		case '\'', '"':
			quote = char
		case ' ', '\t', '\n':
			fields = appendShellField(fields, &builder)
		case ';':
			fields = appendShellField(fields, &builder)
			fields = append(fields, string(char))
		default:
			builder.WriteRune(char)
		}
	}

	return appendShellField(fields, &builder)
}

func appendShellField(fields []string, builder *strings.Builder) []string {
	if builder.Len() == 0 {
		return fields
	}

	fields = append(fields, builder.String())
	builder.Reset()

	return fields
}
