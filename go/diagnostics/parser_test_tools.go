// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package diagnostics

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

func parseGoTest(output string) []Diagnostic {
	state := goTestParseState{
		Outputs: map[string][]string{},
	}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		event, ok := parseGoTestEventLine(line)
		if !ok {
			if diagnostic, found := parseGoCoverageTextLine(line, ""); found {
				state.Diagnostics = append(state.Diagnostics, diagnostic)
			}

			continue
		}

		state.Apply(event)
	}

	return state.Diagnostics
}

//nolint:tagliatelle // go test -json uses exported Go-style field names.
type goTestEvent struct {
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Action  string  `json:"Action"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

type goTestParseState struct {
	Outputs     map[string][]string
	Diagnostics []Diagnostic
}

func parseGoTestEventLine(line string) (goTestEvent, bool) {
	var event goTestEvent

	err := json.Unmarshal([]byte(strings.TrimSpace(line)), &event)
	if err != nil || event.Action == "" {
		return goTestEvent{}, false
	}

	return event, true
}

func (state *goTestParseState) Apply(event goTestEvent) {
	key := goTestEventKey(event)
	if event.Action == "output" && event.Output != "" {
		state.Outputs[key] = append(state.Outputs[key], event.Output)
		if diagnostic, ok := parseGoTestCoverageLine(event); ok {
			state.Diagnostics = append(state.Diagnostics, diagnostic)
		}

		return
	}

	if event.Action != "fail" {
		return
	}

	if event.Test != "" {
		state.Diagnostics = append(
			state.Diagnostics,
			goTestDiagnosticsForFailedTest(event, state.Outputs[key])...,
		)

		return
	}

	state.Diagnostics = append(state.Diagnostics, Diagnostic{
		Tool:     "go-test",
		Severity: severityError,
		Code:     "package_failed",
		Message:  "Go test package failed: " + event.Package,
	})
}

func goTestEventKey(event goTestEvent) string {
	return event.Package + "\x00" + event.Test
}

func parseGoTestCoverageLine(event goTestEvent) (Diagnostic, bool) {
	return parseGoCoverageTextLine(event.Output, event.Package)
}

func parseGoCoverageTextLine(line, packageName string) (Diagnostic, bool) {
	line = strings.TrimSpace(line)

	matches := goTestCoveragePattern.FindStringSubmatch(line)
	if len(matches) == goCoverageMatchParts {
		return goCoverageDiagnostic(
			"go-test",
			"coverage-package",
			packageName,
			"",
			0,
			matches[1],
			false,
		)
	}

	matches = goCoverTotalPattern.FindStringSubmatch(line)
	if len(matches) == goCoverageMatchParts {
		return goCoverageDiagnostic("go-test", "coverage-total", "", "", 0, matches[1], true)
	}

	matches = goCoverFilePattern.FindStringSubmatch(line)
	if len(matches) == goCoverFileMatchParts {
		lineNumber, err := strconv.Atoi(matches[2])
		if err != nil {
			return Diagnostic{}, false
		}

		return goCoverageDiagnostic(
			"go-test",
			"coverage-file",
			packageName,
			matches[1],
			lineNumber,
			matches[3],
			false,
		)
	}

	return Diagnostic{}, false
}

func goCoverageDiagnostic(
	tool string,
	code string,
	packageName string,
	file string,
	line int,
	rawPercent string,
	total bool,
) (Diagnostic, bool) {
	coverage, err := strconv.ParseFloat(rawPercent, 64)
	if err != nil {
		return Diagnostic{}, false
	}

	metadata := map[string]any{
		"coverage_percent": coverage,
	}
	if packageName != "" {
		metadata["package"] = packageName
	}

	message := fmt.Sprintf("Go test coverage is %.2f%%.", coverage)
	if file != "" {
		message = fmt.Sprintf("Go test coverage for %s is %.2f%%.", file, coverage)
	} else if packageName != "" {
		message = fmt.Sprintf("Go test coverage for %s is %.2f%%.", packageName, coverage)
	}

	if total {
		code = "coverage-total"
	}

	return Diagnostic{
		Metadata: metadata,
		Tool:     tool,
		Severity: "record",
		Code:     code,
		File:     file,
		Line:     line,
		Message:  message,
	}, true
}

func goTestDiagnosticsForFailedTest(
	event goTestEvent,
	outputs []string,
) []Diagnostic {
	diagnostics := []Diagnostic{}

	for _, output := range outputs {
		diagnostic, ok := parseGoTestFileLine(output, event.Test)
		if !ok {
			continue
		}

		diagnostics = append(diagnostics, diagnostic)
	}

	if len(diagnostics) > 0 {
		return diagnostics
	}

	return []Diagnostic{{
		Tool:     "go-test",
		Severity: severityError,
		Code:     event.Test,
		Message:  "Go test failed: " + firstNonEmpty(event.Test, event.Package),
	}}
}

func parseGoTestFileLine(line, currentTest string) (Diagnostic, bool) {
	matches := goTestFileLinePattern.FindStringSubmatch(strings.TrimRight(line, "\r\n"))
	if len(matches) != goTestFileLineMatchParts {
		return Diagnostic{}, false
	}

	lineNo, validLine := parseInt(matches[2])
	if !validLine {
		return Diagnostic{}, false
	}

	message := strings.TrimSpace(matches[3])
	if message == "" && currentTest != "" {
		message = "Go test failed: " + currentTest
	}

	return Diagnostic{
		Tool:     "go-test",
		File:     strings.TrimSpace(matches[1]),
		Line:     lineNo,
		Severity: severityError,
		Code:     firstNonEmpty(currentTest, "test_failed"),
		Message:  message,
	}, true
}

func parsePytest(output string) []Diagnostic {
	diagnostics := []Diagnostic{}
	seen := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		text := strings.TrimSpace(line)
		if diagnostic, ok := parsePytestFileLine(text); ok {
			key := diagnosticKey(diagnostic)
			if seen[key] {
				continue
			}

			seen[key] = true

			diagnostics = append(diagnostics, diagnostic)

			continue
		}

		diagnostic, parsed := parsePytestSummaryLine(text)
		if !parsed {
			diagnostic, parsed = parsePytestCoverageTotalLine(text)
		}

		if !parsed {
			diagnostic, parsed = parsePytestCoverageFileLine(text)
		}

		if !parsed {
			continue
		}

		key := diagnosticKey(diagnostic)
		if seen[key] {
			continue
		}

		seen[key] = true

		diagnostics = append(diagnostics, diagnostic)
	}

	return diagnostics
}

func parsePytestFileLine(line string) (Diagnostic, bool) {
	matches := pytestFileLinePattern.FindStringSubmatch(line)
	if len(matches) != pytestFileLineMatchParts {
		return Diagnostic{}, false
	}

	lineNo, ok := parseInt(matches[2])
	if !ok {
		return Diagnostic{}, false
	}

	message := strings.TrimSpace(matches[3])
	if message == "" {
		message = "pytest failure"
	}

	return Diagnostic{
		Tool:     "pytest",
		File:     matches[1],
		Line:     lineNo,
		Severity: severityError,
		Code:     "test-failed",
		Message:  message,
	}, true
}

func parsePytestSummaryLine(line string) (Diagnostic, bool) {
	matches := pytestSummaryPattern.FindStringSubmatch(line)
	if len(matches) != pytestSummaryMatchParts {
		return Diagnostic{}, false
	}

	code := strings.ToLower(matches[1])
	if code == "" {
		code = "failed"
	}

	testName := strings.TrimSpace(matches[3])

	message := strings.TrimSpace(matches[4])
	if message == "" {
		message = "pytest " + code
	}

	diagnostic := Diagnostic{
		Tool:     "pytest",
		File:     strings.TrimSpace(matches[2]),
		Severity: severityError,
		Code:     "pytest-" + code,
		Message:  message,
	}
	if testName != "" {
		diagnostic.Metadata = map[string]any{"test": testName}
	}

	return diagnostic, true
}

func parsePytestCoverageTotalLine(line string) (Diagnostic, bool) {
	matches := pytestCoverageTotalPattern.FindStringSubmatch(line)
	if len(matches) != pytestCoverageMatchParts {
		return Diagnostic{}, false
	}

	coverage, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return Diagnostic{}, false
	}

	return Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": coverage,
		},
		Tool:     "pytest",
		Severity: "record",
		Code:     "coverage-total",
		Message:  fmt.Sprintf("Pytest coverage total is %.2f%%.", coverage),
	}, true
}

func parsePytestCoverageFileLine(line string) (Diagnostic, bool) {
	matches := pytestCoverageFilePattern.FindStringSubmatch(line)
	if len(matches) != pytestCoverageFileParts {
		return Diagnostic{}, false
	}

	coverage, err := strconv.ParseFloat(matches[2], 64)
	if err != nil {
		return Diagnostic{}, false
	}

	return Diagnostic{
		Metadata: map[string]any{
			"coverage_percent": coverage,
		},
		Tool:     "pytest",
		File:     strings.TrimSpace(matches[1]),
		Severity: "record",
		Code:     "coverage-file",
		Message:  fmt.Sprintf("Pytest coverage for %s is %.2f%%.", matches[1], coverage),
	}, true
}

func diagnosticKey(item Diagnostic) string {
	return strings.Join([]string{
		item.Tool,
		item.File,
		strconv.Itoa(item.Line),
		strconv.Itoa(item.Column),
		item.Code,
		item.Message,
	}, "\x00")
}
