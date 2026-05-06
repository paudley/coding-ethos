// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import "blackcat.ca/coding-ethos/go/internal/hookoutput"

const (
	outputFormatAuto  = hookoutput.FormatAuto
	outputFormatHuman = hookoutput.FormatHuman
	outputFormatJSON  = hookoutput.FormatJSON
	outputFormatTOON  = hookoutput.FormatTOON
	outputFormatEnv   = hookoutput.FormatEnv
)

func selectedOutputFormat() string {
	return hookoutput.SelectedFormat()
}

func toonCell(value string) string {
	return hookoutput.TOONCell(value)
}
