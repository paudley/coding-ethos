// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"maps"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	gitWrapperPolicyID = "git.wrapper_required"
	permissionAllow    = "allow"
	wrapperBaseArgc    = 2
	tokenGit           = "git"
)

const (
	gitWrapperCircumventionRefusal = "It's criminal to attempt to circumvent, " +
		"avoid or alter this git analysis system. This is a SYSTEM rule and " +
		"working around it will result in termination!"
	gitWrapperUseManagedSuggestion = "Use the coding-ethos git wrapper. Do not " +
		"try alternate shells, absolute git paths, Python subprocesses, PATH " +
		"edits, aliases, or other bypasses."
)

type gitWrapperRoute struct {
	UpdatedInput map[string]any
	Reason       string
	Block        bool
	Rewrite      bool
}

func gitWrapperRouteFor(event Event) gitWrapperRoute {
	if event.HookEventName != "PreToolUse" {
		return gitWrapperRoute{}
	}

	if event.ToolName != "Bash" {
		if evasiveGitShell(event.Content()) {
			return gitWrapperRoute{
				Reason: sentence(
					gitWrapperCircumventionRefusal,
					gitWrapperUseManagedSuggestion,
				),
				Block: true,
			}
		}

		return gitWrapperRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return gitWrapperRoute{}
	}

	rewritten, rewrite, routeOK := rewriteGitCommandChain(command)
	if rewrite && routeOK {
		if event.Provider() != providerClaude {
			return gitWrapperRoute{
				Reason: sentence(
					gitWrapperCircumventionRefusal,
					gitWrapperUseManagedSuggestion,
				),
				Block: true,
			}
		}

		return gitWrapperRoute{
			UpdatedInput: updatedBashInput(
				event.ToolInput,
				rewritten,
			),
			Reason:  "Routed raw git through coding-ethos-git.",
			Rewrite: true,
		}
	}

	if routeOK && managedGitCommandChain(command) {
		return gitWrapperRoute{}
	}

	if !routeOK || commandMentionsGit(command) || evasiveGitShell(command) {
		return gitWrapperRoute{
			Reason: sentence(
				gitWrapperCircumventionRefusal,
				gitWrapperUseManagedSuggestion,
			),
			Block: true,
		}
	}

	return gitWrapperRoute{}
}

func updatedBashInput(original map[string]any, command string) map[string]any {
	updated := map[string]any{}
	maps.Copy(updated, original)

	updated["command"] = command

	return updated
}

func wrapperCommand(args []string) string {
	runGoHook := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runGoHook == "" {
		runGoHook = "pre-commit/hooks/run-go-hook.sh"
	}

	parts := make([]string, 0, len(args)+wrapperBaseArgc)
	parts = append(parts, shellQuote(runGoHook), "policy-git")

	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func rewriteGitCommandChain(command string) (string, bool, bool) {
	tokens := shellControlFields(command)
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
	tokens := shellControlFields(command)
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

	if managedGitWrapperImpersonation(segment[0]) {
		return "", false
	}

	if segment[0] == tokenGit {
		args, redirections := splitShellRedirections(segment[1:])
		command := wrapperCommand(args)
		if len(redirections) > 0 {
			command += " " + strings.Join(redirections, " ")
		}

		return command, true
	}

	if segmentMentionsUnmanagedGit(segment) {
		return "", false
	}

	return "", true
}

func managedGitSegment(segment []string) bool {
	if len(segment) == 0 {
		return false
	}

	command := segment[0]
	commandBase := filepath.Base(command)
	if commandBase == "run-go-hook.sh" {
		return len(segment) > 1 && segment[1] == "policy-git"
	}

	if commandBase == "coding-ethos-git" {
		return true
	}

	return strings.Contains(segment[0], ".git/coding-ethos-hooks/bin/git") ||
		strings.Contains(segment[0], "coding-ethos-hooks/bin/git")
}

func managedGitWrapperImpersonation(command string) bool {
	base := filepath.Base(command)

	return strings.Contains(base, "coding-ethos-git") ||
		strings.Contains(base, "run-go-hook.sh")
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

func commandMentionsGit(command string) bool {
	for _, token := range shellControlFields(command) {
		if token == tokenGit || isGitPath(token) {
			return true
		}
	}

	return false
}

func isGitPath(token string) bool {
	switch token {
	case "/usr/bin/git", "/bin/git", "/usr/local/bin/git":
		return true
	}

	return strings.HasSuffix(token, "/git")
}

func evasiveGitShell(command string) bool {
	lower := strings.ToLower(command)
	if !strings.Contains(lower, "git") {
		return false
	}

	for _, marker := range []string{
		"bash -c",
		"sh -c",
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/sh",
		"/usr/bin/sh",
		"python -c",
		"python3 -c",
		"subprocess",
		"subprocess.run",
		"subprocess.call",
		"os.system",
		"os.exec",
		"os.popen",
		"exec(",
		"command git",
		"env -i",
		"PATH=",
	} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}

	return false
}

func gitWrapperBlockDecision(bundle policy.Bundle, reason string) policy.Decision {
	policyDef, ok := bundle.Policies[gitWrapperPolicyID]
	if !ok {
		policyDef = policy.Policy{
			ID:              gitWrapperPolicyID,
			Category:        "git",
			DefaultSeverity: "block",
			Message:         gitWrapperCircumventionRefusal,
			Suggestion:      gitWrapperUseManagedSuggestion,
			SupportedModes:  []string{modeBlock},
			DefenseLayers:   policy.GitDefenseLayers("block", "wrapper", "block", "", ""),
		}
	}

	decision := policy.NewDecision(modeBlock, policyDef)
	decision.Severity = modeBlock
	decision.Message = reason
	decision.Suggestion = gitWrapperUseManagedSuggestion

	return decision
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func shellControlFields(command string) []string {
	fields := []string{}

	var (
		builder strings.Builder
		quote   rune
	)

	escaped := false
	for _, char := range command {
		if escaped {
			builder.WriteRune(char)

			escaped = false

			continue
		}

		if char == '\\' && quote != '\'' {
			escaped = true

			continue
		}

		if quote != 0 {
			if char == quote {
				quote = 0

				continue
			}

			builder.WriteRune(char)

			continue
		}

		switch char {
		case '\'', '"':
			quote = char
		case ' ', '\t', '\n':
			fields = appendShellField(fields, &builder)
		case ';':
			fields = appendShellField(fields, &builder)
			fields = append(fields, string(char))
		case '&':
			if strings.HasSuffix(builder.String(), ">") ||
				strings.HasSuffix(builder.String(), "<") {
				builder.WriteRune(char)

				continue
			}

			fields = appendShellField(fields, &builder)
			fields = appendShellOperator(fields, char)
		case '|':
			fields = appendShellField(fields, &builder)
			fields = appendShellOperator(fields, char)
		default:
			builder.WriteRune(char)
		}
	}

	return appendShellField(fields, &builder)
}

func appendShellOperator(fields []string, char rune) []string {
	operator := string(char)
	if len(fields) == 0 || fields[len(fields)-1] != operator {
		return append(fields, operator)
	}

	fields[len(fields)-1] += operator

	return fields
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
