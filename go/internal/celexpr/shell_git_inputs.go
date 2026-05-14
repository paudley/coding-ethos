// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package celexpr

import (
	"path"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/shellparse"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

const (
	gitCheckoutSubcommand  = "checkout"
	gitPushSubcommand      = "push"
	wrappedToolMinimumArgs = 2
)

func shellCommandInputs(command string) []ShellCommandInput {
	parsed, err := shellparse.Commands(command)
	if err != nil {
		return []ShellCommandInput{}
	}

	controlFields, err := shellparse.ControlFields(command)
	if err != nil {
		controlFields = nil
	}

	inputs := make([]ShellCommandInput, 0, len(parsed))
	for _, parsedCommand := range parsed {
		name := shellCommandName(parsedCommand)
		writeTargets := shellWriteTargets(parsedCommand)
		inputs = append(inputs, ShellCommandInput{
			Argv:                   append([]string(nil), parsedCommand.Argv...),
			Assignments:            append([]string(nil), parsedCommand.Assignments...),
			Redirects:              append([]string(nil), parsedCommand.Redirects...),
			WriteTargets:           writeTargets,
			Command:                parsedCommand.Command,
			Name:                   name,
			Column:                 int64(parsedCommand.Column),
			Line:                   int64(parsedCommand.Line),
			Background:             parsedCommand.Background,
			HasCommandSubstitution: parsedCommand.HasCommandSubstitution,
			HasDynamicExpansion:    parsedCommand.HasDynamicExpansion,
			HasHeredoc:             parsedCommand.HasHeredoc,
			HasInlineEnv:           len(parsedCommand.Assignments) > 0,
			HasProcessSubstitution: parsedCommand.HasProcessSubstitution,
			HasRedirects:           len(parsedCommand.Redirects) > 0,
			HasSubshell:            parsedCommand.HasSubshell,
			HasWriteTargets:        len(writeTargets) > 0,
			IsFunctionDeclaration:  parsedCommand.IsFunctionDeclaration,
			IsGitMutation:          shellCommandIsGitMutation(parsedCommand),
			IsGit:                  shellCommandIsGit(parsedCommand),
			IsLintTool:             shellCommandIsLintTool(parsedCommand),
			PipesToShell: shellCommandPipesToShell(
				parsedCommand,
				controlFields,
			),
			IsShellExec:      shellCommandIsShellExec(parsedCommand),
			UsesPathOverride: shellCommandUsesPathOverride(parsedCommand),
			WrapsGitMutation: shellCommandWrapsGitMutation(parsedCommand),
		})
	}

	return inputs
}

func shellWriteTargets(command shellparse.Command) []string {
	assignments := shellAssignmentMap(command.Assignments)
	targets := []string{}

	for _, redirect := range command.Redirects {
		if target, ok := redirectWriteTarget(redirect, assignments); ok {
			targets = append(targets, target)
		}
	}

	targets = append(targets, commandWriteTargets(command, assignments)...)

	return cleanStringSlice(targets)
}

func shellAssignmentMap(assignments []string) map[string]string {
	values := map[string]string{}

	for _, assignment := range assignments {
		name, value, ok := strings.Cut(assignment, "=")
		if !ok || name == "" {
			continue
		}

		values[name] = strings.Trim(value, `"'`)
	}

	return values
}

func redirectWriteTarget(
	redirect string,
	assignments map[string]string,
) (string, bool) {
	operatorIndex := redirectWriteOperatorIndex(redirect)
	if operatorIndex < 0 {
		return "", false
	}

	operator := redirect[operatorIndex:]
	for _, prefix := range []string{">>|", ">|", ">>", "<>", ">"} {
		if strings.HasPrefix(operator, prefix) {
			target := strings.TrimSpace(operator[len(prefix):])
			if target == "" || strings.HasPrefix(target, "&") {
				return "", false
			}

			return resolveShellTarget(target, assignments), true
		}
	}

	return "", false
}

func redirectWriteOperatorIndex(redirect string) int {
	for index, char := range redirect {
		if char == '>' {
			return index
		}

		if char == '<' && strings.HasPrefix(redirect[index:], "<>") {
			return index
		}
	}

	return -1
}

func commandWriteTargets(
	command shellparse.Command,
	assignments map[string]string,
) []string {
	if len(command.Argv) == 0 {
		return nil
	}

	switch shellCommandName(command) {
	case "tee":
		return teeWriteTargets(command.Argv[1:], assignments)
	case "cp", "mv":
		return copyMoveWriteTargets(command.Argv[1:], assignments)
	default:
		return nil
	}
}

func teeWriteTargets(args []string, assignments map[string]string) []string {
	targets := []string{}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		if arg == "--" {
			continue
		}

		if arg == "-a" || arg == "--append" ||
			arg == "-i" || arg == "--ignore-interrupts" {
			continue
		}

		if arg == "-p" || arg == "--output-error" {
			skipNext = true

			continue
		}

		if strings.HasPrefix(arg, "-") {
			continue
		}

		targets = append(targets, resolveShellTarget(arg, assignments))
	}

	return targets
}

func copyMoveWriteTargets(args []string, assignments map[string]string) []string {
	candidates := []string{}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		if arg == "--" {
			continue
		}

		if copyMoveOptionHasValue(arg) {
			skipNext = !strings.Contains(arg, "=")

			continue
		}

		if strings.HasPrefix(arg, "-") {
			continue
		}

		candidates = append(candidates, arg)
	}

	if len(candidates) == 0 {
		return nil
	}

	return []string{resolveShellTarget(candidates[len(candidates)-1], assignments)}
}

func copyMoveOptionHasValue(arg string) bool {
	return strings.HasPrefix(arg, "--target-directory") ||
		strings.HasPrefix(arg, "--backup") ||
		strings.HasPrefix(arg, "--suffix") ||
		arg == "-t" || arg == "-S"
}

func resolveShellTarget(target string, assignments map[string]string) string {
	cleaned := strings.Trim(target, `"'`)
	if variable, ok := shellVariableReference(cleaned); ok {
		if resolved := assignments[variable]; resolved != "" {
			return resolved
		}
	}

	return cleaned
}

func shellVariableReference(value string) (string, bool) {
	if strings.HasPrefix(value, "${") && strings.HasSuffix(value, "}") {
		return strings.TrimSuffix(strings.TrimPrefix(value, "${"), "}"), true
	}

	if strings.HasPrefix(value, "$") && len(value) > 1 {
		return strings.TrimPrefix(value, "$"), true
	}

	return "", false
}

func shellCommandName(command shellparse.Command) string {
	if command.Name != "" {
		return path.Base(command.Name)
	}

	if len(command.Argv) == 0 {
		return ""
	}

	return path.Base(command.Argv[0])
}

func shellCommandIsGit(command shellparse.Command) bool {
	return commandTokenMatchesTool(shellCommandName(command), gitCommandName) ||
		shellCommandWrappedTool(command, gitCommandName)
}

func shellCommandIsLintTool(command shellparse.Command) bool {
	if _, ok := toolcatalog.CapturedLintTool(shellCommandName(command)); ok {
		return true
	}

	for _, arg := range command.Argv {
		if _, ok := toolcatalog.CapturedLintTool(path.Base(arg)); ok {
			return true
		}
	}

	return false
}

func shellCommandIsShellExec(command shellparse.Command) bool {
	switch shellCommandName(command) {
	case "bash", "sh", "zsh", "dash":
		return true
	default:
		return false
	}
}

func shellCommandUsesPathOverride(command shellparse.Command) bool {
	for _, assignment := range command.Assignments {
		if strings.HasPrefix(assignment, "PATH=") {
			return true
		}
	}

	if len(command.Argv) > 0 && shellCommandName(command) == "env" {
		for _, arg := range command.Argv[1:] {
			if strings.HasPrefix(arg, "PATH=") {
				return true
			}
		}
	}

	return false
}

func shellCommandIsGitMutation(command shellparse.Command) bool {
	if !shellCommandIsGit(command) || len(command.Argv) < 2 {
		return false
	}

	switch command.Argv[1] {
	case "commit", gitPushSubcommand:
		return true
	default:
		return false
	}
}

func shellCommandWrapsGitMutation(command shellparse.Command) bool {
	if shellCommandName(command) != "timeout" {
		return false
	}

	for index, arg := range command.Argv {
		if path.Base(arg) != gitCommandName || index+1 >= len(command.Argv) {
			continue
		}

		switch command.Argv[index+1] {
		case "commit", gitPushSubcommand:
			return true
		}
	}

	return false
}

func shellCommandPipesToShell(
	command shellparse.Command,
	controlFields []string,
) bool {
	name := shellCommandName(command)
	switch name {
	case "curl", "wget":
	default:
		return false
	}

	for index, field := range controlFields {
		if path.Base(field) != name {
			continue
		}

	scan:
		for cursor := index + 1; cursor < len(controlFields)-1; cursor++ {
			switch controlFields[cursor] {
			case "|":
				if isShellInterpreter(controlFields[cursor+1]) {
					return true
				}
			case "&&", ";", "||":
				break scan
			}
		}
	}

	return false
}

func isShellInterpreter(value string) bool {
	switch path.Base(value) {
	case "bash", "sh", "zsh", "dash":
		return true
	default:
		return false
	}
}

func shellCommandWrappedTool(command shellparse.Command, tool string) bool {
	if len(command.Argv) < wrappedToolMinimumArgs {
		return false
	}

	switch shellCommandName(command) {
	case "command", "env":
		for _, arg := range command.Argv[1:] {
			if strings.Contains(arg, "=") {
				continue
			}

			return commandTokenMatchesTool(arg, tool)
		}
	}

	return false
}

func gitSubcommandIndex(argv []string) int {
	for idx := 1; idx < len(argv); idx++ {
		arg := argv[idx]
		if arg == "--" {
			return -1
		}

		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			return idx
		}

		if gitGlobalOptionHasValue(arg) && idx+1 < len(argv) {
			idx++
		}
	}

	return -1
}

func gitGlobalOptions(args []string) []string {
	options := []string{}

	for idx := 0; idx < len(args); idx++ {
		arg := args[idx]
		if arg == "" {
			continue
		}

		if !strings.HasPrefix(arg, "-") {
			break
		}

		options = append(options, arg)
		if gitGlobalOptionHasValue(arg) && idx+1 < len(args) {
			idx++
			options = append(options, args[idx])
		}
	}

	return options
}

func gitGlobalOptionHasValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}

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

func gitFlags(args []string) []string {
	flags := []string{}

	for _, arg := range args {
		if arg == "--" {
			break
		}

		if arg == "" || !strings.HasPrefix(arg, "-") {
			continue
		}

		flags = append(flags, arg)
		if strings.HasPrefix(arg, "--") {
			continue
		}

		for _, flag := range strings.TrimPrefix(arg, "-") {
			flags = append(flags, "-"+string(flag))
		}
	}

	return uniqueStrings(flags)
}

func gitTargets(args []string) []string {
	targets := []string{}

	skipNext := false
	for _, arg := range args {
		if skipNext {
			skipNext = false

			continue
		}

		if arg == "--" {
			continue
		}

		if gitArgOptionHasValue(arg) {
			skipNext = true

			continue
		}

		if strings.HasPrefix(arg, "-") {
			continue
		}

		targets = append(targets, arg)
	}

	return targets
}

func gitArgOptionHasValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}

	switch arg {
	case "-m", "-F", "-X", "-C", "-c", "-b", "-B", "--message", "--file",
		"--branch", "--orphan", "--strategy-option", "--strategy",
		"--pathspec-from-file":
		return true
	default:
		return false
	}
}

func gitCleanForceDelete(flags []string) bool {
	return (listContains(flags, "--force") || listContains(flags, "-f")) &&
		listContains(flags, "-d")
}

func gitBranchRewriteReset(subcommand string, args []string) bool {
	if subcommand != "reset" || listContains(args, "--") {
		return false
	}

	for _, arg := range args {
		if arg == "" || strings.HasPrefix(arg, "-") || arg == "HEAD" {
			continue
		}

		return true
	}

	return false
}

func gitForcedBranchMove(subcommand string, flags []string) bool {
	return (subcommand == gitCheckoutSubcommand && listContains(flags, "-B")) ||
		(subcommand == "branch" &&
			(listContains(flags, "-f") || listContains(flags, "--force")))
}

func gitHasForcePush(flags []string) bool {
	return listContains(flags, "--force") ||
		listContains(flags, "--force-with-lease") ||
		listContains(flags, "-f")
}

func gitForcePushProtectedBranch(argv, protectedBranches []string) bool {
	input := gitCommandInputWithoutDerived(argv)
	if input.Subcommand != gitPushSubcommand || !gitHasForcePush(input.Flags) {
		return false
	}

	for _, arg := range input.Args {
		if gitProtectedBranchRef(arg, protectedBranches) {
			return true
		}
	}

	return false
}

func gitCheckoutProtectedBranch(argv, protectedBranches []string) bool {
	input := gitCommandInputWithoutDerived(argv)
	switch input.Subcommand {
	case gitCheckoutSubcommand:
		return gitTargetsContainLocalProtected(
			checkoutBranchTargets(input.Args),
			protectedBranches,
		)
	case "switch":
		return gitTargetsContainLocalProtected(
			switchBranchTargets(input.Args),
			protectedBranches,
		)
	default:
		return false
	}
}

func gitTargetsContainLocalProtected(targets, protectedBranches []string) bool {
	for _, target := range targets {
		if gitLocalProtectedBranchRef(target, protectedBranches) {
			return true
		}
	}

	return false
}

func gitLocalProtectedBranchRef(value string, protectedBranches []string) bool {
	if value == "" {
		return false
	}

	if len(protectedBranches) == 0 {
		protectedBranches = []string{"main", "master"}
	}

	return slices.Contains(protectedBranches, value)
}

func gitCommandInputWithoutDerived(argv []string) GitCommandInput {
	normalized := stripLeadingAssignments(argv)
	if len(normalized) == 0 || !commandTokenMatchesTool(normalized[0], gitCommandName) {
		return GitCommandInput{}
	}

	subcommandIndex := gitSubcommandIndex(normalized)
	if subcommandIndex == -1 {
		return GitCommandInput{}
	}

	args := append([]string(nil), normalized[subcommandIndex+1:]...)

	return GitCommandInput{
		Args:       args,
		Flags:      gitFlags(args),
		IsGit:      true,
		Subcommand: normalized[subcommandIndex],
		Targets:    gitCommandTargets(normalized[subcommandIndex], args),
	}
}

func gitCommandTargets(subcommand string, args []string) []string {
	switch subcommand {
	case gitCheckoutSubcommand:
		return checkoutBranchTargets(args)
	case "switch":
		return switchBranchTargets(args)
	default:
		return gitTargets(args)
	}
}

func gitMergeStrategyShortcut(args []string) bool {
	for index, arg := range args {
		if arg == "-X" && index+1 < len(args) && isTheirsOrOurs(args[index+1]) {
			return true
		}

		if strings.HasPrefix(arg, "-X") &&
			isTheirsOrOurs(strings.TrimPrefix(arg, "-X")) {
			return true
		}
	}

	return false
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
				targets = append(targets, args[index+1])
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
				targets = append(targets, args[index+1])
			}
		case strings.HasPrefix(arg, "-"):
			continue
		default:
			targets = append(targets, arg)
		}
	}

	return targets
}

func gitProtectedBranchRef(value string, protectedBranches []string) bool {
	if value == "" {
		return false
	}

	if len(protectedBranches) == 0 {
		protectedBranches = []string{"main", "master"}
	}

	for _, branch := range protectedBranches {
		if value == branch ||
			value == "origin/"+branch ||
			value == "remotes/origin/"+branch {
			return true
		}
	}

	return false
}

func isTheirsOrOurs(value string) bool {
	return value == "theirs" || value == "ours"
}

func listContains(values []string, needle string) bool {
	return slices.Contains(values, needle)
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}

	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}

		seen[value] = true
		unique = append(unique, value)
	}

	return unique
}
