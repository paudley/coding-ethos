// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookoutput

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	FormatAuto  = "auto"
	FormatHuman = "human"
	FormatJSON  = "json"
	FormatTOON  = "toon"
	FormatEnv   = "CODE_ETHOS_HOOK_OUTPUT_FORMAT"
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

func FormatLintResult(result lint.Result, format string) (string, error) {
	switch format {
	case FormatJSON:
		return FormatLintResultJSON(result)
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
	findings := lintFindings(result)
	status := lintResultStatus(result)
	lines := []string{
		"format: toon",
		"tool: " + TOONCell(lintResultTool(result)),
		"status: " + TOONCell(status),
		"title: " + TOONCell(lintResultTitle(result)),
		"scope: " + TOONCell(result.Scope),
		fmt.Sprintf(
			"findings[%d]{tool,file,line,column,severity,code,policy_id,message,advice,detail}:",
			len(findings),
		),
	}
	for _, finding := range findings {
		lines = append(lines, fmt.Sprintf(
			"  %s,%s,%d,%d,%s,%s,%s,%s,%s,%s",
			TOONCell(finding.Tool),
			TOONCell(finding.File),
			finding.Line,
			finding.Column,
			TOONCell(finding.Severity),
			TOONCell(finding.Code),
			TOONCell(finding.PolicyID),
			TOONCell(finding.Message),
			TOONCell(finding.Advice),
			TOONCell(finding.Detail),
		))
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

func FormatLintResultHuman(result lint.Result) string {
	findings := lintFindings(result)
	lines := []string{
		"coding-ethos lint result: " + lintResultStatus(result),
		"tool: " + lintResultTool(result),
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
			firstNonEmpty(finding.PolicyID, finding.Tool),
			finding.Message,
		))
		if finding.Advice != "" {
			lines = append(lines, "  advice: "+finding.Advice)
		}
	}
	if result.Blocked() {
		lines = append(lines, "Fix the reported diagnostics before continuing.")
	}

	return strings.Join(lines, "\n")
}

func lintResultStatus(result lint.Result) string {
	if result.Blocked() {
		return "FAIL"
	}

	return "PASS"
}

func lintResultTitle(result lint.Result) string {
	if result.Blocked() {
		return "LINT FAILED"
	}

	return "LINT RESULTS"
}

func lintResultTool(result lint.Result) string {
	if tool, ok := strings.CutPrefix(result.Scope, "tool:"); ok && tool != "" {
		return tool
	}

	return "policy-lint"
}

func lintFindings(result lint.Result) []diagnostics.Diagnostic {
	if len(result.Diagnostics) > 0 {
		return diagnostics.Dedupe(result.Diagnostics)
	}

	decisions := result.Decisions
	if result.Blocked() {
		blocking := blockingLintDecisions(decisions)
		if len(blocking) > 0 {
			decisions = blocking
		}
	}

	findings := make([]diagnostics.Diagnostic, 0, len(decisions))
	for _, decision := range decisions {
		if len(decision.Diagnostics) > 0 {
			findings = append(findings, decision.Diagnostics...)
			continue
		}
		findings = append(findings, diagnostics.Diagnostic{
			Tool:     "policy",
			Severity: decision.Severity,
			PolicyID: decision.PolicyID,
			Message:  decision.Message,
			Advice:   decision.Suggestion,
		})
	}

	findings = diagnostics.Dedupe(findings)
	slices.SortStableFunc(findings, compareLintFindings)

	return findings
}

func blockingLintDecisions(decisions []policy.Decision) []policy.Decision {
	blocking := []policy.Decision{}
	for _, decision := range decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			blocking = append(blocking, decision)
		}
	}

	return blocking
}

func compareLintFindings(left diagnostics.Diagnostic, right diagnostics.Diagnostic) int {
	leftBlock := findingBlocks(left)
	rightBlock := findingBlocks(right)
	if leftBlock != rightBlock {
		if leftBlock {
			return -1
		}

		return 1
	}

	return strings.Compare(findingSortKey(left), findingSortKey(right))
}

func findingBlocks(finding diagnostics.Diagnostic) bool {
	return finding.Severity == "block" || finding.Severity == "error"
}

func findingSortKey(finding diagnostics.Diagnostic) string {
	return strings.Join([]string{
		finding.File,
		strconv.Itoa(finding.Line),
		finding.PolicyID,
		finding.Tool,
		finding.Code,
		finding.Message,
	}, "\x00")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}
