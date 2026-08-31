// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

var (
	shellStrictModePattern = regexp.MustCompile(
		`(?m)^\s*set\s+-[euo]+\s*pipefail|^\s*set\s+-euo\s+pipefail`,
	)
	shellCommonSourcePattern = regexp.MustCompile(
		`(?m)source\s+.*common\.sh|^\.\s+.*common\.sh`,
	)
)

type shellViolation struct {
	Message string
	Line    int
	Column  int
}

func EvaluateShellBestPractices(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	requireCommon := stringSliceOption(
		context.EvaluatorOptions,
		"require_common_for_prefixes",
		[]string{"scripts/"},
	)
	if !repositoryHasTrackedCommonShellHelper(context.Cwd) {
		requireCommon = nil
	}

	for _, file := range context.Files {
		if !looksLikeShellFile(file) {
			continue
		}

		text, binary, err := readShellText(file)
		if err != nil {
			return nil, err
		}

		if binary {
			continue
		}

		violations := shellBestPracticeViolations(file, text, requireCommon)
		if len(violations) > 0 {
			return []policy.Decision{
				shellBestPracticesDecision(policyDef, file, violations),
			}, nil
		}
	}

	return nil, nil
}

func repositoryHasTrackedCommonShellHelper(cwd string) bool {
	if strings.TrimSpace(cwd) == "" {
		return false
	}

	output, err := GitCommand(cwd, "ls-files", "--cached").Output()
	if err != nil {
		return false
	}

	for line := range strings.SplitSeq(string(output), "\n") {
		if filepath.Base(strings.TrimSpace(line)) == "common.sh" {
			return true
		}
	}

	return false
}

func looksLikeShellFile(path string) bool {
	base := filepath.Base(path)
	switch strings.ToLower(filepath.Ext(path)) {
	case ".sh", ".bash", ".zsh":
		return true
	default:
		return base == "bashrc" || base == "zshrc"
	}
}

func readShellText(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read shell file %s: %w", path, err)
	}

	if !utf8.Valid(data) || bytes.Contains(data, []byte{0}) {
		return "", true, nil
	}

	return string(data), false, nil
}

func shellBestPracticeViolations(
	path string,
	text string,
	requireCommonForPrefixes []string,
) []shellViolation {
	violations := []shellViolation{}
	violations = append(
		violations,
		shellHeaderViolations(path, text, requireCommonForPrefixes)...)

	commands, err := shellparse.Commands(text)
	if err != nil {
		return append(violations, shellParseViolation(err))
	}

	for _, command := range commands {
		violations = append(violations, shellCommandViolations(command)...)
	}

	return violations
}

func shellHeaderViolations(
	path string,
	text string,
	requireCommonForPrefixes []string,
) []shellViolation {
	violations := []shellViolation{}
	if !validShellShebang(text) {
		violations = append(violations, shellViolation{
			Message: "missing or invalid shell shebang",
			Line:    1,
			Column:  1,
		})
	}

	if !shellStrictModePattern.MatchString(text) {
		violations = append(violations, shellViolation{
			Message: "missing 'set -euo pipefail'",
			Line:    1,
			Column:  1,
		})
	}

	if hasConfiguredPrefix(path, requireCommonForPrefixes) &&
		!shellCommonSourcePattern.MatchString(text) {
		violations = append(violations, shellViolation{
			Message: "scripts/ shell files must source the repository common shell helpers",
			Line:    1,
			Column:  1,
		})
	}

	return violations
}

func shellParseViolation(err error) shellViolation {
	violation := shellViolation{
		Message: "shell script has invalid shell syntax",
		Line:    1,
		Column:  1,
	}

	var parseErr shellparse.Error
	if errors.As(err, &parseErr) {
		violation.Line = parseErr.Line
		violation.Column = parseErr.Column
	}

	return violation
}

func shellCommandViolations(command shellparse.Command) []shellViolation {
	violations := []shellViolation{}
	if command.Name == "eval" {
		violations = append(violations, shellViolation{
			Message: "shell scripts must not use eval",
			Line:    command.Line,
			Column:  command.Column,
		})
	}

	if command.IsFunctionDeclaration &&
		(command.Name == "git" || command.Name == "ruff" || command.Name == "mypy") {
		violations = append(violations, shellViolation{
			Message: "shell functions must not mask protected tool names",
			Line:    command.Line,
			Column:  command.Column,
		})
	}

	return violations
}

func validShellShebang(text string) bool {
	reader := strings.NewReader(text)

	line, err := readFirstLine(reader)
	if err != nil && !errors.Is(err, io.EOF) {
		return false
	}

	return strings.HasPrefix(line, "#!/usr/bin/env bash") ||
		strings.HasPrefix(line, "#!/bin/bash") ||
		strings.HasPrefix(line, "#!/usr/bin/env sh") ||
		strings.HasPrefix(line, "#!/bin/sh")
}

func readFirstLine(reader *strings.Reader) (string, error) {
	var builder strings.Builder

	for {
		char, _, err := reader.ReadRune()
		if err != nil {
			return builder.String(), err
		}

		if char == '\n' {
			return builder.String(), nil
		}

		builder.WriteRune(char)
	}
}

func hasConfiguredPrefix(path string, prefixes []string) bool {
	normalized := filepath.ToSlash(path)

	normalized = strings.TrimPrefix(normalized, "./")
	for _, prefix := range prefixes {
		cleaned := strings.TrimPrefix(filepath.ToSlash(prefix), "./")
		if cleaned != "" && strings.HasPrefix(normalized, cleaned) {
			return true
		}
	}

	return false
}

func shellBestPracticesDecision(
	policyDef policy.Policy,
	file string,
	violations []shellViolation,
) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	diagnosticItems := make([]diagnostics.Diagnostic, 0, len(violations))

	messages := make([]string, 0, len(violations))
	for _, violation := range violations {
		messages = append(messages, violation.Message)
		diagnosticItems = append(diagnosticItems, diagnostics.Diagnostic{
			Tool:     "shell_best_practices",
			File:     file,
			Line:     violation.Line,
			Column:   violation.Column,
			Severity: blockDecision,
			PolicyID: policyDef.ID,
			Message:  violation.Message,
			Advice:   policyDef.Suggestion,
		})
	}

	decision.Diagnostics = diagnosticItems
	decision.Evidence = map[string]any{
		"file":       file,
		"violations": messages,
	}

	return decision
}
