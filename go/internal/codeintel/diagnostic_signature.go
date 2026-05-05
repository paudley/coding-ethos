// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"regexp"
	"strings"
)

var diagnosticTokenPattern = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_.-]*`)

func diagnosticSignature(message string) []string {
	seen := map[string]bool{}
	tokens := []string{}
	for _, token := range diagnosticTokenPattern.FindAllString(message, -1) {
		clean := strings.ToLower(strings.Trim(token, "_.-"))
		if len(clean) < 3 || seen[clean] {
			continue
		}
		seen[clean] = true
		tokens = append(tokens, clean)
	}

	return tokens
}

func diagnosticSearchText(parts ...string) string {
	signature := diagnosticSignature(strings.Join(parts, "\n"))
	values := compactStrings(parts)
	if len(signature) > 0 {
		values = append(values, strings.Join(signature, " "))
	}

	return strings.Join(values, "\n")
}
