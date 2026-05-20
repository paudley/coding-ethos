// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellparse"
)

const (
	gitWrapperPolicyID = "git.wrapper_required"
	permissionAllow    = "allow"
	wrapperBaseArgc    = 2
	tokenGit           = "git"
	tokenCommand       = "command"
	tokenEnv           = "env"
	wrappedToolArgs    = 2
	cerunRunnerName    = "cerun"
	wrapperRunnerName  = "coding-ethos-run"
	wrapperRunnerPath  = "bin/coding-ethos-run"
	agentToolClaude    = "claude"
	agentToolCodex     = "codex"
	agentToolGemini    = "gemini"
)

const (
	agentShellCommandMinArgs        = 3
	agentShellCommandNameIndex      = 0
	agentShellRunnerSubcommandIndex = 1
)

const (
	gitWrapperCircumventionRefusal = severeViolationWarning
	gitWrapperUseManagedSuggestion = "Run ordinary git commands without bypass flags " +
		"or shell indirection; approved git operations are routed by the hook " +
		"automatically. Do not try alternate shells, absolute git paths, Python " +
		"subprocesses, PATH edits, aliases, or other bypasses."
)

func gitWrapperRouteFor(event Event) InspectionRoute {
	if event.HookEventName != eventPreToolUse {
		return InspectionRoute{}
	}

	if event.ToolName != toolBash {
		return InspectionRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return InspectionRoute{}
	}

	rewrittenCommand, rewrite, routeOK := rewriteGitCommandChain(command)
	if rewrite && routeOK {
		updatedCommand := agentShellRewriteCommand(command)
		if agentShellRewriteInspection(event) {
			updatedCommand = rewrittenCommand
		}

		return InspectionRoute{
			UpdatedInput: updatedBashInput(
				event.ToolInput,
				updatedCommand,
			),
			BlockPolicyID:      gitWrapperPolicyID,
			Reason:             "Routed shell command through the approved runner path.",
			RemediationCommand: cerunRemediation(command),
			Rewrite:            true,
		}
	}

	if routeOK && managedGitCommandChain(command) {
		return InspectionRoute{}
	}

	if routeOK && managedAgentShellCommand(command) {
		return InspectionRoute{}
	}

	if gitRouteBlocksCommand(command, routeOK) {
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

func agentShellRewriteInspection(event Event) bool {
	value, ok := event.ToolInput["agent_shell_rewrite"].(bool)

	return ok && value
}

func gitRouteBlocksCommand(command string, routeOK bool) bool {
	return !routeOK ||
		commandDelegatesGitWorkToAgent(command) ||
		commandHasDynamicExecutable(command) ||
		commandReferencesUnmanagedGit(command) ||
		evasiveGitShell(command)
}

func commandDelegatesGitWorkToAgent(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}

	return slices.ContainsFunc(commands, shellCommandDelegatesGitWorkToAgent)
}

func shellCommandDelegatesGitWorkToAgent(parsed shellparse.Command) bool {
	if !shellCommandIsAgentCLI(parsed) {
		return false
	}

	return textRequestsGitWork(strings.Join(parsed.Argv[1:], " "))
}

func shellCommandIsAgentCLI(parsed shellparse.Command) bool {
	switch shellCommandName(parsed) {
	case agentToolClaude, agentToolCodex, agentToolGemini:
		return true
	default:
		return false
	}
}

func textRequestsGitWork(text string) bool {
	normalized := strings.ToLower(text)

	if strings.Contains(normalized, "commit") ||
		strings.Contains(normalized, "push") ||
		strings.Contains(normalized, "amend") ||
		strings.Contains(normalized, "merge") ||
		strings.Contains(normalized, "rebase") ||
		strings.Contains(normalized, "checkout") ||
		strings.Contains(normalized, "reset") {
		return true
	}

	return strings.Contains(normalized, "git")
}

func commandHasDynamicExecutable(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}

	return slices.ContainsFunc(commands, shellCommandHasDynamicExecutable)
}

func shellCommandHasDynamicExecutable(parsed shellparse.Command) bool {
	if !parsed.HasDynamicExpansion || len(parsed.Argv) == 0 {
		return false
	}

	return shellWordHasExpansion(parsed.Argv[0])
}

func cerunRemediation(command string) string {
	return "cerun --rewrite -- " + shellQuote(command)
}

func agentShellRewriteCommand(command string) string {
	return strings.Join(
		[]string{
			shellQuote(runnerCommand()),
			"agent-shell",
			"--rewrite",
			"--",
			shellQuote(command),
		},
		" ",
	)
}

func shellWordHasExpansion(word string) bool {
	return strings.Contains(word, "$")
}

func updatedBashInput(original map[string]any, command string) map[string]any {
	updated := map[string]any{}
	maps.Copy(updated, original)

	updated[tokenCommand] = command

	return updated
}

func wrapperCommand(args []string) string {
	parts := make([]string, 0, len(args)+wrapperBaseArgc)
	parts = append(parts, shellQuote(runnerCommand()), "policy-git")

	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func runnerCommand() string {
	runner := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runner == "" {
		return wrapperRunnerPath
	}

	return runner
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

func managedAgentShellCommand(command string) bool {
	tokens, parseOK := shellControlFieldsOK(command)
	if !parseOK || len(tokens) < 3 {
		return false
	}

	if slices.ContainsFunc(tokens, isShellControlToken) {
		return false
	}

	return agentShellSegment(tokens)
}

func agentShellSegment(segment []string) bool {
	if len(segment) < agentShellCommandMinArgs {
		return false
	}

	switch filepath.Base(segment[agentShellCommandNameIndex]) {
	case cerunRunnerName:
		return cerunAgentShellSegment(segment)
	case wrapperRunnerName:
		return codingEthosAgentShellSegment(segment)
	default:
		return false
	}
}

func cerunAgentShellSegment(segment []string) bool {
	args := segment[1:]
	if len(args) == 0 {
		return false
	}

	switch args[0] {
	case "git", "python", "lint":
		return len(args) > 1
	case "--", "--rewrite", "--check":
		return agentShellArgsHaveCommand(args)
	default:
		return strings.HasPrefix(args[0], "--intent") && agentShellArgsHaveCommand(args)
	}
}

func codingEthosAgentShellSegment(segment []string) bool {
	if !isTrustedRunnerCommand(segment[agentShellCommandNameIndex]) ||
		segment[agentShellRunnerSubcommandIndex] != "agent-shell" {
		return false
	}

	return agentShellArgsHaveCommand(segment[2:])
}

func agentShellArgsHaveCommand(args []string) bool {
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "--":
			return index+1 < len(args)
		case arg == "--rewrite" || arg == "--check":
			continue
		case arg == "--intent":
			index++
			if index >= len(args) || strings.TrimSpace(args[index]) == "" {
				return false
			}
		case strings.HasPrefix(arg, "--intent="):
			if strings.TrimSpace(strings.TrimPrefix(arg, "--intent=")) == "" {
				return false
			}
		default:
			return false
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

	if agentShellSegment(segment) {
		return "", true
	}

	hasEnvPrelude := len(segment) > 0 && isShellEnvAssignment(segment[0])
	commandSegment := trimLeadingEnvAssignments(segment)

	if len(commandSegment) == 0 {
		return "", true
	}

	if agentShellSegment(commandSegment) {
		return "", !hasEnvPrelude
	}

	if filepath.Base(commandSegment[0]) == wrapperRunnerName {
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
		if !isShellEnvNameChar(index, char) {
			return false
		}
	}

	return true
}

func isShellEnvNameChar(index int, char rune) bool {
	return char == '_' ||
		(char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z') ||
		(index > 0 && char >= '0' && char <= '9')
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

	if filepath.IsAbs(command) {
		return slices.Contains(trustedRunnerPaths(), cleaned)
	}

	if !strings.Contains(command, "/") &&
		!strings.Contains(command, string(os.PathSeparator)) {
		return false
	}

	cwd := os.Getenv("INVOCATION_CWD")
	if cwd == "" {
		return false
	}

	abs := filepath.ToSlash(filepath.Join(cwd, command))

	return slices.Contains(trustedRunnerPaths(), abs)
}

func trustedRunnerPaths() []string {
	candidates := []string{
		os.Getenv("CODING_ETHOS_RUN_GO_HOOK"),
		filepath.Join(
			os.Getenv("CODE_ETHOS_PRECOMMIT_ROOT"),
			"..",
			"bin",
			"coding-ethos-run",
		),
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

func appendQuotedTokens(rewritten, tokens []string) []string {
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

		if slices.ContainsFunc(command.Argv, shellTokenIsGitTool) {
			return true
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

	if !shellCommandsMentionGit(commands) {
		return false
	}

	for _, parsed := range commands {
		if shellCommandIsEvasiveGitDefinition(parsed) ||
			shellCommandUsesEvasiveShellFeature(parsed) ||
			shellCommandIsEvasiveGitInvocation(parsed) {
			return true
		}
	}

	return false
}

func shellCommandsMentionGit(commands []shellparse.Command) bool {
	return slices.ContainsFunc(commands, shellCommandMentionsGit)
}

func shellCommandMentionsGit(parsed shellparse.Command) bool {
	return shellCommandIsGit(parsed) ||
		shellCommandWrapsTool(parsed, tokenGit) ||
		shellCommandArgReferencesTool(parsed, tokenGit) ||
		shellExecArgumentReferencesGit(parsed) ||
		pythonExecArgumentReferencesTool(parsed, tokenGit)
}

func shellCommandIsEvasiveGitDefinition(parsed shellparse.Command) bool {
	return parsed.IsFunctionDeclaration && shellTokenIsGitTool(parsed.Name)
}

func shellCommandUsesEvasiveShellFeature(parsed shellparse.Command) bool {
	return parsed.HasCommandSubstitution ||
		parsed.HasProcessSubstitution ||
		parsed.HasHeredoc ||
		parsed.HasSubshell ||
		shellCommandUsesPathOverride(parsed)
}

func shellCommandIsEvasiveGitInvocation(parsed shellparse.Command) bool {
	name := shellCommandName(parsed)
	switch name {
	case shellToolName, "sh", "zsh", "dash":
		return shellExecArgumentReferencesGit(parsed)
	case tokenCommand, tokenEnv, "eval", "alias", "exec":
		return true
	default:
		return pythonGitBypassCommand(name, parsed)
	}
}

func pythonGitBypassCommand(name string, parsed shellparse.Command) bool {
	if !isPythonCommand(name) {
		return false
	}

	return pythonExecArgumentReferencesTool(parsed, tokenGit) ||
		shellCommandArgMentions(parsed, "subprocess") ||
		shellCommandArgMentions(parsed, "os.system") ||
		shellCommandArgMentions(parsed, "os.popen")
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
	if len(command.Argv) < wrappedToolArgs {
		return false
	}

	switch shellCommandName(command) {
	case tokenCommand, tokenEnv:
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
		return false, fmt.Errorf("parse shell command for git references: %w", err)
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
			DefenseLayers: policy.GitDefenseLayers(
				"block",
				"wrapper",
				"block",
				"",
				"",
			),
		}
	}

	decision := policy.NewDecision(modeBlock, policyDef)
	decision.Severity = modeBlock

	decision.Message = reason

	switch {
	case policyDef.Suggestion != "":
		decision.Suggestion = policyDef.Suggestion
	case policyID == shellFileToolPolicyID:
		decision.Suggestion = "Use the provider Read, Edit, Write, or equivalent file " +
			"tool instead of Bash file access."
	default:
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
