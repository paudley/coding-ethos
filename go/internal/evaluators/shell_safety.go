// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
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
		if value == "/coding-ethos/pre-commit/hooks/" &&
			commandIsOnlyTrustedRunGoHookInvocation(command) {
			return nil, nil
		}

		return blockForbiddenStringDecision(
			policyDef,
			command,
			"command",
			forbiddenStringMatch{Value: value, Matched: true},
		), nil
	}

	for _, file := range referencedCommandFiles(context) {
		if forbiddenStringFileExempt(context.Cwd, file, context.EvaluatorOptions) {
			continue
		}

		match := referencedFileForbiddenStringMatch(
			context.Cwd,
			file,
			forbiddenFileStrings(context.EvaluatorOptions),
		)
		if match.Matched {
			return blockForbiddenStringDecision(policyDef, command, file, match), nil
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

func commandIsOnlyTrustedRunGoHookInvocation(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}

	for _, field := range fields {
		if isShellControlWord(field) {
			return false
		}
	}

	for len(fields) > 0 && isShellEnvAssignmentWord(fields[0]) {
		fields = fields[1:]
	}
	if len(fields) == 0 {
		return false
	}

	cleaned := filepath.ToSlash(filepath.Clean(fields[0]))
	if cleaned == "pre-commit/hooks/run-go-hook.sh" {
		return true
	}

	for _, candidate := range trustedRunGoHookCandidatesForEvaluator(fields[0]) {
		if cleaned == candidate {
			return true
		}
	}

	return false
}

func trustedRunGoHookCandidatesForEvaluator(command string) []string {
	candidates := []string{
		os.Getenv("CODING_ETHOS_RUN_GO_HOOK"),
		filepath.Join(os.Getenv("CODE_ETHOS_PRECOMMIT_ROOT"), "hooks", "run-go-hook.sh"),
	}

	if !filepath.IsAbs(command) {
		for _, root := range []string{
			os.Getenv("INVOCATION_CWD"),
			os.Getenv("CODE_ETHOS_CONSUMER_ROOT"),
		} {
			if root == "" {
				continue
			}

			candidates = append(candidates, filepath.Join(root, command))
		}
	}

	cleaned := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}

		cleaned = append(cleaned, filepath.ToSlash(filepath.Clean(candidate)))
	}

	return cleaned
}

func isShellControlWord(word string) bool {
	switch word {
	case "&&", "||", ";", "|", "&":
		return true
	default:
		return false
	}
}

func isShellEnvAssignmentWord(word string) bool {
	name, value, ok := strings.Cut(word, "=")
	if !ok || name == "" || value == "" || strings.HasPrefix(name, "-") {
		return false
	}

	for index, char := range name {
		if char == '_' ||
			(char >= 'A' && char <= 'Z') ||
			(char >= 'a' && char <= 'z') ||
			(index > 0 && char >= '0' && char <= '9') {
			continue
		}

		return false
	}

	return true
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

type forbiddenStringMatch struct {
	Value   string
	Line    int
	Matched bool
}

func referencedFileForbiddenStringMatch(
	cwd string,
	file string,
	forbidden []string,
) forbiddenStringMatch {
	path := cleanEvaluatorPath(cwd, file)

	info, err := os.Stat(path)
	if err != nil || info.IsDir() || !info.Mode().IsRegular() {
		return forbiddenStringMatch{}
	}

	const maxForbiddenStringScanBytes = 1 << 20
	if info.Size() > maxForbiddenStringScanBytes {
		return forbiddenStringMatch{}
	}

	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return forbiddenStringMatch{}
	}

	lines := strings.Split(string(content), "\n")
	for index, line := range lines {
		if matched, value := containsForbiddenString(line, forbidden); matched {
			return forbiddenStringMatch{
				Value:   value,
				Line:    index + 1,
				Matched: true,
			}
		}
	}

	return forbiddenStringMatch{}
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
	match forbiddenStringMatch,
) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"command":          command,
		"location":         location,
		"forbidden_string": match.Value,
	}
	if match.Line > 0 {
		decision.Evidence["line"] = match.Line
	}
	decision.Diagnostics = []diagnostics.Diagnostic{{
		Tool:     "policy",
		File:     location,
		Line:     match.Line,
		Severity: blockDecision,
		PolicyID: policyDef.ID,
		Message:  "forbidden string detected: " + match.Value,
		Advice:   policyDef.Suggestion,
		Detail:   "location=" + location + "; line=" + strconv.Itoa(match.Line),
	}}

	return []policy.Decision{decision}
}
