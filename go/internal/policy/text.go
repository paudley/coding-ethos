// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import "strings"

func sentence(parts ...string) string {
	return strings.Join(parts, " ")
}
