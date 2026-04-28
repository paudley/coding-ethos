// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

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
)

var (
	shellStrictModePattern = regexp.MustCompile(
		`(?m)^\s*set\s+-[euo]+\s*pipefail|^\s*set\s+-euo\s+pipefail`,
	)
	shellCommonSourcePattern = regexp.MustCompile(
		`(?m)source\s+.*common\.sh|^\.\s+.*common\.sh`,
	)
)

func EvaluateShellBestPractices(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	requireCommon := stringSliceOption(
		context.EvaluatorOptions,
		"require_common_for_prefixes",
		[]string{"scripts/"},
	)

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
) []string {
	violations := []string{}
	if !validShellShebang(text) {
		violations = append(violations, "missing or invalid shell shebang")
	}

	if !shellStrictModePattern.MatchString(text) {
		violations = append(violations, "missing 'set -euo pipefail'")
	}

	if hasConfiguredPrefix(path, requireCommonForPrefixes) &&
		!shellCommonSourcePattern.MatchString(text) {
		violations = append(
			violations,
			"scripts/ shell files must source the repository common shell helpers",
		)
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
	violations []string,
) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	diagnosticItems := make([]diagnostics.Diagnostic, 0, len(violations))
	for _, violation := range violations {
		diagnosticItems = append(diagnosticItems, diagnostics.Diagnostic{
			Tool:     "shell_best_practices",
			File:     file,
			Severity: blockDecision,
			PolicyID: policyDef.ID,
			Message:  violation,
			Advice:   policyDef.Suggestion,
		})
	}
	decision.Diagnostics = diagnosticItems
	decision.Evidence = map[string]any{
		"file":       file,
		"violations": append([]string(nil), violations...),
	}

	return decision
}
