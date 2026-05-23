// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func loadCommentSuppressionSettings() (commentSuppressionSettings, error) {
	var settings commentSuppressionSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}

	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.comment_suppressions",
		"comment_suppressions",
		&settings,
	)
	if err != nil {
		return settings, err
	}

	if !sectionFound {
		return settings, nil
	}

	if len(settings.Patterns) == 0 {
		settings.Patterns = []commentSuppressionPattern{
			{Regex: `#\s*ruff:\s*noqa\b`, Label: "ruff: noqa (file-level)"},
			{
				Regex: `#\s*mypy:\s*ignore-errors\b`,
				Label: "mypy: ignore-errors (file-level)",
			},
			{Regex: `#\s*noqa\b`, Label: "noqa"},
			{Regex: `#\s*type:\s*ignore\b`, Label: "type: ignore"},
			{Regex: `#\s*pragma:\s*no\s*cover\b`, Label: "pragma: no cover"},
			{Regex: `#\s*pylint:\s*disable`, Label: "pylint: disable"},
			{Regex: `#\s*noinspection\b`, Label: "noinspection"},
			{Regex: `#\s*fmt:\s*(off|on|skip)\b`, Label: "fmt: off/on/skip"},
			{Regex: `#\s*isort:\s*(skip|skip_file)\b`, Label: "isort: skip"},
			{Regex: `#\s*pyright:\s*ignore\b`, Label: "pyright: ignore"},
		}
	}

	return settings, nil
}

func compileCommentSuppressionPatterns(
	settings commentSuppressionSettings,
) ([]compiledCommentSuppressionPattern, error) {
	compiled := make([]compiledCommentSuppressionPattern, 0, len(settings.Patterns))
	for _, pattern := range settings.Patterns {
		expr := strings.TrimSpace(pattern.Regex)

		label := strings.TrimSpace(pattern.Label)
		if expr == "" || label == "" {
			continue
		}

		regex, err := regexp.Compile(expr)
		if err != nil {
			return nil, fmt.Errorf(
				"invalid comment suppression regex %q: %w",
				expr,
				err,
			)
		}

		compiled = append(compiled, compiledCommentSuppressionPattern{
			Regex: regex,
			Label: label,
		})
	}

	return compiled, nil
}

func classifyCommentSuppression(
	comment string,
	patterns []compiledCommentSuppressionPattern,
) string {
	for _, pattern := range patterns {
		if pattern.Regex.MatchString(comment) {
			return pattern.Label
		}
	}

	return ""
}

func findPythonComments(text string) []commentSuppressionViolation {
	scanner := newPythonCommentScanner(text)

	return scanner.scan()
}

type pythonCommentScanState int

const (
	scanNormal pythonCommentScanState = iota
	scanSingleQuote
	scanDoubleQuote
	scanTripleSingleQuote
	scanTripleDoubleQuote
)

type pythonCommentScanner struct {
	text       string
	violations []commentSuppressionViolation
	state      pythonCommentScanState
	line       int
	cursor     int
}

func newPythonCommentScanner(text string) *pythonCommentScanner {
	return &pythonCommentScanner{
		text:       text,
		violations: make([]commentSuppressionViolation, 0),
		state:      scanNormal,
		line:       1,
	}
}

func (scanner *pythonCommentScanner) scan() []commentSuppressionViolation {
	for scanner.cursor < len(scanner.text) {
		scanner.step()
	}

	return scanner.violations
}

func (scanner *pythonCommentScanner) step() {
	switch scanner.state {
	case scanNormal:
		scanner.stepNormal()
	case scanSingleQuote:
		scanner.stepQuoted('\'')
	case scanDoubleQuote:
		scanner.stepQuoted('"')
	case scanTripleSingleQuote:
		scanner.stepTripleQuoted('\'')
	case scanTripleDoubleQuote:
		scanner.stepTripleQuoted('"')
	}
}

func (scanner *pythonCommentScanner) stepNormal() {
	currentChar := scanner.text[scanner.cursor]
	switch currentChar {
	case '#':
		scanner.recordComment()
	case '\'':
		scanner.enterQuote(scanSingleQuote, scanTripleSingleQuote, '\'')
	case '"':
		scanner.enterQuote(scanDoubleQuote, scanTripleDoubleQuote, '"')
	case '\n':
		scanner.line++
		scanner.cursor++
	default:
		scanner.cursor++
	}
}

func (scanner *pythonCommentScanner) recordComment() {
	start := scanner.cursor
	for scanner.cursor < len(scanner.text) &&
		scanner.text[scanner.cursor] != '\n' {
		scanner.cursor++
	}

	scanner.violations = append(scanner.violations, commentSuppressionViolation{
		Line:    scanner.line,
		Comment: strings.TrimSpace(scanner.text[start:scanner.cursor]),
	})
}

func (scanner *pythonCommentScanner) enterQuote(
	singleState pythonCommentScanState,
	tripleState pythonCommentScanState,
	quote byte,
) {
	if scanner.hasTripleQuote(quote) {
		scanner.state = tripleState
		scanner.cursor += tripleQuoteLen

		return
	}

	scanner.state = singleState
	scanner.cursor++
}

func (scanner *pythonCommentScanner) stepQuoted(quote byte) {
	currentChar := scanner.text[scanner.cursor]
	switch currentChar {
	case '\\':
		scanner.advanceEscaped()
	case '\n':
		scanner.line++
		scanner.state = scanNormal
		scanner.cursor++
	case quote:
		scanner.state = scanNormal
		scanner.cursor++
	default:
		scanner.cursor++
	}
}

func (scanner *pythonCommentScanner) advanceEscaped() {
	if scanner.cursor+1 < len(scanner.text) &&
		scanner.text[scanner.cursor+1] == '\n' {
		scanner.line++
	}

	scanner.cursor += splitNParts
}

func (scanner *pythonCommentScanner) stepTripleQuoted(quote byte) {
	if scanner.text[scanner.cursor] == '\n' {
		scanner.line++
	}

	if scanner.hasTripleQuote(quote) {
		scanner.state = scanNormal
		scanner.cursor += tripleQuoteLen

		return
	}

	scanner.cursor++
}

func (scanner *pythonCommentScanner) hasTripleQuote(quote byte) bool {
	return scanner.cursor+2 < len(scanner.text) &&
		scanner.text[scanner.cursor] == quote &&
		scanner.text[scanner.cursor+1] == quote &&
		scanner.text[scanner.cursor+2] == quote
}

func findCommentSuppressions(
	path string,
	patterns []compiledCommentSuppressionPattern,
) ([]commentSuppressionViolation, error) {
	text, binary, err := readText(path)
	if err != nil {
		return nil, err
	}

	if binary {
		return nil, nil
	}

	comments := findPythonComments(text)
	violations := make([]commentSuppressionViolation, 0)

	for _, comment := range comments {
		label := classifyCommentSuppression(comment.Comment, patterns)
		if label == "" {
			continue
		}

		comment.File = path
		comment.Label = label
		violations = append(violations, comment)
	}

	return violations, nil
}

func checkCommentSuppressionsCommand(_ Config, args []string) int {
	settings, err := loadCommentSuppressionSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled || len(args) == 0 {
		return 0
	}

	patterns, err := compileCommentSuppressionPatterns(settings)
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	allViolations := make([]commentSuppressionViolation, 0)

	for _, path := range existingFiles(args) {
		if filepath.Ext(path) != extPy {
			continue
		}

		violations, err := findCommentSuppressions(path, patterns)
		if err != nil {
			writef(os.Stderr, "ERROR: %s: %v\n", path, err)

			return 1
		}

		allViolations = append(allViolations, violations...)
	}

	if len(allViolations) == 0 {
		return 0
	}

	findings := make([]hookFinding, 0, len(allViolations))
	for _, violation := range allViolations {
		findings = append(findings, hookFinding{
			Tool:    "comment_suppressions",
			File:    violation.File,
			Line:    violation.Line,
			Code:    violation.Label,
			Message: "comment-based lint suppression",
			Detail:  violation.Comment,
		})
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "comment_suppressions",
		Title:    "COMMENT-BASED LINT SUPPRESSION DETECTED",
		Summary:  "Comment-based suppressions are banned. Fix the underlying issue instead.",
		Findings: findings,
		Guidance: []string{
			"Remove the suppression comment and fix the code.",
			"Apply SOLID principles when a linter flags structural issues.",
		},
	}, selectedHookOutputFormat())

	return 1
}
