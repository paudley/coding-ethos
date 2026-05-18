// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package diagnostics

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const eslintErrorSeverity = 2

//nolint:tagliatelle // ESLint JSON uses camel-case fields.
type eslintFileDiagnostic struct {
	FilePath string                    `json:"filePath"`
	Messages []eslintMessageDiagnostic `json:"messages"`
}

//nolint:govet,tagliatelle // ESLint JSON uses camel-case nullable fields.
type eslintMessageDiagnostic struct {
	Message   string  `json:"message"`
	RuleID    *string `json:"ruleId"`
	Severity  int     `json:"severity"`
	Line      int     `json:"line"`
	Column    int     `json:"column"`
	EndLine   int     `json:"endLine"`
	EndColumn int     `json:"endColumn"`
	Fatal     bool    `json:"fatal"`
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

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterPayload struct {
	Reports []kubeLinterReport `json:"Reports"`
}

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterReport struct {
	Diagnostic  kubeLinterDiagnostic `json:"Diagnostic"`
	Object      kubeLinterObject     `json:"Object"`
	Check       string               `json:"Check"`
	Remediation string               `json:"Remediation"`
}

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterDiagnostic struct {
	Severity string `json:"Severity"`
	Message  string `json:"Message"`
}

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterObject struct {
	Metadata  kubeLinterMetadata  `json:"Metadata"`
	K8sObject kubeLinterK8sObject `json:"K8sObject"`
}

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterMetadata struct {
	FilePath string `json:"FilePath"`
}

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterK8sObject struct {
	GroupVersionKind kubeLinterGroupVersionKind `json:"GroupVersionKind"`
	Namespace        string                     `json:"Namespace"`
	Name             string                     `json:"Name"`
}

//nolint:tagliatelle // kube-linter JSON uses exported Go-style field names.
type kubeLinterGroupVersionKind struct {
	Group   string `json:"Group"`
	Version string `json:"Version"`
	Kind    string `json:"Kind"`
}

func parseKubeLinter(output string) []Diagnostic {
	var payload kubeLinterPayload

	err := json.NewDecoder(strings.NewReader(strings.TrimSpace(output))).Decode(&payload)
	if err != nil {
		return nil
	}

	diagnostics := make([]Diagnostic, 0, len(payload.Reports))
	for _, report := range payload.Reports {
		message := strings.TrimSpace(report.Diagnostic.Message)
		code := strings.TrimSpace(report.Check)

		if message == "" && code == "" {
			continue
		}

		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "kube-linter",
			File:     strings.TrimSpace(report.Object.Metadata.FilePath),
			Severity: kubeLinterSeverity(report.Diagnostic.Severity),
			Code:     code,
			Message:  message,
			Advice:   strings.TrimSpace(report.Remediation),
			Metadata: kubeLinterMetadataMap(report.Object.K8sObject),
		})
	}

	return diagnostics
}

func kubeLinterSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warning", "warn":
		return severityWarning
	case severityNotice, "info", "note":
		return severityNotice
	default:
		return severityError
	}
}

func kubeLinterMetadataMap(object kubeLinterK8sObject) map[string]any {
	metadata := map[string]any{}
	if object.Name != "" {
		metadata["name"] = object.Name
	}

	if object.Namespace != "" {
		metadata["namespace"] = object.Namespace
	}

	if object.GroupVersionKind.Group != "" {
		metadata["group"] = object.GroupVersionKind.Group
	}

	if object.GroupVersionKind.Version != "" {
		metadata["version"] = object.GroupVersionKind.Version
	}

	if object.GroupVersionKind.Kind != "" {
		metadata["kind"] = object.GroupVersionKind.Kind
	}

	if len(metadata) == 0 {
		return nil
	}

	return metadata
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
			Severity: severityError,
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
			Severity: severityError,
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

func parseShfmt(output string) []Diagnostic {
	byFile := map[string]int{}
	order := []string{}
	currentFile := ""

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if matches := shfmtDiffHeaderPattern.FindStringSubmatch(trimmed); len(
			matches,
		) == shfmtDiffHeaderParts {
			file := normalizeShfmtDiffPath(matches[1])

			currentFile = file
			if file == "" {
				continue
			}

			if _, exists := byFile[file]; !exists {
				byFile[file] = 1
				order = append(order, file)
			}

			continue
		}

		if currentFile == "" {
			continue
		}

		hunk := shfmtHunkPattern.FindStringSubmatch(trimmed)
		if len(hunk) != shfmtHunkMatchParts {
			continue
		}

		lineNo, validLine := parseInt(hunk[1])
		if !validLine || lineNo <= 0 {
			continue
		}

		byFile[currentFile] = lineNo
	}

	diagnostics := make([]Diagnostic, 0, len(order))
	for _, file := range order {
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     "shfmt",
			File:     file,
			Line:     byFile[file],
			Column:   1,
			Severity: severityError,
			Code:     "format",
			Message:  "Shell file is not shfmt-formatted.",
		})
	}

	return diagnostics
}

func normalizeShfmtDiffPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "b/")
	path = strings.TrimSuffix(path, ".orig")

	if path == "/dev/null" {
		return ""
	}

	return path
}

func parseYamllint(output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		matches := yamllintPattern.FindStringSubmatch(line)
		if len(matches) != yamllintMatchParts {
			continue
		}

		lineNo, validLine := parseInt(matches[2])
		column, validColumn := parseInt(matches[3])

		if !validLine || !validColumn {
			continue
		}

		message := strings.TrimSpace(matches[4])
		severity := ""

		switch {
		case strings.Contains(message, "[error]"):
			severity = severityError
		case strings.Contains(message, "[warning]"):
			severity = severityWarning
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
			File:     strings.TrimSpace(matches[1]),
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
	payload, err := decodeBanditOutput(output)
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

type banditOutput struct {
	Errors  []string       `json:"errors"`
	Results []banditResult `json:"results"`
}

type banditResult struct {
	Filename        string `json:"filename"`
	IssueSeverity   string `json:"issue_severity"`
	IssueConfidence string `json:"issue_confidence"`
	IssueText       string `json:"issue_text"`
	TestID          string `json:"test_id"`
	LineNumber      int    `json:"line_number"`
}

func decodeBanditOutput(output string) (banditOutput, error) {
	var payload banditOutput

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload)
	if err != nil {
		return banditOutput{}, fmt.Errorf("decode bandit output: %w", err)
	}

	return payload, nil
}

func recognizesCleanBanditOutput(output string) bool {
	payload, err := decodeBanditOutput(output)

	return err == nil && len(payload.Errors) == 0 && len(payload.Results) == 0
}

func banditSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "high":
		return severityError
	case "medium":
		return severityWarning
	case "low":
		return "notice"
	default:
		return firstNonEmpty(strings.ToLower(strings.TrimSpace(value)), severityWarning)
	}
}

func parseSQLFluff(output string) []Diagnostic {
	var items []struct {
		Filepath   string `json:"filepath"`
		Violations []struct {
			Code         string `json:"code"`
			Description  string `json:"description"`
			Name         string `json:"name"`
			LineNo       int    `json:"line_no"`
			LinePos      int    `json:"line_pos"`
			StartLineNo  int    `json:"start_line_no"`
			StartLinePos int    `json:"start_line_pos"`
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
			severity := severityError
			if violation.Warning {
				severity = severityWarning
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
	for index := range lines {
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
	case severityError:
		return severityError
	case severityWarning, "warn":
		return severityWarning
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
			Severity: severityWarning,
			Code:     matches[3],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return diagnostics
}

func parseESLint(output string) []Diagnostic {
	var payload []eslintFileDiagnostic

	err := json.Unmarshal([]byte(strings.TrimSpace(output)), &payload)
	if err != nil {
		return nil
	}

	diagnostics := []Diagnostic{}

	for _, file := range payload {
		for _, message := range file.Messages {
			text := strings.TrimSpace(message.Message)
			if text == "" {
				continue
			}

			diagnostics = append(diagnostics, Diagnostic{
				Tool:     "eslint",
				File:     strings.TrimSpace(file.FilePath),
				Line:     message.Line,
				Column:   message.Column,
				Severity: eslintSeverity(message.Severity, message.Fatal),
				Code:     eslintRuleID(message.RuleID, message.Fatal),
				Message:  text,
				Metadata: eslintDiagnosticMetadata(
					message.EndLine,
					message.EndColumn,
				),
			})
		}
	}

	return diagnostics
}

func eslintSeverity(level int, fatal bool) string {
	if fatal {
		return severityError
	}

	switch level {
	case eslintErrorSeverity:
		return severityError
	case 1:
		return severityWarning
	default:
		return severityNotice
	}
}

func eslintRuleID(ruleID *string, fatal bool) string {
	if ruleID != nil && strings.TrimSpace(*ruleID) != "" {
		return strings.TrimSpace(*ruleID)
	}

	if fatal {
		return "fatal"
	}

	return ""
}

func eslintDiagnosticMetadata(endLine, endColumn int) map[string]any {
	if endLine == 0 && endColumn == 0 {
		return nil
	}

	metadata := map[string]any{}
	if endLine > 0 {
		metadata["end_line"] = endLine
	}

	if endColumn > 0 {
		metadata["end_column"] = endColumn
	}

	return metadata
}

func parseTSC(output string) []Diagnostic {
	diagnostics := []Diagnostic{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		if diagnostic, ok := parseTSCLine(trimmed); ok {
			diagnostics = append(diagnostics, diagnostic)
		}
	}

	return diagnostics
}

func parseTSCLine(line string) (Diagnostic, bool) {
	matches := tscDiagnosticPattern.FindStringSubmatch(line)
	if len(matches) == tscDiagnosticMatchParts {
		lineNo, validLine := parseInt(matches[2])
		column, validColumn := parseInt(matches[3])

		if !validLine || !validColumn {
			return Diagnostic{}, false
		}

		return Diagnostic{
			Tool:     "tsc",
			File:     strings.TrimSpace(matches[1]),
			Line:     lineNo,
			Column:   column,
			Severity: tscSeverity(matches[4]),
			Code:     strings.TrimSpace(matches[5]),
			Message:  strings.TrimSpace(matches[6]),
		}, true
	}

	matches = tscPathlessPattern.FindStringSubmatch(line)
	if len(matches) == tscPathlessMatchParts {
		return Diagnostic{
			Tool:     "tsc",
			Severity: tscSeverity(matches[1]),
			Code:     strings.TrimSpace(matches[2]),
			Message:  strings.TrimSpace(matches[3]),
		}, true
	}

	return Diagnostic{}, false
}

func tscSeverity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "warning":
		return severityWarning
	default:
		return severityError
	}
}
