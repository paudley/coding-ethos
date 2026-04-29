// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	tokenRuff           = "ruff"
	tokenPolicyTool     = "policy-tool"
	ruffCapturePolicyID = "tool.ruff_capture_required"
)

func lintToolRouteFor(event Event) gitWrapperRoute {
	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
		return gitWrapperRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return gitWrapperRoute{}
	}

	rewritten, rewrite, routeOK := rewriteRuffCommandChain(command)
	if rewrite && routeOK {
		if event.Provider() != providerClaude {
			return gitWrapperRoute{
				BlockPolicyID: ruffCapturePolicyID,
				Reason:        ruffWrapperRequiredMessage(),
				Block:         true,
			}
		}

		return gitWrapperRoute{
			UpdatedInput: updatedBashInput(event.ToolInput, rewritten),
			Reason:       "Routed ruff through coding-ethos lint capture.",
			Rewrite:      true,
		}
	}

	if routeOK && managedRuffCommandChain(command) {
		return gitWrapperRoute{}
	}

	if !routeOK || evasiveRuffShell(command) {
		return gitWrapperRoute{
			BlockPolicyID: ruffCapturePolicyID,
			Reason:        ruffWrapperRequiredMessage(),
			Block:         true,
		}
	}

	return gitWrapperRoute{}
}

func ruffWrapperRequiredMessage() string {
	return "Ruff must run through the coding-ethos lint capture wrapper so " +
		"diagnostics are logged under .coding-ethos/lint-runs. Use the managed " +
		"ruff wrapper from the hook PATH instead of absolute ruff paths, " +
		"python -m ruff, uv run ruff, PATH edits, subprocesses, or shell bypasses."
}

func rewriteRuffCommandChain(command string) (string, bool, bool) {
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
		segmentRewrite, segmentOK := rewriteRuffSegment(segment)
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

func rewriteRuffSegment(segment []string) (string, bool) {
	if len(segment) == 0 {
		return "", true
	}
	if managedRuffSegment(segment) {
		return "", true
	}

	args, redirections := splitShellRedirections(segment)
	ruffArgs, ok := unmanagedRuffArgs(args)
	if ok {
		command := ruffCaptureCommand(ruffArgs)
		if len(redirections) > 0 {
			command += " " + strings.Join(redirections, " ")
		}

		return command, true
	}

	if segmentMentionsUnmanagedRuff(segment) {
		return "", false
	}

	return "", true
}

func unmanagedRuffArgs(segment []string) ([]string, bool) {
	if len(segment) == 0 {
		return nil, false
	}

	if isRuffCommand(segment[0]) {
		return append([]string(nil), segment[1:]...), true
	}

	if len(segment) >= 3 && isPythonCommand(segment[0]) &&
		segment[1] == "-m" && segment[2] == tokenRuff {
		return append([]string(nil), segment[3:]...), true
	}

	for index, token := range segment {
		if token == tokenRuff && index > 0 && filepath.Base(segment[0]) == "uv" {
			return append([]string(nil), segment[index+1:]...), true
		}
	}

	return nil, false
}

func ruffCaptureCommand(args []string) string {
	runGoHook := strings.TrimSpace(os.Getenv("CODING_ETHOS_RUN_GO_HOOK"))
	if runGoHook == "" {
		runGoHook = "pre-commit/hooks/run-go-hook.sh"
	}

	parts := []string{shellQuote(runGoHook), tokenPolicyTool, tokenRuff}
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func managedRuffCommandChain(command string) bool {
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

		if managedRuffSegment(tokens[start:index]) {
			return true
		}
	}

	return false
}

func managedRuffSegment(segment []string) bool {
	if len(segment) < 3 {
		return false
	}

	return filepath.Base(segment[0]) == "run-go-hook.sh" &&
		isTrustedRunGoHookCommand(segment[0]) &&
		segment[1] == tokenPolicyTool &&
		segment[2] == tokenRuff
}

func isRuffCommand(token string) bool {
	if token == tokenRuff {
		return true
	}

	base := filepath.Base(token)

	return base == tokenRuff && strings.Contains(filepath.ToSlash(token), "/")
}

func isPythonCommand(token string) bool {
	base := filepath.Base(token)

	return base == "python" || base == "python3"
}

func segmentMentionsUnmanagedRuff(segment []string) bool {
	for _, token := range segment {
		if isRuffCommand(token) {
			return true
		}
	}

	return false
}

func evasiveRuffShell(command string) bool {
	lower := strings.ToLower(command)
	if !strings.Contains(lower, "ruff") {
		return false
	}

	for _, marker := range []string{
		"bash -c",
		"sh -c",
		"/bin/bash",
		"/usr/bin/bash",
		"/bin/sh",
		"/usr/bin/sh",
		"subprocess",
		"subprocess.run",
		"subprocess.call",
		"os.system",
		"os.exec",
		"os.popen",
		"exec(",
		"env -i",
		"PATH=",
	} {
		if strings.Contains(lower, strings.ToLower(marker)) {
			return true
		}
	}

	return false
}
