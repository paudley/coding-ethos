// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"maps"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	gitWrapperPolicyID = "git.wrapper_required"
	permissionAllow    = "allow"
	wrapperBaseArgc    = 2
	tokenGit           = "git"
)

const (
	gitWrapperCircumventionRefusal = severeViolationWarning
	gitWrapperUseManagedSuggestion = "Run ordinary git commands without bypass flags " +
		"or shell indirection; approved git operations are routed by the hook " +
		"automatically. Do not try alternate shells, absolute git paths, Python " +
		"subprocesses, PATH edits, aliases, or other bypasses."
)

func gitWrapperRouteFor(event Event) InspectionRoute {
	if event.HookEventName != "PreToolUse" {
		return InspectionRoute{}
	}

	if event.ToolName != "Bash" {
		return InspectionRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return InspectionRoute{}
	}

	rewritten, rewrite, routeOK := rewriteGitCommandChain(command)
	if rewrite && routeOK {
		return InspectionRoute{
			UpdatedInput: updatedBashInput(
				event.ToolInput,
				rewritten,
			),
			Reason:  "Routed raw git through the approved git path.",
			Rewrite: true,
		}
	}

	if routeOK && managedGitCommandChain(command) {
		return InspectionRoute{}
	}

	if !routeOK || commandReferencesUnmanagedGit(command) || evasiveGitShell(command) {
		return InspectionRoute{
			Reason: sentence(
				gitWrapperCircumventionRefusal,
				gitWrapperUseManagedSuggestion,
			),
			Block: true,
		}
	}

	return InspectionRoute{}
}

func updatedBashInput(original map[string]any, command string) map[string]any {
	updated := map[string]any{}
	maps.Copy(updated, original)

	updated["command"] = command

	return updated
}

func wrapperCommand(args []string) string {
	runner := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runner == "" {
		runner = "bin/coding-ethos-run"
	}

	parts := make([]string, 0, len(args)+wrapperBaseArgc)
	parts = append(parts, shellQuote(runner), "policy-git")

	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func rewriteGitCommandChain(command string) (string, bool, bool) {
	tokens, parseOK := shellControlFieldsOK(command)
	if !parseOK {
		return "", false, false
	}
	if len(tokens) == 0 {
		return "", false, true
	}

	rewritten := make([]string, 0, len(tokens))
	rewrite := false

	for index := 0; index < len(tokens); {
		if isShellControlToken(tokens[index]) {
			rewritten = append(rewritten, tokens[index])
			index++

			continue
		}

		start := index
		for index < len(tokens) && !isShellControlToken(tokens[index]) {
			index++
		}

		segment := tokens[start:index]

		segmentRewrite, segmentOK := rewriteGitSegment(segment)
		if !segmentOK {
			return "", false, false
		}

		if segmentRewrite != "" {
			rewritten = append(rewritten, segmentRewrite)
			rewrite = true

			continue
		}

		rewritten = appendQuotedTokens(rewritten, segment)
	}

	return strings.Join(rewritten, " "), rewrite, true
}

func managedGitCommandChain(command string) bool {
	tokens, parseOK := shellControlFieldsOK(command)
	if !parseOK {
		return false
	}
	for index := 0; index < len(tokens); {
		if isShellControlToken(tokens[index]) {
			index++

			continue
		}

		start := index
		for index < len(tokens) && !isShellControlToken(tokens[index]) {
			index++
		}

		segment := tokens[start:index]
		if managedGitSegment(segment) {
			return true
		}
	}

	return false
}

func rewriteGitSegment(segment []string) (string, bool) {
	if len(segment) == 0 {
		return "", true
	}

	if managedGitSegment(segment) {
		return "", true
	}

	commandSegment := trimLeadingEnvAssignments(segment)
	if len(commandSegment) == 0 {
		return "", true
	}

	if filepath.Base(commandSegment[0]) == "coding-ethos-run" {
		return "", isTrustedRunnerCommand(commandSegment[0])
	}

	if managedGitWrapperImpersonation(commandSegment[0]) {
		return "", false
	}

	if commandSegment[0] == tokenGit {
		args, redirections := splitShellRedirections(commandSegment[1:])
		command := wrapperCommand(args)
		if len(redirections) > 0 {
			command += " " + strings.Join(redirections, " ")
		}

		return command, true
	}

	if segmentMentionsUnmanagedGit(commandSegment) {
		return "", false
	}

	return "", true
}

func managedGitSegment(segment []string) bool {
	if len(segment) == 0 {
		return false
	}

	segment = trimLeadingEnvAssignments(segment)
	if len(segment) == 0 {
		return false
	}

	command := segment[0]
	commandBase := filepath.Base(command)
	if commandBase == "coding-ethos-run" {
		return isTrustedRunnerCommand(command) &&
			len(segment) > 1 &&
			segment[1] == "policy-git"
	}

	if commandBase == "coding-ethos-git" {
		return isTrustedHookBinaryCommand(command)
	}

	return commandBase == "git" && isTrustedHookBinaryCommand(command)
}

func trimLeadingEnvAssignments(segment []string) []string {
	for len(segment) > 0 && isShellEnvAssignment(segment[0]) {
		segment = segment[1:]
	}

	return segment
}

func isShellEnvAssignment(token string) bool {
	if strings.HasPrefix(token, "-") {
		return false
	}

	name, value, ok := strings.Cut(token, "=")
	if !ok || name == "" || value == "" {
		return false
	}

	for index, char := range name {
		if char == '_' {
			continue
		}
		if char >= 'A' && char <= 'Z' {
			continue
		}
		if char >= 'a' && char <= 'z' {
			continue
		}
		if index > 0 && char >= '0' && char <= '9' {
			continue
		}

		return false
	}

	return true
}

func managedGitWrapperImpersonation(command string) bool {
	base := filepath.Base(command)

	return strings.Contains(base, "coding-ethos-git") ||
		strings.Contains(base, "coding-ethos-run")
}

func isTrustedRunnerCommand(command string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(command))
	if cleaned == "bin/coding-ethos-run" {
		return true
	}

	for _, resolved := range resolvedRunnerCommandPaths(command) {
		for _, trusted := range trustedRunnerPaths() {
			if resolved == trusted {
				return true
			}
		}
	}

	return false
}

func trustedRunnerPaths() []string {
	candidates := []string{
		os.Getenv("CODING_ETHOS_RUN_GO_HOOK"),
		filepath.Join(os.Getenv("CODE_ETHOS_PRECOMMIT_ROOT"), "..", "bin", "coding-ethos-run"),
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

func resolvedRunnerCommandPaths(command string) []string {
	candidates := []string{}
	if filepath.IsAbs(command) {
		candidates = append(candidates, command)
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

func isTrustedHookBinaryCommand(command string) bool {
	cleaned := filepath.ToSlash(filepath.Clean(command))

	return strings.Contains(cleaned, ".git/coding-ethos-hooks/bin/")
}

func segmentMentionsUnmanagedGit(segment []string) bool {
	for _, token := range segment {
		if token == tokenGit || isGitPath(token) {
			return true
		}
	}

	return false
}

func appendQuotedTokens(rewritten []string, tokens []string) []string {
	for _, token := range tokens {
		if isShellRedirectionSyntax(token) {
			rewritten = append(rewritten, token)

			continue
		}

		rewritten = append(rewritten, shellQuote(token))
	}

	return rewritten
}

func isShellControlToken(token string) bool {
	switch token {
	case "&&", "||", ";", "|", "&":
		return true
	default:
		return false
	}
}

func commandReferencesUnmanagedGit(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}
	for _, command := range commands {
		if shellCommandIsGit(command) || shellCommandWrapsTool(command, tokenGit) {
			return true
		}
		for _, token := range command.Argv {
			if shellTokenIsGitTool(token) {
				return true
			}
		}
	}

	return false
}

func shellTokenIsGitTool(token string) bool {
	return filepath.Base(token) == tokenGit || isGitPath(token)
}

func isGitPath(token string) bool {
	switch token {
	case "/usr/bin/git", "/bin/git", "/usr/local/bin/git":
		return true
	}

	return strings.HasSuffix(token, "/git")
}

func evasiveGitShell(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}

	mentionsGit := false
	for _, parsed := range commands {
		if shellCommandIsGit(parsed) ||
			shellCommandWrapsTool(parsed, tokenGit) ||
			shellCommandArgReferencesTool(parsed, tokenGit) ||
			shellExecArgumentReferencesGit(parsed) ||
			pythonExecArgumentReferencesTool(parsed, tokenGit) {
			mentionsGit = true
			break
		}
	}
	if !mentionsGit {
		return false
	}

	for _, parsed := range commands {
		if parsed.IsFunctionDeclaration &&
			shellTokenIsGitTool(parsed.Name) {
			return true
		}
		if parsed.HasCommandSubstitution ||
			parsed.HasProcessSubstitution ||
			parsed.HasHeredoc ||
			parsed.HasSubshell ||
			shellCommandUsesPathOverride(parsed) {
			return true
		}
		name := shellCommandName(parsed)
		switch name {
		case "bash", "sh", "zsh", "dash":
			if shellExecArgumentReferencesGit(parsed) {
				return true
			}
		case "command", "env", "eval", "alias", "exec":
			return true
		default:
			if pythonExecArgumentReferencesTool(parsed, tokenGit) ||
				shellCommandArgMentions(parsed, "subprocess") ||
				shellCommandArgMentions(parsed, "os.system") ||
				shellCommandArgMentions(parsed, "os.popen") {
				return isPythonCommand(name)
			}
		}
	}

	return false
}

func shellCommandName(command shellparse.Command) string {
	if command.Name != "" {
		return filepath.Base(command.Name)
	}
	if len(command.Argv) == 0 {
		return ""
	}

	return filepath.Base(command.Argv[0])
}

func shellCommandIsGit(command shellparse.Command) bool {
	name := shellCommandName(command)

	return name == tokenGit || isGitPath(name)
}

func shellCommandWrapsTool(command shellparse.Command, tool string) bool {
	if len(command.Argv) < 2 {
		return false
	}
	switch shellCommandName(command) {
	case "command", "env":
		for _, arg := range command.Argv[1:] {
			if strings.Contains(arg, "=") || strings.HasPrefix(arg, "-") {
				continue
			}

			return filepath.Base(arg) == tool || isGitPath(arg)
		}
	}

	return false
}

func shellCommandUsesPathOverride(command shellparse.Command) bool {
	for _, assignment := range command.Assignments {
		if strings.HasPrefix(assignment, "PATH=") {
			return true
		}
	}
	if shellCommandName(command) == "env" {
		for _, arg := range command.Argv[1:] {
			if strings.HasPrefix(arg, "PATH=") {
				return true
			}
		}
	}

	return false
}

func shellCommandArgReferencesTool(command shellparse.Command, tool string) bool {
	for _, arg := range command.Argv {
		if filepath.Base(arg) == tool || isGitPath(arg) {
			return true
		}
	}

	return false
}

func shellCommandArgMentions(command shellparse.Command, needle string) bool {
	needle = strings.ToLower(needle)
	for _, arg := range command.Argv {
		if strings.Contains(strings.ToLower(arg), needle) {
			return true
		}
	}

	return false
}

func shellExecArgumentMentions(command shellparse.Command, needle string) bool {
	for index, arg := range command.Argv {
		if arg == "-c" && index+1 < len(command.Argv) {
			return strings.Contains(strings.ToLower(command.Argv[index+1]), needle)
		}
	}

	return false
}

func shellExecArgumentReferencesGit(command shellparse.Command) bool {
	for index, arg := range command.Argv {
		if arg != "-c" || index+1 >= len(command.Argv) {
			continue
		}
		referencesGit, err := parsedShellReferencesGit(command.Argv[index+1])
		if err != nil || referencesGit {
			return true
		}
	}

	return false
}

func parsedShellReferencesGit(command string) (bool, error) {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return false, err
	}
	for _, parsed := range commands {
		if shellCommandIsGit(parsed) ||
			shellCommandWrapsTool(parsed, tokenGit) ||
			shellCommandArgReferencesTool(parsed, tokenGit) {
			return true, nil
		}
	}

	return false, nil
}

func pythonExecArgumentReferencesTool(command shellparse.Command, tool string) bool {
	for index, arg := range command.Argv {
		if arg != "-c" || index+1 >= len(command.Argv) {
			continue
		}
		for _, token := range pythonLikeTokens(command.Argv[index+1]) {
			if filepath.Base(token) == tool || isGitPath(token) {
				return true
			}
		}
	}

	return false
}

func pythonLikeTokens(source string) []string {
	return strings.FieldsFunc(source, func(char rune) bool {
		switch char {
		case ' ', '\t', '\n', '\r', '\'', '"', '[', ']', '(', ')', ',', ';':
			return true
		default:
			return false
		}
	})
}

func gitWrapperBlockDecision(bundle policy.Bundle, reason string) policy.Decision {
	return routeBlockDecision(bundle, gitWrapperPolicyID, reason)
}

func routeBlockDecision(
	bundle policy.Bundle,
	policyID string,
	reason string,
) policy.Decision {
	if policyID == "" {
		policyID = gitWrapperPolicyID
	}

	policyDef, ok := bundle.Policies[gitWrapperPolicyID]
	if policyID != gitWrapperPolicyID {
		policyDef, ok = bundle.Policies[policyID]
	}
	if !ok {
		policyDef = policy.Policy{
			ID:              policyID,
			Category:        "tooling",
			DefaultSeverity: "block",
			Message:         reason,
			SupportedModes:  []string{modeBlock},
			DefenseLayers:   policy.GitDefenseLayers("block", "wrapper", "block", "", ""),
		}
	}

	decision := policy.NewDecision(modeBlock, policyDef)
	decision.Severity = modeBlock
	decision.Message = reason
	if policyDef.Suggestion != "" {
		decision.Suggestion = policyDef.Suggestion
	} else {
		decision.Suggestion = gitWrapperUseManagedSuggestion
	}

	return decision
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellControlFieldsOK(command string) ([]string, bool) {
	fields, err := shellparse.ControlFields(command)

	return fields, err == nil
}

func splitShellRedirections(tokens []string) ([]string, []string) {
	args := make([]string, 0, len(tokens))
	redirections := []string{}

	for index := 0; index < len(tokens); index++ {
		token := tokens[index]
		if isShellRedirectionOperator(token) && index+1 < len(tokens) {
			redirections = append(
				redirections,
				token+" "+shellQuote(tokens[index+1]),
			)
			index++

			continue
		}

		if isShellRedirectionSyntax(token) {
			redirections = append(redirections, token)

			continue
		}

		args = append(args, token)
	}

	return args, redirections
}

func isShellRedirectionSyntax(token string) bool {
	return isShellRedirectionOperator(token) || isFusedShellRedirection(token)
}

func isShellRedirectionOperator(token string) bool {
	operator := tokenWithoutLeadingFileDescriptor(token)

	switch operator {
	case "<", ">", "<<", ">>", "<>", "<&", ">&", "&>", "&>>":
		return true
	default:
		return false
	}
}

func isFusedShellRedirection(token string) bool {
	operator := tokenWithoutLeadingFileDescriptor(token)

	return strings.HasPrefix(operator, "<") ||
		strings.HasPrefix(operator, ">") ||
		strings.HasPrefix(operator, "&>")
}

func tokenWithoutLeadingFileDescriptor(token string) string {
	for index, char := range token {
		if char < '0' || char > '9' {
			return token[index:]
		}
	}

	return token
}
