// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	defaultPrivateKeyPattern = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
	executePermissionMask    = 0o111
	kibibyte                 = 1024
)

func EvaluateFileMergeConflict(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	markers := stringSliceOption(
		context.EvaluatorOptions,
		"markers",
		[]string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"},
	)

	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)
		text, binary, err := readGuardText(path)
		if err != nil {
			return nil, err
		}
		if binary {
			continue
		}

		for lineNumber, line := range strings.Split(text, "\n") {
			for _, marker := range markers {
				if strings.HasPrefix(line, marker) {
					return []policy.Decision{
						fileGuardDecision(
							policyDef,
							"merge_conflict",
							file,
							lineNumber+1,
							marker,
							"unresolved merge conflict marker",
						),
					}, nil
				}
			}
		}
	}

	return nil, nil
}

func EvaluateFilePrivateKey(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	pattern := stringOption(
		context.EvaluatorOptions,
		"pattern",
		defaultPrivateKeyPattern,
	)
	privateKey, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("compile private key pattern: %w", err)
	}

	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("read private-key candidate %s: %w", file, err)
		}

		if privateKey.Match(data) {
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"private_key",
					file,
					0,
					"",
					"possible private key detected",
				),
			}, nil
		}
	}

	return nil, nil
}

func EvaluateFileShebang(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)
		text, binary, err := readGuardText(path)
		if err != nil {
			return nil, err
		}
		if binary {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("stat shebang file %s: %w", file, err)
		}

		executable := info.Mode()&executePermissionMask != 0
		hasShebang := strings.HasPrefix(text, "#!")
		switch {
		case executable && !hasShebang:
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"shebangs",
					file,
					1,
					"",
					"executable file has no shebang",
				),
			}, nil
		case hasShebang && !executable:
			return []policy.Decision{
				fileGuardDecision(
					policyDef,
					"shebangs",
					file,
					1,
					"",
					"shebang script is not executable",
				),
			}, nil
		}
	}

	return nil, nil
}

func EvaluateFileLargeFile(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	suffixes := stringSet(normalizedSuffixes(stringSliceOption(
		context.EvaluatorOptions,
		"suffixes",
		[]string{".bash", ".json", ".py", ".pyi", ".sh", ".toml", ".yaml", ".yml"},
	)))
	excludePrefixes := stringSliceOption(
		context.EvaluatorOptions,
		"exclude_prefixes",
		nil,
	)
	maxKB := intOption(context.EvaluatorOptions, "max_kb", 500)

	for _, file := range context.Files {
		path := resolveGuardPath(context.Cwd, file)
		if !suffixes[strings.ToLower(filepath.Ext(file))] ||
			hasConfiguredPrefix(file, excludePrefixes) ||
			!isGitAddedFile(context.Cwd, file) {
			continue
		}

		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}

			return nil, fmt.Errorf("stat large-file candidate %s: %w", file, err)
		}

		if info.Size() > int64(maxKB*kibibyte) {
			message := fmt.Sprintf(
				"%d KiB exceeds %d KiB limit",
				info.Size()/kibibyte,
				maxKB,
			)

			return []policy.Decision{
				fileGuardDecision(policyDef, "large_files", file, 0, "", message),
			}, nil
		}
	}

	return nil, nil
}

func EvaluateFileLineLimit(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	pythonHard := intOption(context.EvaluatorOptions, "python_hard", 1000)
	shellHard := intOption(context.EvaluatorOptions, "shell_hard", 500)

	for _, file := range context.Files {
		if !isLineLimitedFile(file) {
			continue
		}

		path := resolveGuardPath(context.Cwd, file)
		text, binary, err := readGuardText(path)
		if err != nil {
			return nil, err
		}
		if binary {
			continue
		}

		lineCount := countGuardLines(text)
		hardLimit := lineLimitForFile(file, pythonHard, shellHard)
		if lineCount <= hardLimit {
			continue
		}

		originalCount := originalGitLineCount(context.Cwd, file)
		if originalCount >= 0 && lineCount <= originalCount {
			continue
		}

		message := fmt.Sprintf("new file has %d lines over %d limit", lineCount, hardLimit)
		if originalCount >= 0 {
			message = fmt.Sprintf(
				"file grew from %d to %d lines over %d limit",
				originalCount,
				lineCount,
				hardLimit,
			)
		}

		return []policy.Decision{
			fileGuardDecision(policyDef, "line_limits", file, 0, "", message),
		}, nil
	}

	return nil, nil
}

func resolveGuardPath(cwd string, path string) string {
	if filepath.IsAbs(path) || cwd == "" {
		return path
	}

	return filepath.Join(cwd, path)
}

func readGuardText(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("read file %s: %w", path, err)
	}

	if isBinaryBytes(data) {
		return "", true, nil
	}

	return string(data), false, nil
}

func isBinaryBytes(data []byte) bool {
	for _, value := range data {
		if value == 0 {
			return true
		}
	}

	return false
}

func normalizedSuffixes(suffixes []string) []string {
	normalized := make([]string, 0, len(suffixes))
	for _, suffix := range suffixes {
		value := strings.ToLower(strings.TrimSpace(suffix))
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		normalized = append(normalized, value)
	}

	return normalized
}

func isGitAddedFile(cwd string, path string) bool {
	cmd := gitCommand(cwd, "diff", "--cached", "--name-only", "--diff-filter=A", "--", path)

	output, err := cmd.Output()
	if err != nil {
		return false
	}

	for _, added := range strings.Split(string(output), "\n") {
		if added == path {
			return true
		}
	}

	return false
}

func isLineLimitedFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))

	return ext == ".py" || ext == ".sh" || ext == ".bash" ||
		strings.Contains(filepath.ToSlash(path), "scripts/")
}

func lineLimitForFile(path string, pythonHard int, shellHard int) int {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".sh" || ext == ".bash" || strings.Contains(filepath.ToSlash(path), "scripts/") {
		return shellHard
	}

	return pythonHard
}

func originalGitLineCount(cwd string, path string) int {
	cmd := gitCommand(cwd, "show", "HEAD:"+path)

	output, err := cmd.Output()
	if err != nil {
		return -1
	}

	return countGuardLines(string(output))
}

func countGuardLines(text string) int {
	trimmed := strings.TrimRight(text, "\n")
	if trimmed == "" {
		return 0
	}

	return len(strings.Split(trimmed, "\n"))
}

func fileGuardDecision(
	policyDef policy.Policy,
	tool string,
	file string,
	line int,
	code string,
	message string,
) policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     tool,
		File:     file,
		Line:     line,
		Severity: blockDecision,
		Code:     code,
		PolicyID: policyDef.ID,
		Message:  message,
		Advice:   policyDef.Suggestion,
	}}
	decision.Evidence = map[string]any{
		"file": file,
	}
	if code != "" {
		decision.Evidence["code"] = code
	}

	return decision
}
