// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func EvaluateShellDangerousCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	lower := strings.ToLower(command)
	switch {
	case strings.Contains(lower, "rm -rf") || strings.Contains(lower, "rm -fr"):
		return blockShellDecision(policyDef, command), nil
	case strings.Contains(lower, "curl ") && pipesToShell(lower):
		return blockShellDecision(policyDef, command), nil
	case strings.Contains(lower, "wget ") && pipesToShell(lower):
		return blockShellDecision(policyDef, command), nil
	case strings.Contains(lower, "chmod 777"):
		return blockShellDecision(policyDef, command), nil
	default:
		return nil, nil
	}
}

func EvaluateShellBackgroundGit(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	lower := strings.ToLower(command)
	if !strings.Contains(lower, "git commit") && !strings.Contains(lower, "git push") {
		return nil, nil
	}

	if strings.Contains(lower, "timeout ") || strings.Contains(lower, " &") ||
		strings.HasSuffix(strings.TrimSpace(lower), "&") {
		return blockShellDecision(policyDef, command), nil
	}

	return nil, nil
}

func EvaluateShellGitHubAdmin(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if len(argv) == 0 {
		return nil, nil
	}

	if argv[0] != "gh" || !hasArg(argv, "--admin") {
		return nil, nil
	}

	return blockShellDecision(policyDef, strings.Join(argv, " ")), nil
}

func EvaluateShellForbiddenStrings(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	if matched, value := containsForbiddenString(
		command,
		forbiddenCommandStrings(context.EvaluatorOptions),
	); matched {
		return blockForbiddenStringDecision(policyDef, command, "command", value), nil
	}

	for _, file := range referencedCommandFiles(context) {
		if forbiddenStringFileExempt(context.Cwd, file, context.EvaluatorOptions) {
			continue
		}

		matched, value := referencedFileContainsForbiddenString(
			context.Cwd,
			file,
			forbiddenFileStrings(context.EvaluatorOptions),
		)
		if matched {
			return blockForbiddenStringDecision(policyDef, command, file, value), nil
		}
	}

	return nil, nil
}

func forbiddenCommandStrings(options map[string]any) []string {
	return stringSliceOption(options, "strings", []string{
		"/.claude/settings.json",
		"/.claude/settings.local.json",
		"~/.claude/settings.json",
		"~/.claude/settings.local.json",
		"/.codex/config.toml",
		"/.codex/hooks.json",
		"/.gemini/settings.json",
		"/coding-ethos/pre-commit/hooks/",
		"/coding-ethos/go/internal/",
		"/coding-ethos/config.yaml",
		"/coding-ethos/ruff.toml",
		"/coding-ethos/.golangci.yml",
		"header must match",
	})
}

func forbiddenFileStrings(options map[string]any) []string {
	return stringSliceOption(options, "file_strings", nil)
}

func forbiddenStringFileExempt(cwd string, file string, options map[string]any) bool {
	path := cleanEvaluatorPath(cwd, file)
	for _, exempt := range stringSliceOption(options, "exempt_paths", nil) {
		if path == cleanEvaluatorPath(cwd, exempt) {
			return true
		}
	}

	return false
}

func containsForbiddenString(text string, forbidden []string) (bool, string) {
	lower := strings.ToLower(text)
	for _, value := range forbidden {
		if strings.Contains(lower, strings.ToLower(value)) {
			return true, value
		}
	}

	return false, ""
}

func referencedCommandFiles(context Context) []string {
	files := append([]string{}, context.Files...)
	for _, arg := range context.Argv {
		if arg == "" || strings.HasPrefix(arg, "-") || strings.Contains(arg, "=") {
			continue
		}

		files = append(files, arg)
	}

	return dedupeEvaluatorStrings(files)
}

func referencedFileContainsForbiddenString(
	cwd string,
	file string,
	forbidden []string,
) (bool, string) {
	path := cleanEvaluatorPath(cwd, file)

	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return false, ""
	}

	const maxForbiddenStringScanBytes = 1 << 20
	if info.Size() > maxForbiddenStringScanBytes {
		return false, ""
	}

	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return false, ""
	}

	return containsForbiddenString(string(content), forbidden)
}

func cleanEvaluatorPath(cwd string, file string) string {
	path := file
	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}

	return filepath.Clean(path)
}

func dedupeEvaluatorStrings(values []string) []string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}

		seen[value] = true
		result = append(result, value)
	}

	return result
}

func pipesToShell(command string) bool {
	return strings.Contains(command, "| sh") ||
		strings.Contains(command, "| bash") ||
		strings.Contains(command, "| /bin/sh") ||
		strings.Contains(command, "| /bin/bash")
}

func blockShellDecision(policyDef policy.Policy, command string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{"command": command}

	return []policy.Decision{decision}
}

func blockForbiddenStringDecision(
	policyDef policy.Policy,
	command string,
	location string,
	value string,
) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"command":          command,
		"location":         location,
		"forbidden_string": value,
	}

	return []policy.Decision{decision}
}
