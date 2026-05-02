// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const tokenPolicyTool = "policy-tool"

func lintToolRouteFor(event Event) gitWrapperRoute {
	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
		return gitWrapperRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return gitWrapperRoute{}
	}

	rewritten, tool, rewrite, routeOK := rewriteLintToolCommandChain(command)
	if rewrite && routeOK {
		if event.Provider() != providerClaude {
			return gitWrapperRoute{
				BlockPolicyID: lintCapturePolicyID(tool.Name),
				Reason:        lintCaptureRequiredMessage(tool),
				Block:         true,
			}
		}

		return gitWrapperRoute{
			UpdatedInput: updatedBashInput(event.ToolInput, rewritten),
			Reason:       "Routed " + tool.Name + " through coding-ethos lint capture.",
			Rewrite:      true,
		}
	}

	if routeOK && managedLintToolCommandChain(command) {
		return gitWrapperRoute{}
	}

	if !routeOK || evasiveLintToolShell(command) {
		blockTool := tool
		if blockTool.Name == "" {
			blockTool = firstMentionedCapturedTool(command)
		}

		return gitWrapperRoute{
			BlockPolicyID: lintCapturePolicyID(blockTool.Name),
			Reason:        lintCaptureRequiredMessage(blockTool),
			Block:         true,
		}
	}

	return gitWrapperRoute{}
}

func lintCapturePolicyID(toolName string) string {
	if toolName == "" {
		return "tool.lint_capture_required"
	}

	return "tool." + strings.ReplaceAll(toolName, "-", "_") + "_capture_required"
}

func lintCaptureRequiredMessage(tool toolcatalog.CapturedTool) string {
	name := firstCaptureNonEmpty(tool.Description, tool.Name, "Lint tools")

	return name + " must run through the coding-ethos lint capture wrapper so " +
		"diagnostics are logged under .coding-ethos/lint-runs. Use the managed " +
		"tool wrapper from the hook PATH instead of absolute tool paths, " +
		"python -m, uv run, PATH edits, subprocesses, or shell bypasses."
}

func rewriteLintToolCommandChain(command string) (string, toolcatalog.CapturedTool, bool, bool) {
	tokens, parseOK := shellControlFieldsOK(command)
	if !parseOK {
		return "", toolcatalog.CapturedTool{}, false, false
	}
	if len(tokens) == 0 {
		return "", toolcatalog.CapturedTool{}, false, true
	}

	rewritten := make([]string, 0, len(tokens))
	var routedTool toolcatalog.CapturedTool
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
		segmentRewrite, segmentTool, segmentOK := rewriteLintToolSegment(segment)
		if !segmentOK {
			return "", segmentTool, false, false
		}
		if segmentRewrite != "" {
			rewritten = append(rewritten, segmentRewrite)
			routedTool = segmentTool
			rewrite = true

			continue
		}

		rewritten = appendQuotedTokens(rewritten, segment)
	}

	return strings.Join(rewritten, " "), routedTool, rewrite, true
}

func rewriteLintToolSegment(
	segment []string,
) (string, toolcatalog.CapturedTool, bool) {
	if len(segment) == 0 {
		return "", toolcatalog.CapturedTool{}, true
	}
	if managedLintToolSegment(segment) {
		return "", toolcatalog.CapturedTool{}, true
	}
	if segmentUsesPathOverride(segment) {
		if tool := segmentMentionsUnmanagedLintTool(segment); tool.Name != "" {
			return "", tool, false
		}
	}

	args, redirections := splitShellRedirections(segment)
	tool, toolArgs, ok := unmanagedLintToolArgs(args)
	if ok {
		command := lintCaptureCommand(tool.Name, toolArgs)
		if len(redirections) > 0 {
			command += " " + strings.Join(redirections, " ")
		}

		return command, tool, true
	}

	if tool := segmentMentionsUnmanagedLintTool(segment); tool.Name != "" {
		return "", tool, false
	}

	return "", toolcatalog.CapturedTool{}, true
}

func unmanagedLintToolArgs(segment []string) (toolcatalog.CapturedTool, []string, bool) {
	if len(segment) == 0 {
		return toolcatalog.CapturedTool{}, nil, false
	}

	if tool, ok := capturedToolForCommand(segment[0]); ok {
		return tool, append([]string(nil), segment[1:]...), true
	}

	if len(segment) >= 3 && isPythonCommand(segment[0]) && segment[1] == "-m" {
		if tool, ok := capturedToolForModule(segment[2]); ok && tool.PythonModule {
			return tool, append([]string(nil), segment[3:]...), true
		}
	}

	if filepath.Base(segment[0]) == "uv" {
		for index, token := range segment {
			if tool, ok := capturedToolForCommand(token); ok {
				return tool, append([]string(nil), segment[index+1:]...), true
			}
		}
	}

	if filepath.Base(segment[0]) == "command" && len(segment) > 1 {
		if tool, ok := capturedToolForCommand(segment[1]); ok {
			return tool, append([]string(nil), segment[2:]...), true
		}
	}

	if filepath.Base(segment[0]) == "env" {
		for index, token := range segment[1:] {
			if strings.Contains(token, "=") || strings.HasPrefix(token, "-") {
				continue
			}
			if tool, ok := capturedToolForCommand(token); ok {
				start := index + 2

				return tool, append([]string(nil), segment[start:]...), true
			}

			break
		}
	}

	return toolcatalog.CapturedTool{}, nil, false
}

func segmentUsesPathOverride(segment []string) bool {
	for _, token := range segment {
		if strings.HasPrefix(token, "PATH=") {
			return true
		}
	}

	return false
}

func lintCaptureCommand(toolName string, args []string) string {
	runner := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runner == "" {
		runner = "bin/coding-ethos-run"
	}

	parts := []string{shellQuote(runner), tokenPolicyTool, toolName}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func managedLintToolCommandChain(command string) bool {
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

		if managedLintToolSegment(tokens[start:index]) {
			return true
		}
	}

	return false
}

func firstCaptureNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func managedLintToolSegment(segment []string) bool {
	if len(segment) < 3 {
		return false
	}

	return filepath.Base(segment[0]) == "coding-ethos-run" &&
		isTrustedRunnerCommand(segment[0]) &&
		segment[1] == tokenPolicyTool &&
		capturedToolName(segment[2])
}

func capturedToolForCommand(token string) (toolcatalog.CapturedTool, bool) {
	base := filepath.Base(token)
	for _, tool := range toolcatalog.CapturedLintTools() {
		if base == tool.Name && (token == tool.Name || strings.Contains(filepath.ToSlash(token), "/")) {
			return tool, true
		}
	}

	return toolcatalog.CapturedTool{}, false
}

func capturedToolForModule(module string) (toolcatalog.CapturedTool, bool) {
	for _, tool := range toolcatalog.CapturedLintTools() {
		if slices.Contains(tool.ModuleNames, module) {
			return tool, true
		}
	}

	return toolcatalog.CapturedTool{}, false
}

func capturedToolName(name string) bool {
	_, found := toolcatalog.CapturedLintTool(name)

	return found
}

func firstMentionedCapturedTool(command string) toolcatalog.CapturedTool {
	tokens, parseOK := shellControlFieldsOK(command)
	if !parseOK {
		return toolcatalog.CapturedTool{}
	}
	for _, token := range tokens {
		if tool, ok := capturedToolForCommand(token); ok {
			return tool
		}
		if tool, ok := capturedToolForModule(token); ok {
			return tool
		}
	}

	return toolcatalog.CapturedTool{}
}

func isPythonCommand(token string) bool {
	base := filepath.Base(token)

	return base == "python" || base == "python3" || strings.HasPrefix(base, "python3.")
}

func segmentMentionsUnmanagedLintTool(segment []string) toolcatalog.CapturedTool {
	for _, token := range segment {
		if tool, ok := capturedToolForCommand(token); ok {
			return tool
		}
	}

	return toolcatalog.CapturedTool{}
}

func evasiveLintToolShell(command string) bool {
	commands, err := shellparse.Commands(command)
	if err != nil {
		return true
	}

	for _, parsed := range commands {
		mentionsLint := shellCommandIsLintTool(parsed)
		switch shellCommandName(parsed) {
		case "bash", "sh", "zsh", "dash":
			if shellExecArgumentMentionsCapturedTool(parsed) {
				return true
			}
		case "eval", "alias", "exec":
			if shellCommandArgMentionsCapturedTool(parsed) {
				return true
			}
		default:
			if isPythonCommand(shellCommandName(parsed)) &&
				(shellCommandArgMentions(parsed, "subprocess") ||
					shellCommandArgMentionsCapturedTool(parsed)) {
				return true
			}
		}
		if mentionsLint &&
			(parsed.HasCommandSubstitution ||
				parsed.HasProcessSubstitution ||
				parsed.HasHeredoc ||
				parsed.HasSubshell ||
				shellCommandUsesPathOverride(parsed)) {
			return true
		}
	}

	return false
}

func shellCommandIsLintTool(command shellparse.Command) bool {
	if _, ok := capturedToolForCommand(shellCommandName(command)); ok {
		return true
	}
	for _, arg := range command.Argv {
		if _, ok := capturedToolForCommand(arg); ok {
			return true
		}
		if _, ok := capturedToolForModule(arg); ok {
			return true
		}
	}

	return false
}

func shellExecArgumentMentionsCapturedTool(command shellparse.Command) bool {
	for index, arg := range command.Argv {
		if arg == "-c" && index+1 < len(command.Argv) {
			return commandStringMentionsCapturedTool(command.Argv[index+1])
		}
	}

	return false
}

func shellCommandArgMentionsCapturedTool(command shellparse.Command) bool {
	for _, arg := range command.Argv {
		if commandStringMentionsCapturedTool(arg) {
			return true
		}
	}

	return false
}

func commandStringMentionsCapturedTool(value string) bool {
	lower := strings.ToLower(value)
	for _, tool := range toolcatalog.CapturedLintTools() {
		if strings.Contains(lower, strings.ToLower(tool.Name)) {
			return true
		}
		for _, module := range tool.ModuleNames {
			if strings.Contains(lower, strings.ToLower(module)) {
				return true
			}
		}
	}

	return false
}
