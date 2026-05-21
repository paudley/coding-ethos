// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

const uvPythonStaticArgCount = 5

func pythonRuntimeRouteFor(event Event) InspectionRoute {
	if event.HookEventName != eventPreToolUse || event.ToolName != toolBash {
		return InspectionRoute{}
	}

	command := strings.TrimSpace(event.Command())
	if command == "" {
		return InspectionRoute{}
	}

	rewritten, rewrite := rewritePythonRuntimeCommandChain(command, event.Cwd)
	if !rewrite {
		return InspectionRoute{}
	}

	reason := "Python commands must run through the consumer repo environment: " +
		"`uv run --project <repo> python ...` when uv project evidence exists."

	return InspectionRoute{
		UpdatedInput: updatedBashInput(event.ToolInput, rewritten),
		Reason:       reason,
		Rewrite:      true,
	}
}

func rewritePythonRuntimeCommandChain(command, cwd string) (string, bool) {
	tokens, parseOK := shellControlFieldsOK(command)
	if !parseOK {
		return "", false
	}

	if len(tokens) == 0 {
		return "", false
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

		segmentRewrite := rewritePythonRuntimeSegment(segment, cwd)
		if segmentRewrite != "" {
			rewritten = append(rewritten, segmentRewrite)
			rewrite = true

			continue
		}

		rewritten = appendQuotedTokens(rewritten, segment)
	}

	return strings.Join(rewritten, " "), rewrite
}

func rewritePythonRuntimeSegment(segment []string, cwd string) string {
	if len(segment) == 0 {
		return ""
	}

	if !isPythonCommand(segment[0]) && !shellAssignmentForCommand(segment[0]) {
		return ""
	}

	args, redirections := splitShellRedirections(segment)
	assignments, argv := splitShellAssignments(args)

	if len(argv) == 0 || !isPythonCommand(argv[0]) {
		return ""
	}

	runtimeCommand, ok := pythonRuntimeCommand(cwd, argv[1:])
	if !ok {
		return ""
	}

	parts := make([]string, 0, len(assignments)+uvPythonStaticArgCount+len(argv))
	for _, assignment := range assignments {
		parts = append(parts, shellQuoteAssignment(assignment))
	}

	parts = append(parts, runtimeCommand...)
	if len(redirections) > 0 {
		parts = append(parts, redirections...)
	}

	return strings.Join(parts, " ")
}

func pythonRuntimeCommand(cwd string, args []string) ([]string, bool) {
	root := pythonRuntimeRoot(cwd)
	if root == "" {
		return nil, false
	}

	parts := make([]string, 0, uvPythonStaticArgCount+len(args))

	parts = append(parts, "uv", "run", "--project", shellQuote(root), "python")
	for _, arg := range args {
		parts = append(parts, shellQuote(arg))
	}

	return parts, true
}

func pythonRuntimeRoot(cwd string) string {
	cwd = normalizedPythonRuntimeCwd(cwd)

	if root := gitRootFromPath(cwd); root != "" {
		if gitPathIgnored(root, cwd) {
			return ""
		}

		if pythonRuntimeAvailable(root) {
			return root
		}

		return ""
	}

	for current := filepath.Clean(cwd); ; current = filepath.Dir(current) {
		if pythonRuntimeAvailable(current) {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
	}
}

func normalizedPythonRuntimeCwd(cwd string) string {
	if cwd == "" {
		cwd = strings.TrimSpace(os.Getenv("CODE_ETHOS_CONSUMER_ROOT"))
	}

	if cwd == "" {
		current, err := os.Getwd()
		if err != nil {
			return ""
		}

		cwd = current
	}

	if filepath.IsAbs(cwd) {
		return cwd
	}

	abs, err := filepath.Abs(cwd)
	if err != nil {
		return cwd
	}

	return abs
}

func pythonRuntimeAvailable(root string) bool {
	return pythonRepoUsesUV(root)
}

func gitRootFromPath(path string) string {
	for current := filepath.Clean(path); ; current = filepath.Dir(current) {
		if fileExists(filepath.Join(current, ".git")) ||
			dirExists(filepath.Join(current, ".git")) {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
	}
}

func pythonRepoUsesUV(root string) bool {
	return fileExists(filepath.Join(root, "uv.lock")) ||
		fileExists(filepath.Join(root, "uv.toml")) ||
		pyprojectDeclaresUV(root)
}

func pyprojectDeclaresUV(root string) bool {
	content, err := os.ReadFile(filepath.Join(root, "pyproject.toml"))
	if err != nil {
		return false
	}

	var config struct {
		Tool struct {
			UV map[string]any `toml:"uv"`
		} `toml:"tool"`
	}

	err = toml.Unmarshal(content, &config)
	if err != nil {
		return false
	}

	return config.Tool.UV != nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

func splitShellAssignments(tokens []string) ([]string, []string) {
	assignments := []string{}

	index := 0
	for index < len(tokens) && shellAssignmentForCommand(tokens[index]) {
		assignments = append(assignments, tokens[index])
		index++
	}

	return assignments, tokens[index:]
}

func shellAssignmentForCommand(token string) bool {
	name, _, found := strings.Cut(token, "=")
	if !found || name == "" {
		return false
	}

	return shellAssignmentName(name)
}

func shellAssignmentName(name string) bool {
	for index, char := range name {
		if !shellAssignmentNameChar(index, char) {
			return false
		}
	}

	return true
}

func shellAssignmentNameChar(index int, char rune) bool {
	if char == '_' ||
		(char >= 'A' && char <= 'Z') ||
		(char >= 'a' && char <= 'z') {
		return true
	}

	return index > 0 && char >= '0' && char <= '9'
}

func shellQuoteAssignment(token string) string {
	name, value, _ := strings.Cut(token, "=")

	return name + "=" + shellQuote(value)
}
