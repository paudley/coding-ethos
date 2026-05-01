// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/lint"
)

const (
	FormatAuto  = "auto"
	FormatHuman = "human"
	FormatJSON  = "json"
	FormatSARIF = "sarif"
	FormatTOON  = "toon"
	FormatEnv   = "CODE_ETHOS_HOOK_OUTPUT_FORMAT"

	maxTOONFindingCellRunes = 320
)

func SelectedFormat() string {
	return SelectedFormatWithEnv(os.Getenv)
}

func SelectedFormatWithEnv(getenv func(string) string) string {
	format := strings.ToLower(strings.TrimSpace(getenv(FormatEnv)))
	switch format {
	case FormatHuman, FormatJSON, FormatTOON:
		return format
	case "", FormatAuto:
		if IsAgentEnvironment(getenv) {
			return FormatTOON
		}
		return FormatHuman
	default:
		return FormatHuman
	}
}

func IsAgentEnvironment(getenv func(string) string) bool {
	for _, marker := range AgentEnvironmentMarkers() {
		if strings.TrimSpace(getenv(marker)) != "" {
			return true
		}
	}

	return false
}

func AgentEnvironmentMarkers() []string {
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

func TOONCell(value string) string {
	cleaned := strings.TrimSpace(value)
	cleaned = strings.ReplaceAll(cleaned, "\\", "\\\\")
	cleaned = strings.ReplaceAll(cleaned, "\r\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, "\n", "\\n")
	cleaned = strings.ReplaceAll(cleaned, ",", "\\,")

	return cleaned
}

func TOONFindingCell(value string) string {
	cleaned := TOONCell(value)
	runes := []rune(cleaned)
	if len(runes) <= maxTOONFindingCellRunes {
		return cleaned
	}

	return string(runes[:maxTOONFindingCellRunes]) + "...[truncated]"
}

func FormatLintResult(result lint.Result, format string) (string, error) {
	switch format {
	case FormatJSON:
		return FormatLintResultJSON(result)
	case FormatSARIF:
		return FormatLintResultSARIF(result)
	case FormatTOON:
		return FormatLintResultTOON(result), nil
	default:
		return FormatLintResultHuman(result), nil
	}
}

func EncodeLintResult(writer io.Writer, result lint.Result, format string) error {
	output, err := FormatLintResult(result, format)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(writer, output)

	return err
}

func FormatLintResultJSON(result lint.Result) (string, error) {
	var builder strings.Builder
	encoder := json.NewEncoder(&builder)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		return "", err
	}

	return strings.TrimRight(builder.String(), "\n"), nil
}

func FormatLintResultTOON(result lint.Result) string {
	findings := lint.OutputDiagnostics(result)
	status := lint.ResultStatus(result)
	lines := []string{
		"format: toon",
		"tool: " + TOONCell(lint.ResultTool(result)),
		"status: " + TOONCell(status),
		"title: " + TOONCell(lintResultTitle(result)),
		"scope: " + TOONCell(result.Scope),
		fmt.Sprintf(
			"findings[%d]{tool,file,line,column,severity,code,policy_id,skill_id,message,advice,detail}:",
			len(findings),
		),
	}
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%d,%d,%s,%s,%s,%s,%s,%s,%s",
			TOONCell(finding.Tool),
			TOONCell(finding.File),
			finding.Line,
			finding.Column,
			TOONCell(finding.Severity),
			TOONCell(finding.Code),
			TOONCell(finding.PolicyID),
			TOONCell(finding.SkillID),
			TOONFindingCell(finding.Message),
			TOONFindingCell(finding.Advice),
			TOONFindingCell(finding.Detail),
		))
	}
	if len(result.SkillHints) > 0 {
		lines = append(
			lines,
			fmt.Sprintf(
				"advice[%d]{skill_id,message}:",
				len(result.SkillHints),
			),
		)
		for _, hint := range result.SkillHints {
			lines = append(lines, fmt.Sprintf(
				"  %s,%s",
				TOONCell(hint.SkillID),
				TOONFindingCell(compactSkillHintMessage(hint.Message)),
			))
		}
	}
	if result.Blocked() {
		lines = append(
			lines,
			"guidance[1]{message}:",
			"  Fix the reported diagnostics before continuing.",
		)
	}

	return strings.Join(lines, "\n")
}

func compactSkillHintMessage(message string) string {
	normalized := strings.Join(strings.Fields(message), " ")
	if normalized == "" {
		return ""
	}

	if sentence, _, ok := strings.Cut(normalized, ". "); ok && sentence != "" {
		return sentence + "."
	}

	return normalized
}

func FormatLintResultHuman(result lint.Result) string {
	findings := lint.OutputDiagnostics(result)
	lines := []string{
		"coding-ethos lint result: " + lint.ResultStatus(result),
		"tool: " + lint.ResultTool(result),
		"scope: " + result.Scope,
	}
	for _, finding := range findings {
		location := finding.File
		if finding.Line > 0 {
			location += ":" + strconv.Itoa(finding.Line)
		}
		lines = append(lines, fmt.Sprintf(
			"- %s [%s] %s",
			location,
			firstOutputLabel(finding.PolicyID, finding.Tool),
			finding.Message,
		))
		if finding.Advice != "" {
			lines = append(lines, "  advice: "+finding.Advice)
		}
	}
	if len(result.SkillHints) > 0 {
		lines = append(lines, "skill advice:")
		for _, hint := range result.SkillHints {
			lines = append(
				lines,
				"- "+hint.SkillID+": "+hint.Message+" Next: "+hint.Next,
			)
		}
	}
	if result.Blocked() {
		lines = append(lines, "Fix the reported diagnostics before continuing.")
	}

	return strings.Join(lines, "\n")
}

func lintResultTitle(result lint.Result) string {
	if result.Blocked() {
		return "LINT FAILED"
	}

	return "LINT RESULTS"
}

func firstOutputLabel(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
