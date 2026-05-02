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
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

func EvaluateShellMalformedCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if strings.TrimSpace(context.Command) == "" {
		return nil, nil
	}
	if _, err := shellparse.Commands(context.Command); err == nil {
		return nil, nil
	}

	return blockShellDecision(policyDef, context.Command), nil
}

func EvaluateShellDangerousCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	if shellDangerousCommand(command) {
		return blockShellDecision(policyDef, command), nil
	}

	return nil, nil
}

func shellDangerousCommand(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}
	tokens, err := shellparse.ControlFields(command)
	if err != nil {
		return true
	}

	for _, parsed := range commands {
		name := filepath.Base(parsed.Name)
		switch name {
		case "rm":
			if hasAnyArg(parsed.Argv, "-rf", "-fr") {
				return true
			}
		case "chmod":
			if hasArg(parsed.Argv, "777") {
				return true
			}
		case "curl", "wget":
			if commandPipelineToShell(tokens, name) {
				return true
			}
		case "eval":
			return true
		}
	}

	return false
}

func commandPipelineToShell(tokens []string, source string) bool {
	for index, token := range tokens {
		if filepath.Base(token) != source {
			continue
		}
		for cursor := index + 1; cursor < len(tokens)-1; cursor++ {
			if tokens[cursor] == "|" {
				switch filepath.Base(tokens[cursor+1]) {
				case "bash", "sh", "zsh", "dash":
					return true
				}
			}
			if tokens[cursor] == "&&" || tokens[cursor] == ";" || tokens[cursor] == "||" {
				break
			}
		}
	}

	return false
}

func hasAnyArg(argv []string, values ...string) bool {
	for _, value := range values {
		if hasArg(argv, value) {
			return true
		}
	}

	return false
}

func EvaluateShellBackgroundGit(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	command := context.Command
	if command == "" {
		command = strings.Join(context.Argv, " ")
	}

	if shellBackgroundGit(command) {
		return blockShellDecision(policyDef, command), nil
	}

	return nil, nil
}

func shellBackgroundGit(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}

	underTimeout := false
	for _, parsed := range commands {
		name := filepath.Base(parsed.Name)
		if name == "timeout" {
			underTimeout = true
			if timeoutWrapsGitMutation(parsed.Argv) {
				return true
			}
		}
		if name != "git" {
			continue
		}
		if len(parsed.Argv) < 2 {
			continue
		}
		subcommand := parsed.Argv[1]
		if subcommand != "commit" && subcommand != "push" {
			continue
		}
		if parsed.Background || underTimeout {
			return true
		}
	}

	return false
}

func timeoutWrapsGitMutation(argv []string) bool {
	for index, arg := range argv {
		if filepath.Base(arg) != "git" || index+1 >= len(argv) {
			continue
		}
		switch argv[index+1] {
		case "commit", "push":
			return true
		}
	}

	return false
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
			commandIsOnlyTrustedRunnerInvocation(command) {
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
	if isAgentWorkspacePath(path) {
		return true
	}
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

func commandIsOnlyTrustedRunnerInvocation(command string) bool {
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
	if cleaned == "bin/coding-ethos-run" {
		return true
	}

	for _, candidate := range trustedRunnerCandidatesForEvaluator(fields[0]) {
		if cleaned == candidate {
			return true
		}
	}

	return false
}

func trustedRunnerCandidatesForEvaluator(command string) []string {
	candidates := []string{
		os.Getenv("CODING_ETHOS_RUN_GO_HOOK"),
		filepath.Join(os.Getenv("CODE_ETHOS_PRECOMMIT_ROOT"), "..", "bin", "coding-ethos-run"),
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

func blockShellDecision(policyDef policy.Policy, command string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{"command": command}
	if commands, err := shellparse.Commands(command); err == nil {
		decision.Evidence["shell_commands"] = shellDecisionEvidence(commands)
	}

	return []policy.Decision{decision}
}

func shellDecisionEvidence(commands []shellparse.Command) []map[string]any {
	items := make([]map[string]any, 0, len(commands))
	for _, command := range commands {
		items = append(items, map[string]any{
			"argv":                     append([]string(nil), command.Argv...),
			"background":               command.Background,
			"column":                   command.Column,
			"has_command_substitution": command.HasCommandSubstitution,
			"has_dynamic_expansion":    command.HasDynamicExpansion,
			"has_heredoc":              command.HasHeredoc,
			"has_process_substitution": command.HasProcessSubstitution,
			"is_function_declaration":  command.IsFunctionDeclaration,
			"line":                     command.Line,
			"name":                     command.Name,
			"redirects":                append([]string(nil), command.Redirects...),
		})
	}

	return items
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
