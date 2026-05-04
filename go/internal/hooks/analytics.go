// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hooks

import (
	"regexp"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

var (
	shellQuotedStringPattern = regexp.MustCompile(`'[^']*'|"[^"]*"`)
	shellNumberPattern       = regexp.MustCompile(`\b\d+\b`)
	shellWhitespacePattern   = regexp.MustCompile(`\s+`)
)

type hookTraceAnalytics struct {
	OperationKind     string
	TargetKind        string
	RiskCategory      string
	CommandShapeHash  string
	TargetSetHash     string
	NormalizedTargets []string
}

func traceAnalytics(event Event, result Result) hookTraceAnalytics {
	targets := normalizedTargets(event.Files())
	command := event.Command()

	return hookTraceAnalytics{
		OperationKind:     operationKind(event.ToolName, command),
		TargetKind:        targetKind(targets, command),
		RiskCategory:      riskCategory(result.Decisions, result.Status),
		CommandShapeHash:  commandShapeHash(command),
		TargetSetHash:     targetSetHash(targets),
		NormalizedTargets: targets,
	}
}

func operationKind(toolName string, command string) string {
	tool := strings.ToLower(strings.TrimSpace(toolName))
	commandText := strings.ToLower(strings.TrimSpace(command))
	switch tool {
	case "bash", "shell":
		return shellOperationKind(commandText)
	case "write", "edit", "multiedit", "update", "notebookedit":
		return "file_write"
	case "read", "grep", "glob", "ls":
		return "file_read"
	case "":
		return "unknown"
	default:
		return strings.ReplaceAll(tool, " ", "_")
	}
}

func shellOperationKind(command string) string {
	for _, candidate := range []struct {
		needle string
		kind   string
	}{
		{"git add", "git_add"},
		{"git commit", "git_commit"},
		{"git diff", "git_diff"},
		{"git status", "git_status"},
		{"git reset", "git_reset"},
		{"git checkout", "git_checkout"},
		{"git switch", "git_switch"},
		{"git submodule", "git_submodule"},
		{"gh pr", "github_pr"},
		{"gh api", "github_api"},
		{"make check", "quality_check"},
		{"make build", "build"},
		{"ruff", "lint"},
		{"mypy", "type_check"},
		{"pyright", "type_check"},
		{"golangci-lint", "lint"},
		{"go test", "test"},
		{"pytest", "test"},
	} {
		if strings.Contains(command, candidate.needle) {
			return candidate.kind
		}
	}
	if command == "" {
		return "shell"
	}

	return "shell_command"
}

func targetKind(targets []string, command string) string {
	for _, target := range targets {
		normalized := strings.ToLower(target)
		switch {
		case isEnforcementPath(normalized):
			return "enforcement_point"
		case isAgentStatePath(normalized):
			return "agent_state"
		case isGeneratedConfigPath(normalized):
			return "generated_config"
		}
	}
	if strings.Contains(strings.ToLower(command), " git ") ||
		strings.HasPrefix(strings.ToLower(strings.TrimSpace(command)), "git ") {
		return "repo_state"
	}
	if len(targets) > 0 {
		return "source_file"
	}

	return "unknown"
}

func riskCategory(decisions []policy.Decision, status string) string {
	if len(decisions) == 0 {
		if status == statusBlocked {
			return "blocked_without_policy"
		}

		return "allowed"
	}
	for _, decision := range decisions {
		policyID := strings.ToLower(decision.PolicyID)
		message := strings.ToLower(decision.Message + " " + decision.Suggestion)
		switch {
		case strings.Contains(policyID, "bypass") ||
			strings.Contains(policyID, "wrapper") ||
			strings.Contains(message, "bypass") ||
			strings.Contains(message, "wrapper"):
			return "bypass"
		case strings.Contains(policyID, "forbidden_strings") ||
			strings.Contains(policyID, "malformed") ||
			strings.Contains(policyID, "shell"):
			return "unsafe_shell"
		case strings.Contains(policyID, "protected_path") ||
			strings.Contains(policyID, "enforcement"):
			return "protected_write"
		case strings.Contains(policyID, "large") ||
			strings.Contains(policyID, "growth") ||
			strings.Contains(policyID, "file_size"):
			return "large_file_growth"
		case strings.Contains(policyID, "lint") ||
			strings.Contains(policyID, "capture"):
			return "lint_capture"
		case decision.Decision == modeBlock || decision.Severity == modeBlock:
			return "policy_block"
		}
	}

	return "policy_signal"
}

func commandShapeHash(command string) string {
	shape := strings.TrimSpace(command)
	if shape == "" {
		return ""
	}
	shape = shellQuotedStringPattern.ReplaceAllString(shape, "?")
	shape = shellNumberPattern.ReplaceAllString(shape, "#")
	shape = shellWhitespacePattern.ReplaceAllString(shape, " ")

	return sha256Hex(shape)
}

func targetSetHash(targets []string) string {
	if len(targets) == 0 {
		return ""
	}

	return sha256Hex(strings.Join(targets, "\x00"))
}

func normalizedTargets(targets []string) []string {
	normalized := make([]string, 0, len(targets))
	for _, target := range targets {
		target = strings.TrimSpace(target)
		if target != "" {
			normalized = append(normalized, target)
		}
	}
	slices.Sort(normalized)

	return dedupeStrings(normalized)
}

func isEnforcementPath(path string) bool {
	for _, marker := range []string{
		".git/hooks",
		".git/coding-ethos-hooks",
		"coding-ethos-hooks",
		"policy-bundle.json",
		"pre-commit/hooks",
		"bin/coding-ethos",
	} {
		if strings.Contains(path, marker) {
			return true
		}
	}

	return false
}

func isAgentStatePath(path string) bool {
	if !(strings.Contains(path, "/.claude/") ||
		strings.Contains(path, "/.codex/") ||
		strings.Contains(path, "/.gemini/") ||
		strings.HasPrefix(path, ".claude/") ||
		strings.HasPrefix(path, ".codex/") ||
		strings.HasPrefix(path, ".gemini/")) {
		return false
	}

	return strings.Contains(path, "/memory/") ||
		strings.Contains(path, "/memories/") ||
		strings.Contains(path, "/plan/") ||
		strings.Contains(path, "/plans/") ||
		strings.HasSuffix(path, "/memory.md")
}

func isGeneratedConfigPath(path string) bool {
	for _, marker := range []string{
		".github/workflows/",
		".gitlab-ci.yml",
		"pyrightconfig.json",
		"mypy.ini",
		"ruff.toml",
		".pylintrc",
		".yamllint.yml",
		".bandit.yml",
		".sqlfluff",
		"tombi.toml",
		".golangci.yml",
	} {
		if strings.Contains(path, marker) {
			return true
		}
	}

	return false
}
