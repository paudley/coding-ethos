// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

func pythonRuntimeRouteFor(event Event) InspectionRoute {
	if event.HookEventName != "PreToolUse" || event.ToolName != "Bash" {
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
		"`uv run --project <repo> python ...` when a uv project exists, " +
		"otherwise `<repo>/.venv/bin/python ...` when a venv exists."

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
	if len(segment) == 0 || !isPythonCommand(segment[0]) {
		return ""
	}

	args, redirections := splitShellRedirections(segment)
	if len(args) == 0 || !isPythonCommand(args[0]) {
		return ""
	}

	root := pythonRuntimeRoot(cwd)
	if root == "" {
		return ""
	}

	parts := pythonRuntimeCommand(root, args[1:])
	if len(redirections) > 0 {
		parts = append(parts, redirections...)
	}

	return strings.Join(parts, " ")
}

func pythonRuntimeCommand(root string, args []string) []string {
	if pythonRepoUsesUV(root) {
		parts := []string{"uv", "run", "--project", shellQuote(root), "python"}
		for _, arg := range args {
			parts = append(parts, shellQuote(arg))
		}

		return parts
	}

	pythonPath := filepath.Join(root, ".venv", "bin", "python")
	if fileExecutable(pythonPath) {
		parts := []string{shellQuote(pythonPath)}
		for _, arg := range args {
			parts = append(parts, shellQuote(arg))
		}

		return parts
	}

	return nil
}

func pythonRuntimeRoot(cwd string) string {
	if cwd == "" {
		if root := strings.TrimSpace(os.Getenv("CODE_ETHOS_CONSUMER_ROOT")); root != "" {
			cwd = root
		}
	}

	if cwd == "" {
		var err error

		cwd, err = os.Getwd()
		if err != nil {
			return ""
		}
	}

	if !filepath.IsAbs(cwd) {
		abs, err := filepath.Abs(cwd)
		if err == nil {
			cwd = abs
		}
	}

	if root := gitRootFromPath(cwd); root != "" &&
		(pythonRepoUsesUV(root) || fileExecutable(filepath.Join(root, ".venv", "bin", "python"))) {
		return root
	}

	for current := filepath.Clean(cwd); ; current = filepath.Dir(current) {
		if pythonRepoUsesUV(current) ||
			fileExecutable(filepath.Join(current, ".venv", "bin", "python")) {
			return current
		}

		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
	}
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
		fileExists(filepath.Join(root, "pyproject.toml"))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir()
}

func dirExists(path string) bool {
	info, err := os.Stat(path)

	return err == nil && info.IsDir()
}

func fileExecutable(path string) bool {
	info, err := os.Stat(path)

	return err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0
}
