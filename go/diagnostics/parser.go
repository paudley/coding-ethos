// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package diagnostics normalizes external tool findings into one schema.
//
//nolint:tagliatelle // External tool schemas use their own JSON field names.
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

var (
	actionlintPattern = regexp.MustCompile(
		`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\[([^\]]+)])?$`,
	)
	golangciPattern = regexp.MustCompile(
		`^(.+?):(\d+):(\d+):\s*(.+?)(?:\s+\(([^)]+)\))?$`,
	)
	hadolintPattern = regexp.MustCompile(
		`^(.+?):(\d+)\s+([A-Z]+\d+)\s+([^:]+):\s*(.+)$`,
	)
	dotenvPattern = regexp.MustCompile(
		`^(.+?):(?:(\d+)\s+)?([^:\s]+):\s*(.+)$`,
	)
	tombiHeaderPattern = regexp.MustCompile(
		`(?i)^\s*(error|warning|warn|info|hint)\s*:\s*(.+)$`,
	)
	tombiLocationPattern = regexp.MustCompile(
		`^\s*at\s+(.+?):(\d+):(\d+)\s*$`,
	)
	ruffCodePattern     = regexp.MustCompile(`^[A-Z]+[0-9]+$`)
	ruffFormatPattern   = regexp.MustCompile(`^(Would reformat|would reformat|reformatted):\s+(.+)$`)
	ruffFormatUnchanged = regexp.MustCompile(`^\d+\s+files?\s+would\s+be\s+left\s+unchanged$`)
)

const (
	actionlintTextMatchParts = 6
	dotenvTextMatchParts     = 5
	hadolintTextMatchParts   = 6
	tombiHeaderMatchParts    = 3
	tombiLocationMatchParts  = 4
	yamllintParts            = 4
)

func Parse(tool string, stdout string, stderr string) []Diagnostic {
	output := strings.TrimSpace(firstNonEmpty(stdout, stderr))
	if output == "" {
		return nil
	}

	parser, ok := parserForTool(tool)
	if ok {
		return parser(output)
	}

	return parseFallback(tool, output)
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

func parserForTool(tool string) (Parser, bool) {
	switch normalizedToolName(tool) {
	case "ruff":
		return parseRuff, true
	case "pyright":
		return parsePyright, true
	case "mypy":
		return parseMypy, true
	case "pylint":
		return parsePylint, true
	case "golangci-lint":
		return parseGolangciLint, true
	case "hadolint":
		return parseHadolint, true
	case "actionlint":
		return parseActionlint, true
	case "shellcheck":
		return parseShellcheck, true
	case "yamllint":
		return parseYamllint, true
	case "bandit":
		return parseBandit, true
	case "sqlfluff":
		return parseSQLFluff, true
	case "tombi":
		return parseTombi, true
	case "dotenv-linter":
		return parseDotenvLinter, true
	default:
		return nil, false
	}
}

func normalizedToolName(tool string) string {
	name := filepath.Base(strings.TrimSpace(tool))
	name = strings.TrimSuffix(name, ".exe")

	return name
}

func parseRuff(output string) []Diagnostic {
	var items []struct {
		Filename string `json:"filename"`
		Code     string `json:"code"`
		Message  string `json:"message"`
		Location struct {
			Row    int `json:"row"`
			Column int `json:"column"`
		} `json:"location"`
	}

	err := json.Unmarshal([]byte(output), &items)
	if err != nil {
		return parseRuffText(output)
	}

	diagnostics := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "ruff",
			File:     item.Filename,
			Severity: "error",
			Code:     item.Code,
			Message:  item.Message,
			Line:     item.Location.Row,
			Column:   item.Location.Column,
		})
	}

	return diagnostics
}

func parseRuffText(output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		text := strings.TrimSpace(line)
		if text == "" || ruffFormatUnchanged.MatchString(text) {
			continue
		}
		if matches := ruffFormatPattern.FindStringSubmatch(text); len(matches) == 3 {
			diagnostics = append(diagnostics, Diagnostic{
				Tool:     "ruff",
				File:     matches[2],
				Severity: "error",
				Code:     "format",
				Message:  "File would be reformatted by ruff format.",
				Line:     1,
				Column:   1,
			})

			continue
		}

		matches := fallbackPattern.FindStringSubmatch(text)
		if len(matches) != fallbackMatchParts {
			continue
		}

		lineNo, validLine := parseInt(matches[2])
		if !validLine {
			continue
		}

		column, _ := parseInt(matches[3])
		code, message := splitRuffCodeMessage(matches[5])
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "ruff",
			File:     matches[1],
			Line:     lineNo,
			Column:   column,
			Severity: firstNonEmpty(matches[4], "error"),
			Code:     code,
			Message:  message,
		})
	}

	return diagnostics
}

func splitRuffCodeMessage(raw string) (string, string) {
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return "", ""
	}
	if ruffCodePattern.MatchString(fields[0]) {
		return fields[0], strings.TrimSpace(strings.TrimPrefix(raw, fields[0]))
	}

	return "", strings.TrimSpace(raw)
}

func parsePyright(output string) []Diagnostic {
	var payload struct {
		GeneralDiagnostics []struct {
			File     string `json:"file"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Rule     string `json:"rule"`
			Range    struct {
				Start struct {
					Line      int `json:"line"`
					Character int `json:"character"`
				} `json:"start"`
			} `json:"range"`
		} `json:"generalDiagnostics"`
	}

	err := json.Unmarshal([]byte(output), &payload)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(payload.GeneralDiagnostics))
	for _, item := range payload.GeneralDiagnostics {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "pyright",
			File:     item.File,
			Severity: item.Severity,
			Code:     item.Rule,
			Message:  item.Message,
			Line:     item.Range.Start.Line + 1,
			Column:   item.Range.Start.Character + 1,
		})
	}

	return diagnostics
}

func parseMypy(output string) []Diagnostic {
	items := parseMypyItems(output)
	if len(items) == 0 {
		return parseMypyText(output)
	}

	diagnostics := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "mypy",
			File:     firstNonEmpty(item.File, item.Path),
			Severity: item.Severity,
			Code:     item.Code,
			Message:  item.Message,
			Line:     item.Line,
			Column:   item.Column,
		})
	}

	return diagnostics
}

func parseMypyText(output string) []Diagnostic {
	items := parseFallback("mypy", output)
	for index, item := range items {
		message, code := splitTrailingBracketCode(item.Message)
		items[index].Message = message
		items[index].Code = code
	}

	return items
}

func splitTrailingBracketCode(message string) (string, string) {
	trimmed := strings.TrimSpace(message)
	start := strings.LastIndex(trimmed, "[")
	if start < 0 || !strings.HasSuffix(trimmed, "]") {
		return trimmed, ""
	}

	return strings.TrimSpace(trimmed[:start]), strings.TrimSuffix(trimmed[start+1:], "]")
}

type mypyItem struct {
	File     string `json:"file"`
	Path     string `json:"path"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Code     string `json:"code"`
	Line     int    `json:"line"`
	Column   int    `json:"column"`
}

func parseMypyItems(output string) []mypyItem {
	trimmedOutput := strings.TrimSpace(output)
	if strings.HasPrefix(trimmedOutput, "[") {
		var items []mypyItem
		if json.Unmarshal([]byte(trimmedOutput), &items) != nil {
			return nil
		}

		return items
	}

	items := []mypyItem{}

	for line := range strings.SplitSeq(trimmedOutput, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}

		var item mypyItem
		if json.Unmarshal([]byte(line), &item) != nil {
			return nil
		}

		items = append(items, item)
	}

	return items
}

func parsePylint(output string) []Diagnostic {
	if diagnostics := parsePylintJSON2(output); len(diagnostics) > 0 {
		return diagnostics
	}

	var items []struct {
		Path      string `json:"path"`
		Type      string `json:"type"`
		Symbol    string `json:"symbol"`
		MessageID string `json:"message-id"`
		Message   string `json:"message"`
		Line      int    `json:"line"`
		Column    int    `json:"column"`
	}

	err := json.Unmarshal([]byte(output), &items)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "pylint",
			File:     item.Path,
			Severity: item.Type,
			Code:     firstNonEmpty(item.Symbol, item.MessageID),
			Message:  item.Message,
			Line:     item.Line,
			Column:   item.Column + 1,
		})
	}

	return diagnostics
}

func parsePylintJSON2(output string) []Diagnostic {
	var payload struct {
		Messages []struct {
			Path      string `json:"path"`
			Type      string `json:"type"`
			Symbol    string `json:"symbol"`
			MessageID string `json:"messageId"`
			Message   string `json:"message"`
			Line      int    `json:"line"`
			Column    int    `json:"column"`
		} `json:"messages"`
	}

	err := json.Unmarshal([]byte(output), &payload)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(payload.Messages))
	for _, item := range payload.Messages {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "pylint",
			File:     item.Path,
			Severity: item.Type,
			Code:     firstNonEmpty(item.Symbol, item.MessageID),
			Message:  item.Message,
			Line:     item.Line,
			Column:   item.Column + 1,
		})
	}

	return diagnostics
}

func parseGolangciLint(output string) []Diagnostic {
	if diagnostics := parseGolangciLintJSON(output); len(diagnostics) > 0 {
		return diagnostics
	}

	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := golangciPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != actionlintTextMatchParts {
			continue
		}

		lineNo, validLine := parseInt(matches[2])
		column, validColumn := parseInt(matches[3])

		if !validLine || !validColumn {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "golangci-lint",
			File:     matches[1],
			Line:     lineNo,
			Column:   column,
			Severity: "error",
			Code:     matches[5],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return diagnostics
}

func parseGolangciLintJSON(output string) []Diagnostic {
	var payload struct {
		Issues []struct {
			FromLinter string `json:"FromLinter"`
			Text       string `json:"Text"`
			Severity   string `json:"Severity"`
			Pos        struct {
				Filename string `json:"Filename"`
				Line     int    `json:"Line"`
				Column   int    `json:"Column"`
			} `json:"Pos"`
		} `json:"Issues"`
	}

	err := json.Unmarshal([]byte(extractJSONObject(output)), &payload)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(payload.Issues))
	for _, issue := range payload.Issues {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "golangci-lint",
			File:     issue.Pos.Filename,
			Line:     issue.Pos.Line,
			Column:   issue.Pos.Column,
			Severity: firstNonEmpty(issue.Severity, "error"),
			Code:     issue.FromLinter,
			Message:  issue.Text,
		})
	}

	return diagnostics
}

func parseHadolint(output string) []Diagnostic {
	if diagnostics := parseHadolintJSON(output); len(diagnostics) > 0 {
		return diagnostics
	}

	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := hadolintPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != hadolintTextMatchParts {
			continue
		}

		lineNo, ok := parseInt(matches[2])
		if !ok {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "hadolint",
			File:     matches[1],
			Line:     lineNo,
			Severity: strings.TrimSpace(matches[4]),
			Code:     matches[3],
			Message:  strings.TrimSpace(matches[5]),
		})
	}

	return diagnostics
}

func parseHadolintJSON(output string) []Diagnostic {
	var items []struct {
		Code    string `json:"code"`
		File    string `json:"file"`
		Level   string `json:"level"`
		Message string `json:"message"`
		Line    int    `json:"line"`
		Column  int    `json:"column"`
	}

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &items)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(items))
	for _, item := range items {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "hadolint",
			File:     item.File,
			Line:     item.Line,
			Column:   item.Column,
			Severity: item.Level,
			Code:     item.Code,
			Message:  item.Message,
		})
	}

	return diagnostics
}

func parseActionlint(output string) []Diagnostic {
	if diagnostics := parseActionlintJSON(output); len(diagnostics) > 0 {
		return diagnostics
	}

	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := actionlintPattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != actionlintTextMatchParts {
			continue
		}

		lineNo, validLine := parseInt(matches[2])
		column, validColumn := parseInt(matches[3])

		if !validLine || !validColumn {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "actionlint",
			File:     matches[1],
			Line:     lineNo,
			Column:   column,
			Severity: "error",
			Code:     matches[5],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return diagnostics
}

func parseActionlintJSON(output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		var item struct {
			FilePath string `json:"filepath"`
			File     string `json:"file"`
			Path     string `json:"path"`
			Message  string `json:"message"`
			Kind     string `json:"kind"`
			Check    string `json:"check"`
			Line     int    `json:"line"`
			Column   int    `json:"column"`
		}

		err := json.Unmarshal([]byte(strings.TrimSpace(line)), &item)
		if err != nil {
			continue
		}

		file := firstNonEmpty(item.FilePath, item.File, item.Path)
		if file == "" && item.Message == "" {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "actionlint",
			File:     file,
			Line:     item.Line,
			Column:   item.Column,
			Severity: "error",
			Code:     firstNonEmpty(item.Kind, item.Check),
			Message:  item.Message,
		})
	}

	return diagnostics
}

func parseShellcheck(output string) []Diagnostic {
	var payload struct {
		Comments []struct {
			File    string `json:"file"`
			Level   string `json:"level"`
			Message string `json:"message"`
			Line    int    `json:"line"`
			Column  int    `json:"column"`
			Code    int    `json:"code"`
		} `json:"comments"`
	}

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(payload.Comments))
	for _, comment := range payload.Comments {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "shellcheck",
			File:     comment.File,
			Line:     comment.Line,
			Column:   comment.Column,
			Severity: comment.Level,
			Code:     "SC" + strconv.Itoa(comment.Code),
			Message:  comment.Message,
		})
	}

	return diagnostics
}

func parseYamllint(output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		parts := strings.SplitN(line, ":", yamllintParts)
		if len(parts) < yamllintParts {
			continue
		}

		lineNo, validLine := parseInt(parts[1])
		column, validColumn := parseInt(parts[2])

		if !validLine || !validColumn {
			continue
		}

		message := strings.TrimSpace(parts[3])
		severity := ""

		switch {
		case strings.Contains(message, "[error]"):
			severity = "error"
		case strings.Contains(message, "[warning]"):
			severity = "warning"
		}

		code := ""
		if start := strings.LastIndex(message, "("); start >= 0 &&
			strings.HasSuffix(message, ")") {
			code = strings.TrimSuffix(message[start+1:], ")")
			message = strings.TrimSpace(message[:start])
		}

		message = strings.TrimPrefix(message, "[error]")
		message = strings.TrimPrefix(message, "[warning]")
		message = strings.TrimSpace(message)

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "yamllint",
			File:     strings.TrimSpace(parts[0]),
			Line:     lineNo,
			Column:   column,
			Severity: severity,
			Code:     code,
			Message:  message,
		})
	}

	return diagnostics
}

func parseBandit(output string) []Diagnostic {
	var payload struct {
		Results []struct {
			Filename        string `json:"filename"`
			IssueSeverity   string `json:"issue_severity"`
			IssueConfidence string `json:"issue_confidence"`
			IssueText       string `json:"issue_text"`
			TestID          string `json:"test_id"`
			LineNumber      int    `json:"line_number"`
		} `json:"results"`
	}

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload)
	if err != nil {
		return parseFallback("bandit", output)
	}

	diagnostics := make([]Diagnostic, 0, len(payload.Results))
	for _, item := range payload.Results {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "bandit",
			File:     item.Filename,
			Line:     item.LineNumber,
			Severity: banditSeverity(item.IssueSeverity),
			Code:     item.TestID,
			Message:  item.IssueText,
		})
	}

	return diagnostics
}

func banditSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return "error"
	case "medium":
		return "warning"
	case "low":
		return "notice"
	default:
		return firstNonEmpty(strings.ToLower(strings.TrimSpace(value)), "warning")
	}
}

func parseSQLFluff(output string) []Diagnostic {
	var items []struct {
		Filepath   string `json:"filepath"`
		Violations []struct {
			Code         string `json:"code"`
			Description  string `json:"description"`
			LineNo       int    `json:"line_no"`
			LinePos      int    `json:"line_pos"`
			StartLineNo  int    `json:"start_line_no"`
			StartLinePos int    `json:"start_line_pos"`
			Name         string `json:"name"`
			Warning      bool   `json:"warning"`
		} `json:"violations"`
	}

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &items)
	if err != nil {
		return parseFallback("sqlfluff", output)
	}

	diagnostics := []Diagnostic{}
	for _, item := range items {
		for _, violation := range item.Violations {
			severity := "error"
			if violation.Warning {
				severity = "warning"
			}
			line := violation.LineNo
			if line == 0 {
				line = violation.StartLineNo
			}
			column := violation.LinePos
			if column == 0 {
				column = violation.StartLinePos
			}
			diagnostics = append(diagnostics, Diagnostic{
				Tool:     "sqlfluff",
				File:     item.Filepath,
				Line:     line,
				Column:   column,
				Severity: severity,
				Code:     violation.Code,
				Message:  firstNonEmpty(violation.Description, violation.Name),
			})
		}
	}

	return diagnostics
}

func parseTombi(output string) []Diagnostic {
	diagnostics := []Diagnostic{}
	lines := strings.Split(stripANSI(output), "\n")
	for index := 0; index < len(lines); index++ {
		header := tombiHeaderPattern.FindStringSubmatch(strings.TrimSpace(lines[index]))
		if len(header) != tombiHeaderMatchParts {
			continue
		}

		diagnostic := Diagnostic{
			Tool:     "tombi",
			Severity: normalizedTombiSeverity(header[1]),
			Message:  strings.TrimSpace(header[2]),
		}
		for lookahead := index + 1; lookahead < len(lines); lookahead++ {
			line := strings.TrimSpace(lines[lookahead])
			if line == "" {
				continue
			}
			if tombiHeaderPattern.MatchString(line) {
				break
			}
			location := tombiLocationPattern.FindStringSubmatch(line)
			if len(location) != tombiLocationMatchParts {
				continue
			}
			diagnostic.File = location[1]
			diagnostic.Line, _ = parseInt(location[2])
			diagnostic.Column, _ = parseInt(location[3])
			break
		}

		diagnostics = append(diagnostics, diagnostic)
	}

	return diagnostics
}

func normalizedTombiSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "error":
		return "error"
	case "warning", "warn":
		return "warning"
	default:
		return "notice"
	}
}

func stripANSI(value string) string {
	ansiPattern := regexp.MustCompile(`\x1b\[[0-9;]*m`)

	return ansiPattern.ReplaceAllString(value, "")
}

func parseDotenvLinter(output string) []Diagnostic {
	diagnostics := []Diagnostic{}
	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" ||
			strings.HasPrefix(trimmed, "Checking") ||
			strings.HasPrefix(trimmed, "Nothing to check") ||
			strings.HasPrefix(trimmed, "No problems found") {
			continue
		}

		matches := dotenvPattern.FindStringSubmatch(trimmed)
		if len(matches) != dotenvTextMatchParts {
			continue
		}

		lineNo, _ := parseInt(matches[2])
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "dotenv-linter",
			File:     matches[1],
			Line:     lineNo,
			Severity: "warning",
			Code:     matches[3],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return diagnostics
}

func parseFallback(tool string, output string) []Diagnostic {
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
			Severity: firstNonEmpty(matches[4], "error"),
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
