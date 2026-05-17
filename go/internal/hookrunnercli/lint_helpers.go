// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"fmt"
	"strconv"
	"strings"
)

func parseDiagnosticInt(value string) (int, bool) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, false
	}

	return number, true
}

func treeSitterLine(row uint) int {
	line, err := strconv.Atoi(strconv.FormatUint(uint64(row), 10))
	if err != nil {
		panic(fmt.Sprintf("tree-sitter row %d exceeds int range: %v", row, err))
	}

	return line + 1
}
