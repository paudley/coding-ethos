// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package diagnostics

import (
	"encoding/json"
	"strings"
)

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
			Severity: severityError,
			Code:     matches[5],
			Message:  strings.TrimSpace(matches[4]),
		})
	}

	return diagnostics
}

func parseGofmt(output string) []Diagnostic {
	return parseFilenameList(
		"gofmt",
		output,
		"format",
		"Go file is not gofmt-formatted.",
	)
}

func parseFilenameList(tool, output, code, message string) []Diagnostic {
	diagnostics := []Diagnostic{}
	seen := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		file := strings.TrimSpace(line)
		if file == "" || strings.HasPrefix(file, "#") || seen[file] {
			continue
		}

		seen[file] = true
		diagnostics = append(diagnostics, Diagnostic{
			Tool:     tool,
			File:     file,
			Line:     1,
			Severity: severityError,
			Code:     code,
			Message:  message,
		})
	}

	return diagnostics
}

func parseGoVet(output string) []Diagnostic {
	diagnostics := []Diagnostic{}
	currentPackage := ""

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if packageName, ok := strings.CutPrefix(line, "# "); ok {
			currentPackage = strings.TrimSpace(packageName)

			continue
		}

		items := parseFallback("go-vet", line)
		for _, item := range items {
			if item.Code == "" {
				item.Code = "vet"
			}

			if currentPackage != "" {
				item.Metadata = map[string]any{"package": currentPackage}
			}

			diagnostics = append(diagnostics, item)
		}
	}

	return diagnostics
}

//nolint:tagliatelle // GolangCI-Lint JSON uses published upper-case fields.
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
			Severity: firstNonEmpty(issue.Severity, severityError),
			Code:     issue.FromLinter,
			Message:  issue.Text,
		})
	}

	return diagnostics
}
