// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockDecision     = "block"
	gitSubcommandArgc = 2
)

func EvaluateGitCommitAttribution(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	messages, err := commitMessagesFromContext(context)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	matches := forbiddenAttributionMatches(messages, attributionNames(context))
	if len(matches) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"matches": matches,
	}
	if len(context.Argv) > 0 {
		decision.Evidence["argv"] = append([]string(nil), context.Argv...)
	}
	if len(context.Files) > 0 {
		decision.Evidence["files"] = append([]string(nil), context.Files...)
	}

	return []policy.Decision{decision}, nil
}

func EvaluateGitCommitLint(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	messages, err := commitMessagesFromContext(context)
	if err != nil {
		return nil, err
	}
	if len(messages) == 0 {
		return nil, nil
	}

	for _, message := range messages {
		errs := validateCommitMessageText(message, context.EvaluatorOptions)
		if len(errs) == 0 {
			continue
		}

		decision := policy.NewDecision(blockDecision, policyDef)
		decision.Evidence = map[string]any{
			"errors":  errs,
			"example": "fix(hooks): compile commit message checks",
		}

		return []policy.Decision{decision}, nil
	}

	return nil, nil
}

func blockGitDecision(policyDef policy.Policy, argv []string) []policy.Decision {
	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"argv": append([]string(nil), argv...),
	}

	return []policy.Decision{decision}
}

func commitMessagesFromContext(context Context) ([]string, error) {
	messages := []string{}
	if isGitSubcommand(context.Argv, "commit") {
		argvMessages, err := commitMessagesFromArgv(
			context.Argv,
			context.Cwd,
			context.Stdin,
		)
		if err != nil {
			return nil, err
		}

		messages = append(messages, argvMessages...)
	}

	if context.Scope == "commit-msg" {
		for _, file := range context.Files {
			message, err := readCommitMessageFile(file, context.Cwd, nil)
			if err != nil {
				return nil, err
			}
			if strings.TrimSpace(message) != "" {
				messages = append(messages, message)
			}
		}
	}

	return messages, nil
}

func commitMessagesFromArgv(argv []string, cwd string, stdin []byte) ([]string, error) {
	args := gitArgsAfterSubcommand(argv)
	messageFragments := []string{}

	for idx := 0; idx < len(args); idx++ {
		nextIdx, message, found, err := commitMessageArg(args, idx, cwd, stdin)
		if err != nil {
			return nil, err
		}

		idx = nextIdx

		if found {
			messageFragments = append(messageFragments, message)
		}
	}

	if len(messageFragments) == 0 {
		return nil, nil
	}

	return []string{strings.Join(messageFragments, "\n\n")}, nil
}

func validateCommitMessageText(message string, options map[string]any) []string {
	lines := commitMessageLines(message)
	if len(lines) == 0 {
		return []string{"commit message is empty"}
	}

	header := lines[0]
	for _, prefix := range stringSliceOption(options, "ignored_prefixes", defaultIgnoredCommitPrefixes()) {
		if strings.HasPrefix(header, prefix) {
			return nil
		}
	}

	errs := []string{}
	maxHeaderLength := intOption(options, "max_header_length", 150)
	if len(header) > maxHeaderLength {
		errs = append(errs, fmt.Sprintf("header must be <= %d characters", maxHeaderLength))
	}

	match := regexp.MustCompile(`^([a-z]+)\(([A-Za-z0-9_.-]+)\)!?: (.+)$`).
		FindStringSubmatch(header)
	if match == nil {
		return append(errs, "header must match: type(scope): subject")
	}

	allowed := stringSliceOption(options, "allowed_types", defaultCommitTypes())
	if !stringSet(allowed)[match[1]] {
		sort.Strings(allowed)
		errs = append(errs, "type must be one of: "+strings.Join(allowed, ", "))
	}

	if strings.TrimSpace(match[2]) == "" {
		errs = append(errs, "scope is required")
	}

	if strings.TrimSpace(match[3]) == "" {
		errs = append(errs, "subject is required")
	}

	if commitHasBodyOrFooter(lines) && len(lines) > 1 && strings.TrimSpace(lines[1]) != "" {
		errs = append(errs, "body/footer must be separated from header by a blank line")
	}

	return errs
}

func commitMessageLines(message string) []string {
	lines := []string{}
	for _, line := range strings.Split(message, "\n") {
		if !strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
			lines = append(lines, strings.TrimRight(line, " \t\r"))
		}
	}

	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}

	return lines
}

func commitHasBodyOrFooter(lines []string) bool {
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			return true
		}
	}

	return false
}

func defaultCommitTypes() []string {
	return []string{"chore", "docs", "feat", "fix", "perf", "refactor", "test"}
}

func defaultIgnoredCommitPrefixes() []string {
	return []string{"Merge ", "Revert ", "fixup! ", "squash! "}
}

func commitMessageArg(
	args []string,
	idx int,
	cwd string,
	stdin []byte,
) (int, string, bool, error) {
	arg := args[idx]

	if arg == "-m" || arg == "--message" {
		return nextCommitMessageValue(args, idx)
	}

	if strings.HasPrefix(arg, "-m") && arg != "-m" {
		return idx, normalizeCommitMessageValue(strings.TrimPrefix(arg, "-m")), true, nil
	}

	if value, found := strings.CutPrefix(arg, "--message="); found {
		return idx, normalizeCommitMessageValue(value), true, nil
	}

	if arg == "-F" || arg == "--file" {
		return nextCommitMessageFile(args, idx, cwd, stdin)
	}

	if strings.HasPrefix(arg, "-F") && arg != "-F" {
		return commitMessageFileValue(idx, strings.TrimPrefix(arg, "-F"), cwd, stdin)
	}

	if value, found := strings.CutPrefix(arg, "--file="); found {
		return commitMessageFileValue(idx, value, cwd, stdin)
	}

	return idx, "", false, nil
}

func nextCommitMessageValue(args []string, idx int) (int, string, bool, error) {
	if idx+1 >= len(args) {
		return idx, "", false, nil
	}

	return idx + 1, normalizeCommitMessageValue(args[idx+1]), true, nil
}

func normalizeCommitMessageValue(value string) string {
	message, ok := catHeredocCommandSubstitution(value)
	if ok {
		return message
	}

	return value
}

func catHeredocCommandSubstitution(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "$(") || !strings.HasSuffix(trimmed, ")") {
		return "", false
	}

	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "$("), ")"))
	if !strings.HasPrefix(inner, "cat") {
		return "", false
	}

	afterCat := strings.TrimSpace(strings.TrimPrefix(inner, "cat"))
	if !strings.HasPrefix(afterCat, "<<") {
		return "", false
	}

	rest := strings.TrimSpace(strings.TrimPrefix(afterCat, "<<"))
	newline := strings.IndexByte(rest, '\n')
	if newline < 0 {
		return "", false
	}

	delimiter := strings.Trim(strings.TrimSpace(rest[:newline]), `'"`)
	if delimiter == "" {
		return "", false
	}

	lines := strings.Split(rest[newline+1:], "\n")
	for index, line := range lines {
		if strings.TrimSuffix(line, "\r") == delimiter {
			return strings.Join(lines[:index], "\n"), true
		}
	}

	return "", false
}

func nextCommitMessageFile(
	args []string,
	idx int,
	cwd string,
	stdin []byte,
) (int, string, bool, error) {
	if idx+1 >= len(args) {
		return idx, "", false, nil
	}

	message, err := readCommitMessageFile(args[idx+1], cwd, stdin)
	if err != nil {
		return idx, "", false, err
	}

	return idx + 1, message, true, nil
}

func commitMessageFileValue(
	idx int,
	path string,
	cwd string,
	stdin []byte,
) (int, string, bool, error) {
	message, err := readCommitMessageFile(path, cwd, stdin)
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

func readCommitMessageFile(path string, cwd string, stdin []byte) (string, error) {
	if path == "" {
		return "", nil
	}
	if path == "-" {
		return string(stdin), nil
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

func protectedCheckoutTargets(argv []string) []string {
	if len(argv) < gitSubcommandArgc {
		return nil
	}

	switch gitSubcommand(argv) {
	case "checkout":
		return checkoutBranchTargets(argv[2:])
	case "switch":
		return switchBranchTargets(argv[2:])
	default:
		return nil
	}
}

func checkoutBranchTargets(args []string) []string {
	targets := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-b" || arg == "-B":
			if index+1 < len(args) {
				targets = append(targets, args[index+1])
				index++
			}
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				index++
			}
		case arg == "--branch" || arg == "--orphan":
			if index+1 < len(args) {
				targets = append(targets, args[index+1])
				index++
			}
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			targets = append(targets, arg)
		}
	}

	return targets
}

func switchBranchTargets(args []string) []string {
	targets := []string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		switch {
		case arg == "-c" || arg == "-C" || arg == "--create" || arg == "--force-create":
			if index+1 < len(args) {
				targets = append(targets, args[index+1])
				index++
			}
			if index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
				index++
			}
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			targets = append(targets, arg)
		}
	}

	return targets
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
