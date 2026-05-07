// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"
)

func runShellcheck(_ Config, paths []string) int {
	files := toolchainFiles("shellcheck", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"shellcheck",
		repoRoot(),
		toolchainCommandWithFiles("shellcheck", files),
		parseShellcheckFindings,
	)
}

func parseShellcheckFindings(output string) []hookFinding {
	return parseCatalogFindings("shellcheck", output)
}

func runShfmt(_ Config, paths []string) int {
	files := toolchainFiles("shfmt", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"shfmt",
		repoRoot(),
		toolchainCommandWithFiles("shfmt", files),
		parseShfmtFindings,
	)
}

func parseShfmtFindings(output string) []hookFinding {
	findings := []hookFinding{}
	seen := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		trimmed := strings.TrimSpace(line)
		if finding, ok := parseShfmtSyntaxFinding(trimmed); ok {
			findings = append(findings, finding)

			continue
		}

		file, ok := shfmtDiffFile(trimmed)
		if !ok || seen[file] {
			continue
		}

		seen[file] = true
		findings = append(findings, hookFinding{
			Tool:     "shfmt",
			File:     file,
			Severity: "error",
			Code:     "format",
			Message:  "shell file is not shfmt-formatted",
			Advice:   "Run shfmt with the repo formatting settings.",
		})
	}

	return findings
}

var shfmtSyntaxPattern = regexp.MustCompile(`^(.+?):(\d+):(\d+):\s*(.+)$`)

const shfmtSyntaxMatchParts = 5

func parseShfmtSyntaxFinding(line string) (hookFinding, bool) {
	matches := shfmtSyntaxPattern.FindStringSubmatch(line)
	if len(matches) != shfmtSyntaxMatchParts {
		return hookFinding{}, false
	}

	lineNo, validLine := parseDiagnosticInt(matches[2])

	column, validColumn := parseDiagnosticInt(matches[3])
	if !validLine || !validColumn {
		return hookFinding{}, false
	}

	return hookFinding{
		Tool:     "shfmt",
		File:     matches[1],
		Line:     lineNo,
		Column:   column,
		Severity: "error",
		Code:     "syntax",
		Message:  strings.TrimSpace(matches[4]),
	}, true
}

func shfmtDiffFile(line string) (string, bool) {
	if !strings.HasPrefix(line, "--- ") {
		return "", false
	}

	file := strings.TrimSpace(strings.TrimPrefix(line, "--- "))
	file = strings.TrimSuffix(file, ".orig")
	file = strings.Trim(file, "\"")

	if file == "" || file == "/dev/null" {
		return "", false
	}

	return file, true
}

func runYamllint(_ Config, paths []string) int {
	files := toolchainFiles("yamllint", existingFiles(paths))
	if len(files) == 0 {
		return 0
	}

	return runHookToolWithParser(
		"yamllint",
		repoRoot(),
		uvToolchainCommandWithRepoConfig("yamllint", ".yamllint.yml", files),
		parseYamllintFindings,
	)
}

func parseYamllintFindings(output string) []hookFinding {
	return parseCatalogFindings("yamllint", output)
}

func isShellFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == extShell || ext == extBash {
		return true
	}

	data, err := os.ReadFile(path)
	if err != nil || !utf8.Valid(data) {
		return false
	}

	firstLine, _, _ := strings.Cut(string(data), "\n")

	return strings.HasPrefix(firstLine, "#!") &&
		(strings.Contains(firstLine, "bash") || strings.Contains(firstLine, "sh"))
}
