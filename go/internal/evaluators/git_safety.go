// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockDecision              = "block"
	gitSubcommandArgc          = 2
	gitWorktreeOperationArgc   = 3
	gitWorktreeSubcommandIndex = 2
)

func EvaluateGitDestructiveCommand(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGit(argv) {
		return nil, nil
	}

	switch gitSubcommand(argv) {
	case "reset":
		if hasArg(argv, "--hard") {
			return blockGitDecision(policyDef, argv), nil
		}
	case "clean":
		if hasCleanForceDelete(argv) {
			return blockGitDecision(policyDef, argv), nil
		}
	case "checkout":
		if hasArg(argv, "--theirs") || hasArg(argv, "--ours") {
			return blockGitDecision(policyDef, argv), nil
		}
	case "restore":
		if hasArg(argv, "--") {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitMergeStrategyShortcut(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "merge") {
		return nil, nil
	}

	for idx, arg := range argv {
		if arg == "-X" && idx+1 < len(argv) && isTheirsOrOurs(argv[idx+1]) {
			return blockGitDecision(policyDef, argv), nil
		}

		if strings.HasPrefix(arg, "-X") &&
			isTheirsOrOurs(strings.TrimPrefix(arg, "-X")) {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitForcePushProtectedBranch(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "push") {
		return nil, nil
	}

	if !hasForcePush(argv) {
		return nil, nil
	}

	if hasProtectedBranchArg(argv) {
		return blockGitDecision(policyDef, argv), nil
	}

	return nil, nil
}

func EvaluateGitCheckoutProtectedBranch(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "checkout") && !isGitSubcommand(argv, "switch") {
		return nil, nil
	}

	for _, arg := range argv[2:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}

		if isProtectedBranchRef(arg) {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitDestructiveWorktree(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "worktree") || len(argv) < gitWorktreeOperationArgc {
		return nil, nil
	}

	switch argv[gitWorktreeSubcommandIndex] {
	case "prune":
		return blockGitDecision(policyDef, argv), nil
	case "remove", "move":
		if hasArg(argv, "--force") || hasShortFlag(argv, "f") {
			return blockGitDecision(policyDef, argv), nil
		}
	}

	return nil, nil
}

func EvaluateGitChangeDirFlag(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGit(argv) {
		return nil, nil
	}

	if slices.Contains(argv[1:], "-C") {
		return blockGitDecision(policyDef, argv), nil
	}

	return nil, nil
}

func EvaluateGitStashBlocked(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if isGitSubcommand(argv, "stash") {
		return blockGitDecision(policyDef, argv), nil
	}

	return nil, nil
}

func EvaluateGitCommitAttribution(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	argv := context.Argv
	if !isGitSubcommand(argv, "commit") {
		return nil, nil
	}

	messages, err := commitMessagesFromArgv(argv, context.Cwd)
	if err != nil {
		return nil, err
	}

	matches := forbiddenAttributionMatches(messages, attributionNames(context))
	if len(matches) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"argv":    append([]string(nil), argv...),
		"matches": matches,
	}

	return []policy.Decision{decision}, nil
}

func blockGitDecision(policyDef policy.Policy, argv []string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"argv": append([]string(nil), argv...),
	}

	return []policy.Decision{decision}
}

func commitMessagesFromArgv(argv []string, cwd string) ([]string, error) {
	args := gitArgsAfterSubcommand(argv)
	messages := []string{}

	for idx := 0; idx < len(args); idx++ {
		nextIdx, message, found, err := commitMessageArg(args, idx, cwd)
		if err != nil {
			return nil, err
		}

		idx = nextIdx

		if found {
			messages = append(messages, message)
		}
	}

	return messages, nil
}

func commitMessageArg(
	args []string,
	idx int,
	cwd string,
) (int, string, bool, error) {
	arg := args[idx]

	if arg == "-m" || arg == "--message" {
		return nextCommitMessageValue(args, idx)
	}

	if strings.HasPrefix(arg, "-m") && arg != "-m" {
		return idx, strings.TrimPrefix(arg, "-m"), true, nil
	}

	if value, found := strings.CutPrefix(arg, "--message="); found {
		return idx, value, true, nil
	}

	if arg == "-F" || arg == "--file" {
		return nextCommitMessageFile(args, idx, cwd)
	}

	if strings.HasPrefix(arg, "-F") && arg != "-F" {
		return commitMessageFileValue(idx, strings.TrimPrefix(arg, "-F"), cwd)
	}

	if value, found := strings.CutPrefix(arg, "--file="); found {
		return commitMessageFileValue(idx, value, cwd)
	}

	return idx, "", false, nil
}

func nextCommitMessageValue(args []string, idx int) (int, string, bool, error) {
	if idx+1 >= len(args) {
		return idx, "", false, nil
	}

	return idx + 1, args[idx+1], true, nil
}

func nextCommitMessageFile(
	args []string,
	idx int,
	cwd string,
) (int, string, bool, error) {
	if idx+1 >= len(args) {
		return idx, "", false, nil
	}

	message, err := readCommitMessageFile(args[idx+1], cwd)
	if err != nil {
		return idx, "", false, err
	}

	return idx + 1, message, true, nil
}

func commitMessageFileValue(
	idx int,
	path string,
	cwd string,
) (int, string, bool, error) {
	message, err := readCommitMessageFile(path, cwd)
	if err != nil {
		return idx, "", false, err
	}

	return idx, message, true, nil
}

func gitArgsAfterSubcommand(argv []string) []string {
	argv = stripLeadingAssignments(argv)
	for idx := 1; idx < len(argv); idx++ {
		arg := argv[idx]
		if arg == "--" {
			return nil
		}

		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			if idx+1 >= len(argv) {
				return nil
			}

			return argv[idx+1:]
		}

		if skipNextGitGlobalArg(arg) && idx+1 < len(argv) {
			idx++
		}
	}

	return nil
}

func readCommitMessageFile(path string, cwd string) (string, error) {
	if path == "" {
		return "", nil
	}

	if !filepath.IsAbs(path) && cwd != "" {
		path = filepath.Join(cwd, path)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read commit message file %s: %w", path, err)
	}

	return string(data), nil
}

func forbiddenAttributionMatches(messages []string, names []string) []string {
	patterns := attributionPatterns(names)
	matches := []string{}

	for _, message := range messages {
		for lineNo, line := range strings.Split(message, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "" || strings.HasPrefix(trimmed, "#") {
				continue
			}

			for _, pattern := range patterns {
				if match := pattern.FindString(trimmed); match != "" {
					matches = append(
						matches,
						fmt.Sprintf("line %d: %q in %s", lineNo+1, match, trimmed),
					)

					break
				}
			}
		}
	}

	return matches
}

func attributionPatterns(names []string) []*regexp.Regexp {
	quoted := make([]string, 0, len(names))
	for _, name := range names {
		if name != "" {
			quoted = append(quoted, regexp.QuoteMeta(name))
		}
	}

	if len(quoted) == 0 {
		quoted = append(quoted, regexp.QuoteMeta("ai"))
	}

	namesPattern := strings.Join(quoted, "|")

	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)co-?authored-?by:\s*(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)signed-?off-?by:\s*(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)generated\s+(by|with|using)\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)created\s+(by|with|using)\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)written\s+(by|with|using)\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)assisted\s+by\s+(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)\x{1F916}\s*(` + namesPattern + `)`),
		regexp.MustCompile(`(?i)(` + namesPattern + `)\s*\x{1F916}`),
		regexp.MustCompile(`\x{1F916}`),
	}
}

func attributionNames(context Context) []string {
	raw, ok := context.EvaluatorOptions["blocked_names"]
	if !ok {
		return defaultAttributionNames()
	}

	names := stringOptionList(raw)
	if len(names) == 0 {
		return defaultAttributionNames()
	}

	return names
}

func defaultAttributionNames() []string {
	return []string{
		"claude",
		"anthropic",
		"gpt",
		"chatgpt",
		"openai",
		"copilot",
		"github copilot",
		"ai assistant",
		"ai agent",
		"llm",
		"large language model",
		"gemini",
		"bard",
		"cursor",
	}
}

func stringOptionList(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return append([]string(nil), typed...)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok && text != "" {
				values = append(values, text)
			}
		}

		return values
	default:
		return nil
	}
}

func isGit(argv []string) bool {
	normalized := stripLeadingAssignments(argv)

	return len(normalized) > 0 && normalized[0] == "git"
}

func isGitSubcommand(argv []string, subcommand string) bool {
	return isGit(argv) && gitSubcommand(argv) == subcommand
}

func gitSubcommand(argv []string) string {
	argv = stripLeadingAssignments(argv)
	if len(argv) < gitSubcommandArgc {
		return ""
	}

	for idx := 1; idx < len(argv); idx++ {
		arg := argv[idx]
		if arg == "--" {
			return ""
		}

		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			return arg
		}

		if skipNextGitGlobalArg(arg) && idx+1 < len(argv) {
			idx++
		}
	}

	return ""
}

func stripLeadingAssignments(argv []string) []string {
	for len(argv) > 0 && isShellAssignment(argv[0]) {
		argv = argv[1:]
	}

	return argv
}

func isShellAssignment(arg string) bool {
	name, value, ok := strings.Cut(arg, "=")
	if !ok || name == "" || value == "" {
		return false
	}

	for _, char := range name {
		if char != '_' && (char < 'A' || char > 'Z') &&
			(char < 'a' || char > 'z') &&
			(char < '0' || char > '9') {
			return false
		}
	}

	return true
}

func skipNextGitGlobalArg(arg string) bool {
	switch arg {
	case "-C",
		"-c",
		"--git-dir",
		"--work-tree",
		"--namespace",
		"--exec-path",
		"--config-env":
		return true
	default:
		return false
	}
}

func hasArg(argv []string, target string) bool {
	return slices.Contains(argv, target)
}

func hasShortFlag(argv []string, flag string) bool {
	for _, arg := range argv {
		if !strings.HasPrefix(arg, "-") || strings.HasPrefix(arg, "--") {
			continue
		}

		if strings.Contains(strings.TrimPrefix(arg, "-"), flag) {
			return true
		}
	}

	return false
}

func hasCleanForceDelete(argv []string) bool {
	return hasArg(argv, "--force") && hasArg(argv, "-d") ||
		hasArg(argv, "-f") && hasArg(argv, "-d") ||
		hasShortFlag(argv, "f") && hasShortFlag(argv, "d")
}

func isTheirsOrOurs(value string) bool {
	return value == "theirs" || value == "ours"
}

func hasForcePush(argv []string) bool {
	return hasArg(argv, "--force") || hasArg(argv, "--force-with-lease") ||
		hasShortFlag(argv, "f")
}

func hasProtectedBranchArg(argv []string) bool {
	return slices.ContainsFunc(argv[2:], isProtectedBranchRef)
}

func isProtectedBranchRef(value string) bool {
	switch value {
	case "main",
		"master",
		"origin/main",
		"origin/master",
		"remotes/origin/main",
		"remotes/origin/master":
		return true
	default:
		return false
	}
}
