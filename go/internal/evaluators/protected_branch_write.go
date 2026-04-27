// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"os/exec"
	"regexp"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func bashModifyingPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`\s>\s*[^&]`),
		regexp.MustCompile(`\s>>\s*`),
		regexp.MustCompile(`\btee\b`),
		regexp.MustCompile(`\btouch\b`),
		regexp.MustCompile(`\bmkdir\b`),
		regexp.MustCompile(`\bmv\b`),
		regexp.MustCompile(`\bcp\b`),
		regexp.MustCompile(`\bsed\s+-i`),
		regexp.MustCompile(`\bgit\s+mv\b`),
		regexp.MustCompile(`\bgit\s+add\b`),
		regexp.MustCompile(`\bgit\s+stash\b`),
	}
}

func EvaluateProtectedBranchWrite(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if !modifiesFiles(context) {
		return nil, nil
	}

	if allFilesExemptFromProtectedBranch(context.Files, context.EvaluatorOptions) {
		return nil, nil
	}

	branch, ok := currentBranch(context.Cwd)
	if !ok || !isProtectedBranch(branch, context.EvaluatorOptions) {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)

	decision.Evidence = map[string]any{
		"branch": branch,
		"tool":   context.Tool,
	}
	if context.Command != "" {
		decision.Evidence["command"] = context.Command
	}

	if len(context.Files) > 0 {
		decision.Evidence["files"] = append([]string(nil), context.Files...)
	}

	return []policy.Decision{decision}, nil
}

func modifiesFiles(context Context) bool {
	switch context.Tool {
	case "Write", "Edit", "MultiEdit":
		return len(context.Files) > 0
	case "Bash":
		return bashCommandModifiesFiles(context.Command)
	default:
		return false
	}
}

func bashCommandModifiesFiles(command string) bool {
	for _, pattern := range bashModifyingPatterns() {
		if pattern.MatchString(command) {
			return true
		}
	}

	return false
}

func allFilesExemptFromProtectedBranch(files []string, options map[string]any) bool {
	if len(files) == 0 {
		return false
	}

	for _, file := range files {
		if !protectedBranchFileExempt(file, options) {
			return false
		}
	}

	return true
}

func protectedBranchFileExempt(file string, options map[string]any) bool {
	normalized := strings.TrimPrefix(file, "./")
	for _, prefix := range protectedBranchExemptPathPrefixes(options) {
		if strings.Contains(normalized, "/"+prefix) ||
			strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	return false
}

func protectedBranchExemptPathPrefixes(options map[string]any) []string {
	return stringSliceOption(options, "exempt_path_prefixes", []string{
		".claude/",
		"docs/plans/",
	})
}

func currentBranch(cwd string) (string, bool) {
	cmd := exec.CommandContext(context.Background(), "git", "branch", "--show-current")
	if cwd != "" {
		cmd.Dir = cwd
	}

	output, err := cmd.Output()
	if err != nil {
		return "", false
	}

	return strings.TrimSpace(string(output)), true
}

func isProtectedBranch(branch string, options map[string]any) bool {
	branches := stringSet(stringSliceOption(
		options,
		"branches",
		[]string{"main", "master"},
	))

	return branches[branch]
}
