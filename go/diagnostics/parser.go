// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package diagnostics normalizes external tool findings into one schema.

package diagnostics

import (
	"encoding/json"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type Parser func(output string) []Diagnostic

var fallbackPattern = regexp.MustCompile(
	`^(.+?):(\d+)(?::(\d+))?:\s*(?:(\w+):\s*)?(.+)$`,
)

const fallbackMatchParts = 6

const severityNotice = "notice"

var (
	actionlintPattern = regexp.MustCompile(
		`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\[([^\]]+)])?$`,
	)
	golangciPattern = regexp.MustCompile(
		`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\(([^)]+)\))?$`,
	)
	goTestFileLinePattern = regexp.MustCompile(
		`^\s+(.+\.go):(\d+):\s*(.*)$`,
	)
	goTestCoveragePattern = regexp.MustCompile(
		`coverage:\s+(\d+(?:\.\d+)?)%\s+of\s+statements`,
	)
	goCoverTotalPattern = regexp.MustCompile(
		`^total:\s+\(statements\)\s+(\d+(?:\.\d+)?)%$`,
	)
	goCoverFilePattern = regexp.MustCompile(
		`^(.+?\.go):(\d+):\s+\S+\s+(\d+(?:\.\d+)?)%$`,
	)
	hadolintPattern = regexp.MustCompile(
		`^(.+?):(\d+)\s+([A-Z]+\d+)\s+([^:]+):\s*(.+)$`,
	)
	yamllintPattern = regexp.MustCompile(`^(.+):(\d+):(\d+):\s*(.+)$`)
	dotenvPattern   = regexp.MustCompile(
		`^(.+?):\s*(?:(\d+)\s+)?([^:\s]+):\s*(.+)$`,
	)
	tombiHeaderPattern = regexp.MustCompile(
		`(?i)^\s*(error|warning|warn|info|hint)\s*:\s*(.+)$`,
	)
	tombiLocationPattern = regexp.MustCompile(
		`^\s*at\s+(.+?):(\d+):(\d+)\s*$`,
	)
	ruffCodePattern   = regexp.MustCompile(`^[A-Z]+[0-9]+$`)
	ruffFormatPattern = regexp.MustCompile(
		`^(Would reformat|would reformat|reformatted):\s+(.+)$`,
	)
	ruffFormatUnchanged = regexp.MustCompile(
		`^\d+\s+files?\s+would\s+be\s+left\s+unchanged$`,
	)
	shfmtDiffHeaderPattern = regexp.MustCompile(`^\+\+\+\s+(\S+)`)
	shfmtHunkPattern       = regexp.MustCompile(
		`^@@\s+-\d+(?:,\d+)?\s+\+(\d+)(?:,\d+)?\s+@@`,
	)
	pytestFileLinePattern = regexp.MustCompile(`^(.+?\.py):(\d+):\s*(.+)$`)
	pytestSummaryPattern  = regexp.MustCompile(
		`^(FAILED|ERROR)\s+(.+?\.py)(?:::([^\s]+))?\s+-\s+(.+)$`,
	)
	pytestCoverageTotalPattern = regexp.MustCompile(
		`^TOTAL\s+\d+\s+\d+\s+(\d+(?:\.\d+)?)%$`,
	)
	pytestCoverageFilePattern = regexp.MustCompile(
		`^(\S+\.(?:py|pyi))\s+\d+\s+\d+\s+(\d+(?:\.\d+)?)%$`,
	)
	vultureLinePattern = regexp.MustCompile(`^(.+?):(\d+):\s*(.+)$`)
)

const (
	actionlintTextMatchParts  = 6
	dotenvTextMatchParts      = 5
	goCoverFileMatchParts     = 4
	goCoverageMatchParts      = 2
	goTestFileLineMatchParts  = 4
	hadolintTextMatchParts    = 6
	shfmtDiffHeaderParts      = 2
	shfmtHunkMatchParts       = 2
	tombiHeaderMatchParts     = 3
	tombiLocationMatchParts   = 4
	ruffFormatMatchParts      = 3
	yamllintMatchParts        = 5
	pytestFileLineMatchParts  = 4
	pytestSummaryMatchParts   = 5
	pytestCoverageMatchParts  = 2
	pytestCoverageFileParts   = 3
	vultureLineMatchParts     = 4
	defaultComplexityLimit    = 15
	defaultMaintainabilityMin = 50
	severityError             = "error"
	severityWarning           = "warning"
)

func Parse(tool, stdout, stderr string) []Diagnostic {
	stdout = strings.TrimSpace(stdout)

	stderr = strings.TrimSpace(stderr)
	if stdout == "" && stderr == "" {
		return nil
	}

	parser, ok := parserForTool(tool)
	if ok {
		return parseStreams(parser, stdout, stderr)
	}

	return parseStreams(func(output string) []Diagnostic {
		return parseFallback(tool, output)
	}, stdout, stderr)
}

func parseStreams(parser Parser, stdout, stderr string) []Diagnostic {
	items := []Diagnostic{}
	if stdout != "" {
		items = append(items, parser(stdout)...)
	}

	if stderr != "" && stderr != stdout {
		items = append(items, parser(stderr)...)
	}

	return Dedupe(items)
}

func InferTool(command []string) string {
	for _, arg := range command {
		name := filepath.Base(strings.TrimSpace(arg))
		if _, ok := parserForTool(name); ok {
			return name
		}
	}

	if len(command) == 0 {
		return ""
	}

	return filepath.Base(command[0])
}

func HasParser(tool string) bool {
	_, ok := parserForTool(tool)

	return ok
}

func RegisteredParsers() []string {
	entries := parserEntries()

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}

	return names
}

func parserForTool(tool string) (Parser, bool) {
	normalized := normalizedToolName(tool)
	for _, entry := range parserEntries() {
		if entry.Name == normalized {
			return entry.Parser, true
		}
	}

	return nil, false
}

type parserEntry struct {
	Parser Parser
	Name   string
}

func parserEntries() []parserEntry {
	return []parserEntry{
		{Name: "actionlint", Parser: parseActionlint},
		{Name: "bandit", Parser: parseBandit},
		{Name: "dotenv-linter", Parser: parseDotenvLinter},
		{Name: "gofmt", Parser: parseGofmt},
		{Name: "gofmt-check", Parser: parseGofmt},
		{Name: "go-test", Parser: parseGoTest},
		{Name: "go-vet", Parser: parseGoVet},
		{Name: "golangci-lint", Parser: parseGolangciLint},
		{Name: "gemini", Parser: parseGemini},
		{Name: "gemini-check", Parser: parseGemini},
		{Name: "hadolint", Parser: parseHadolint},
		{Name: "mypy", Parser: parseMypy},
		{Name: "pylint", Parser: parsePylint},
		{Name: "pyright", Parser: parsePyright},
		{Name: "pytest", Parser: parsePytest},
		{Name: "pytest-gate", Parser: parsePytest},
		{Name: "radon-complexity", Parser: parseRadonComplexity},
		{Name: "radon-maintainability", Parser: parseRadonMaintainability},
		{Name: "ruff", Parser: parseRuff},
		{Name: "shfmt", Parser: parseShfmt},
		{Name: "shellcheck", Parser: parseShellcheck},
		{Name: "sqlfluff", Parser: parseSQLFluff},
		{Name: "tombi", Parser: parseTombi},
		{Name: "vulture", Parser: parseVulture},
		{Name: "yamllint", Parser: parseYamllint},
	}
}

func normalizedToolName(tool string) string {
	name := filepath.Base(strings.TrimSpace(tool))
	name = strings.TrimSuffix(name, ".exe")

	return name
}

type geminiResultPayload struct {
	Verdict    string                   `json:"verdict"`
	Violations []geminiViolationPayload `json:"violations"`
}

type geminiViolationPayload struct {
	Severity     string `json:"severity"`
	File         string `json:"file"`
	Message      string `json:"message"`
	EthosSection string `json:"ethos_section"`
	Line         int    `json:"line"`
}

func parseGemini(output string) []Diagnostic {
	cleaned := stripJSONCodeFence(output)

	violations, ok := parseGeminiViolations(cleaned)
	if !ok {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(violations))
	for _, violation := range violations {
		message := strings.TrimSpace(violation.Message)
		if message == "" {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "gemini",
			File:     strings.TrimSpace(violation.File),
			Line:     violation.Line,
			Severity: geminiSeverity(violation.Severity),
			Code:     strings.TrimSpace(violation.EthosSection),
			Message:  message,
		})
	}

	return diagnostics
}

func parseGeminiViolations(output string) ([]geminiViolationPayload, bool) {
	if strings.HasPrefix(strings.TrimSpace(output), "[") {
		var violations []geminiViolationPayload

		err := json.Unmarshal([]byte(output), &violations)

		return violations, err == nil
	}

	var result geminiResultPayload

	err := json.Unmarshal([]byte(output), &result)

	return result.Violations, err == nil
}

func stripJSONCodeFence(output string) string {
	trimmed := strings.TrimSpace(output)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```JSON")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")

	return strings.TrimSpace(trimmed)
}

func geminiSeverity(severity string) string {
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case "CRITICAL", "HIGH", "ERROR", "BLOCK":
		return severityError
	case "MEDIUM", "WARNING", "WARN":
		return severityWarning
	default:
		return severityNotice
	}
}

func parseFallback(tool, output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(output, "\n") {
		matches := fallbackPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != fallbackMatchParts {
			continue
		}

		lineNo, validLine := parseInt(matches[2])
		if !validLine {
			continue
		}

		column, _ := parseInt(matches[3])
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     tool,
			File:     matches[1],
			Line:     lineNo,
			Column:   column,
			Severity: firstNonEmpty(matches[4], severityError),
			Message:  strings.TrimSpace(matches[5]),
		})
	}

	return diagnostics
}

func extractJSONObject(output string) string {
	start := strings.Index(output, "{")

	end := strings.LastIndex(output, "}")
	if start < 0 || end < start {
		return strings.TrimSpace(output)
	}

	return strings.TrimSpace(output[start : end+1])
}

func parseInt(value string) (int, bool) {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))

	return parsed, err == nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}
