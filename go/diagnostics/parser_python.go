// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package diagnostics

import (
	"encoding/json"
	"fmt"
	"strings"
)

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
			Severity: severityError,
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

		if matches := ruffFormatPattern.FindStringSubmatch(
			text,
		); len(
			matches,
		) == ruffFormatMatchParts {
			diagnostics = append(diagnostics, Diagnostic{
				Tool:     "ruff",
				File:     matches[2],
				Severity: severityError,
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
			Severity: firstNonEmpty(matches[4], severityError),
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

//nolint:tagliatelle // Pyright JSON uses camel-case fields.
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

	return strings.TrimSpace(
			trimmed[:start],
		), strings.TrimSuffix(
			trimmed[start+1:],
			"]",
		)
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

//nolint:tagliatelle // Pylint JSON uses mixed historical field names.
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
			Path   string `json:"path"`
			Type   string `json:"type"`
			Symbol string `json:"symbol"`
			//nolint:tagliatelle // Pylint JSON2 uses messageId.
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

type radonComplexityItem struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Rank       string `json:"rank"`
	LineNo     int    `json:"lineno"`
	Complexity int    `json:"complexity"`
}

func parseRadonComplexity(output string) []Diagnostic {
	results, err := decodeRadonComplexity(output)
	if err != nil {
		return nil
	}

	diagnostics := []Diagnostic{}

	for file, items := range results {
		for _, item := range items {
			if item.Complexity <= defaultComplexityLimit {
				continue
			}

			diagnostics = append(diagnostics, Diagnostic{
				Metadata: map[string]any{
					"rank":       item.Rank,
					"type":       item.Type,
					"complexity": item.Complexity,
					"threshold":  defaultComplexityLimit,
				},
				Tool:     "radon-complexity",
				File:     file,
				Line:     item.LineNo,
				Severity: severityError,
				Code:     "cyclomatic-complexity",
				Message:  item.Name,
				Detail:   fmt.Sprintf("complexity: %d", item.Complexity),
			})
		}
	}

	return diagnostics
}

func decodeRadonComplexity(output string) (map[string][]radonComplexityItem, error) {
	results := map[string][]radonComplexityItem{}
	err := decodeRadonJSON(output, &results, "radon complexity")

	return results, err
}

func recognizesCleanRadonComplexityOutput(output string) bool {
	results, err := decodeRadonComplexity(output)
	if err != nil {
		return false
	}

	for _, items := range results {
		for _, item := range items {
			if item.Complexity > defaultComplexityLimit {
				return false
			}
		}
	}

	return true
}

type radonMaintainabilityItem struct {
	Rank string  `json:"rank"`
	MI   float64 `json:"mi"`
}

func parseRadonMaintainability(output string) []Diagnostic {
	results, err := decodeRadonMaintainability(output)
	if err != nil {
		return nil
	}

	diagnostics := []Diagnostic{}

	for file, item := range results {
		if item.MI >= defaultMaintainabilityMin {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Metadata: map[string]any{
				"rank":      item.Rank,
				"mi":        item.MI,
				"threshold": defaultMaintainabilityMin,
			},
			Tool:     "radon-maintainability",
			File:     file,
			Severity: severityWarning,
			Code:     "maintainability-index",
			Message:  "Maintainability index below configured threshold.",
			Detail:   fmt.Sprintf("MI: %.2f", item.MI),
		})
	}

	return diagnostics
}

func decodeRadonMaintainability(
	output string,
) (map[string]radonMaintainabilityItem, error) {
	results := map[string]radonMaintainabilityItem{}
	err := decodeRadonJSON(output, &results, "radon maintainability")

	return results, err
}

func recognizesCleanRadonMaintainabilityOutput(output string) bool {
	results, err := decodeRadonMaintainability(output)
	if err != nil {
		return false
	}

	for _, item := range results {
		if item.MI < defaultMaintainabilityMin {
			return false
		}
	}

	return true
}

func decodeRadonJSON(output string, target any, label string) error {
	err := json.Unmarshal([]byte(strings.TrimSpace(output)), target)
	if err != nil {
		return fmt.Errorf("decode %s JSON: %w", label, err)
	}

	return nil
}

func parseVulture(output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := vultureLinePattern.FindStringSubmatch(strings.TrimSpace(line))
		if len(matches) != vultureLineMatchParts ||
			strings.HasPrefix(matches[1], "=") {
			continue
		}

		lineNo, ok := parseInt(matches[2])
		if !ok {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "vulture",
			File:     matches[1],
			Line:     lineNo,
			Severity: severityWarning,
			Code:     "unused-code",
			Message:  strings.TrimSpace(matches[3]),
		})
	}

	return diagnostics
}
