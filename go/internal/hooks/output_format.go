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

func isAgentEnvironment(getenv func(string) string) bool {
	return hookoutput.IsAgentEnvironment(getenv)
}

func agentEnvironmentMarkers() []string {
	return hookoutput.AgentEnvironmentMarkers()
}

func toonCell(value string) string {
	return hookoutput.TOONCell(value)
}
