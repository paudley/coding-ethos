// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"strings"
)

const (
	outputFormatAuto  = "auto"
	outputFormatHuman = "human"
	outputFormatJSON  = "json"
	outputFormatTOON  = "toon"
	outputFormatEnv   = "CODE_ETHOS_HOOK_OUTPUT_FORMAT"
)

func selectedOutputFormat() string {
	format := strings.ToLower(strings.TrimSpace(os.Getenv(outputFormatEnv)))
	if format == "" {
		format = outputFormatAuto
	}

	switch format {
	case outputFormatAuto:
		if isAgentEnvironment(os.Getenv) {
			return outputFormatTOON
		}

		return outputFormatHuman
	case outputFormatHuman, outputFormatJSON, outputFormatTOON:
		return format
	default:
		return outputFormatHuman
	}
}

func isAgentEnvironment(getenv func(string) string) bool {
	for _, marker := range agentEnvironmentMarkers() {
		if strings.TrimSpace(getenv(marker)) != "" {
			return true
		}
	}

	return false
}

func agentEnvironmentMarkers() []string {
	return []string{
		"CODEX_THREAD_ID",
		"CODEX_CI",
		"CODEX_MANAGED_BY_NPM",
		"CLAUDECODE",
		"CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC",
		"GEMINI_CLI",
		"AIDER_MODEL",
		"CURSOR_TRACE_ID",
	}
}

func toonCell(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "\\", "\\\\")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, "\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, ",", "\\,")

	return cleaned
}
