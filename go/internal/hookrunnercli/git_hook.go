// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
)

const (
	allZeroSHA              = "0000000000000000000000000000000000000000"
	emptyTreeSHA            = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	hookGroupChildEnv       = "CODE_ETHOS_HOOK_GROUP_CHILD"
	hookGroupResultPathEnv  = "CODE_ETHOS_HOOK_GROUP_RESULT_PATH"
	hookGroupResultFileMode = 0o600
)

func runGitHookCommand(cfg Config, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(
			os.Stderr,
			"Usage: coding-ethos-hook git-hook "+
				"<pre-commit|pre-push|commit-msg|validate> [args...]",
		)

		return 1
	}

	switch args[0] {
	case hookStagePreCommit:
		return runPreCommitHook(cfg, args[1:])
	case hookStagePrePush:
		return runPrePushHook(cfg, os.Stdin)
	case "commit-msg":
		fmt.Fprintln(
			os.Stderr,
			"FATAL: commit-msg is enforced by compiled policy preflight",
		)

		return 1
	case "validate":
		return validateGoHookRuntime()
	default:
		writef(os.Stderr, "FATAL: unknown git hook %q\n", args[0])

		return 1
	}
}

func runPreCommitHook(cfg Config, args []string) int {
	cfg.HookStage = hookStagePreCommit
	allFiles := slices.Contains(args, "--all-files")

	files, err := hookFilesForPreCommit(allFiles)
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if runFormatGroup(cfg, files, !allFiles) != 0 {
		return 1
	}

	exit := runNamedHookGroups(cfg, []string{
		"syntax",
		"docker",
		"workflow",
		"shell",
		"python-policy",
		"python-quality",
		"python-static",
		"docs",
		"security",
		"go",
	}, files)

	if exit != 0 {
		return exit
	}

	return runNamedHookGroups(cfg, []string{"ai"}, files)
}

func runPrePushHook(cfg Config, input io.Reader) int {
	cfg.HookStage = hookStagePrePush
	restoreRoot := useLocalRootForPrePush()

	defer restoreRoot()

	files, err := pushedFiles(input)
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	exit := runNamedHookGroups(cfg, []string{
		"python-policy",
		"python-quality",
		"python-static",
		"docs",
		"security",
		"shell",
		"docker",
		"workflow",
		"go",
	}, files)

	if exit != 0 {
		return exit
	}

	if slices.Contains(enabledHookGroupNames([]string{"ai"}), "ai") &&
		runGeminiCheck(cfg, []string{"--full-check"}) != 0 {
		return 1
	}

	return 0
}

func useLocalRootForPrePush() func() {
	localRoot := strings.TrimSpace(localRepoRoot())
	currentRoot := strings.TrimSpace(repoRoot())

	if localRoot == "" || localRoot == currentRoot {
		return func() {}
	}

	previousRoot, hadPreviousRoot := os.LookupEnv(consumerRootEnv)
	_ = os.Setenv(consumerRootEnv, localRoot)

	return func() {
		if hadPreviousRoot {
			_ = os.Setenv(consumerRootEnv, previousRoot)

			return
		}

		_ = os.Unsetenv(consumerRootEnv)
	}
}

func runNamedHookGroups(cfg Config, names, files []string) int {
	groups := canonicalHookGroups()
	selectedNames := enabledHookGroupNames(names)
	selectedGroups := make([]hookGroup, 0, len(selectedNames))

	for _, name := range selectedNames {
		group, ok := groups[name]
		if !ok {
			writef(os.Stderr, "FATAL: unknown hook group %q\n", name)

			return 1
		}

		if !group.matchesFiles(files) {
			continue
		}

		selectedGroups = append(selectedGroups, group)
	}

	return runHookGroupsInProcess(cfg, selectedGroups, files)
}

func runHookGroupsInProcess(cfg Config, groups []hookGroup, files []string) int {
	results := make([]hookGroupResult, len(groups))

	var groupWait sync.WaitGroup

	for idx, group := range groups {
		groupWait.Add(1)

		go func(pos int, grp hookGroup) {
			defer groupWait.Done()

			results[pos] = runHookGroupInProcess(cfg, grp, files)
		}(idx, group)
	}

	groupWait.Wait()

	exit := 0

	for _, result := range results {
		if result.ExitCode != 0 {
			exit = 1
		}

		if hookVerboseSuccessOutputEnabled() {
			writeLine(os.Stdout, formatHookExecutionSummary(
				[]hookGroupResult{result},
				selectedHookOutputFormat(),
			))
		}
	}

	if exit != 0 && !hookVerboseSuccessOutputEnabled() {
		writeLine(os.Stdout, formatHookExecutionSummary(
			results,
			selectedHookOutputFormat(),
		))
	}

	return exit
}

func runHookGroupInProcess(
	cfg Config,
	group hookGroup,
	files []string,
) hookGroupResult {
	start := time.Now()
	exit := 0
	commandResults := make([]hookCommandResult, 0, len(group.Commands))

	seqEnd := group.ParallelAfter
	if seqEnd > len(group.Commands) || seqEnd <= 0 {
		seqEnd = len(group.Commands)
	}

	for _, command := range group.Commands[:seqEnd] {
		commandStart := time.Now()

		commandExit := runFilteredHookCommand(cfg, command, files)
		if commandExit != 0 {
			exit = 1
		}

		commandResults = append(commandResults, hookCommandResult{
			Name:       command.Name,
			Status:     hookStatusForExitCode(commandExit),
			ExitCode:   commandExit,
			DurationMS: durationMilliseconds(commandStart),
		})
	}

	if exit != 0 || seqEnd >= len(group.Commands) {
		return hookGroupResult{
			Name:       group.Name,
			Status:     hookStatusForExitCode(exit),
			ExitCode:   exit,
			DurationMS: durationMilliseconds(start),
			Commands:   commandResults,
		}
	}

	parallelCommands := group.Commands[seqEnd:]
	parallelResults := make([]hookCommandResult, len(parallelCommands))

	var parallelWait sync.WaitGroup

	for idx, command := range parallelCommands {
		parallelWait.Add(1)

		go func(pos int, cmd hookCommand) {
			defer parallelWait.Done()

			cmdStart := time.Now()
			cmdExit := runFilteredHookCommand(cfg, cmd, files)

			parallelResults[pos] = hookCommandResult{
				Name:       cmd.Name,
				Status:     hookStatusForExitCode(cmdExit),
				ExitCode:   cmdExit,
				DurationMS: durationMilliseconds(cmdStart),
			}
		}(idx, command)
	}

	parallelWait.Wait()

	for _, result := range parallelResults {
		commandResults = append(commandResults, result)

		if result.ExitCode != 0 {
			exit = 1
		}
	}

	return hookGroupResult{
		Name:       group.Name,
		Status:     hookStatusForExitCode(exit),
		ExitCode:   exit,
		DurationMS: durationMilliseconds(start),
		Commands:   commandResults,
	}
}

func runFilteredHookCommand(cfg Config, command hookCommand, files []string) int {
	if command.Filter == nil {
		return command.Run(cfg, files)
	}

	filtered := command.Filter(files)
	if len(filtered) == 0 {
		return 0
	}

	return command.Run(cfg, filtered)
}

type hookCommandResult struct {
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	ExitCode   int     `json:"exit_code"`
	DurationMS float64 `json:"duration_ms"`
}

type hookGroupResult struct {
	RunnerFailure error               `json:"-"`
	Name          string              `json:"name"`
	Status        string              `json:"status"`
	Output        string              `json:"output,omitempty"`
	Commands      []hookCommandResult `json:"commands,omitempty"`
	ExitCode      int                 `json:"exit_code"`
	DurationMS    float64             `json:"duration_ms"`
}

func hookGroupChildEnvironment(resultPath, childConsumerRoot string) []string {
	env := []string{
		hookGroupChildEnv + "=1",
		hookGroupResultPathEnv + "=" + resultPath,
	}

	if root := strings.TrimSpace(os.Getenv(precommitRootEnv)); root != "" {
		env = append(env, precommitRootEnv+"="+root)
	}

	if root := strings.TrimSpace(childConsumerRoot); root != "" {
		env = append(env, consumerRootEnv+"="+root)
	}

	return env
}

func writeHookGroupResultFile(path string, result hookGroupResult) {
	cleanPath := filepath.Clean(strings.TrimSpace(path))

	tempPrefix := os.TempDir() + string(os.PathSeparator)
	if cleanPath == "." || !strings.HasPrefix(cleanPath, tempPrefix) {
		return
	}

	data, err := json.Marshal(result)
	if err != nil {
		return
	}

	if os.WriteFile(cleanPath, data, hookGroupResultFileMode) != nil {
		return
	}
}

func readHookGroupResultFile(path string) (hookGroupResult, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return hookGroupResult{}, false
	}

	var result hookGroupResult
	if json.Unmarshal(data, &result) != nil {
		return hookGroupResult{}, false
	}

	return result, true
}

func hookStatusForExitCode(exitCode int) string {
	if exitCode == 0 {
		return statusPass
	}

	return statusFail
}

func durationMilliseconds(start time.Time) float64 {
	return float64(time.Since(start).Milliseconds())
}

func enabledHookGroupNames(names []string) []string {
	settings := loadHookSettings()
	if len(settings.EnabledGroups) == 0 {
		return names
	}

	enabled := map[string]bool{}
	for _, name := range settings.EnabledGroups {
		enabled[strings.TrimSpace(name)] = true
	}

	selected := make([]string, 0, len(names))
	for _, name := range names {
		if enabled[name] {
			selected = append(selected, name)
		}
	}

	return selected
}

func hookFilesForPreCommit(allFiles bool) ([]string, error) {
	if allFiles {
		return gitLines("ls-files")
	}

	return gitLines("diff", "--cached", "--name-only", "--diff-filter=ACMR")
}

func pushedFiles(input io.Reader) ([]string, error) {
	scanner := bufio.NewScanner(input)
	seen := map[string]bool{}
	files := []string{}

	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 4 || fields[1] == allZeroSHA {
			continue
		}

		var (
			changed []string
			err     error
		)

		if fields[3] == allZeroSHA {
			changed, err = gitLinesInRoot(
				localRepoRoot(),
				"diff",
				"--name-only",
				emptyTreeSHA,
				fields[1],
			)
		} else {
			changed, err = gitLinesInRoot(
				localRepoRoot(),
				"diff",
				"--name-only",
				fields[3]+".."+fields[1],
			)
		}

		if err != nil {
			return nil, err
		}

		for _, path := range changed {
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}

	err := scanner.Err()
	if err != nil {
		return nil, fmt.Errorf("read pre-push refs: %w", err)
	}

	if len(files) > 0 {
		return files, nil
	}

	upstream := gitOutputInRoot(
		localRepoRoot(),
		"rev-parse",
		"--abbrev-ref",
		"--symbolic-full-name",
		"@{upstream}",
	)
	if upstream != "" {
		changed, err := gitLinesInRoot(
			localRepoRoot(),
			"diff",
			"--name-only",
			upstream+"...HEAD",
		)
		if err == nil {
			return changed, nil
		}
	}

	return gitLinesInRoot(localRepoRoot(), "diff", "--name-only", "HEAD")
}

func gitLines(args ...string) ([]string, error) {
	return gitLinesInRoot(repoRoot(), args...)
}

func gitLinesInRoot(root string, args ...string) ([]string, error) {
	output, err := combinedGitOutputInRoot(root, args...)
	if err != nil {
		return nil, fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(args, " "),
			err,
			strings.TrimSpace(string(output)),
		)
	}

	lines := []string{}
	seen := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		path := strings.TrimSpace(line)
		if path != "" && !seen[path] {
			seen[path] = true
			lines = append(lines, path)
		}
	}

	return lines, nil
}

func restageFiles(files []string) int {
	files = existingFiles(files)
	if len(files) == 0 {
		return 0
	}

	args := append([]string{"add", "--"}, files...)

	output, err := combinedGitOutput(args...)
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			message = err.Error()
		}

		writeLine(os.Stderr, message)

		return 1
	}

	return 0
}

func combinedGitOutput(args ...string) ([]byte, error) {
	return combinedGitOutputInRoot(repoRoot(), args...)
}

func combinedGitOutputInRoot(root string, args ...string) ([]byte, error) {
	output, err := evaluators.GitCommand(root, args...).CombinedOutput()
	if err != nil {
		return output, fmt.Errorf("run git command: %w", err)
	}

	return output, nil
}

func validateGoHookRuntime() int {
	_, err := findBundleRoot()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	for name, group := range canonicalHookGroups() {
		if strings.TrimSpace(name) == "" || len(group.Commands) == 0 {
			writef(os.Stderr, "FATAL: invalid empty hook group %q\n", name)

			return 1
		}

		for _, command := range group.Commands {
			if strings.TrimSpace(command.Name) == "" || command.Run == nil {
				fmt.Fprintf(
					os.Stderr,
					"FATAL: invalid hook command in group %q\n",
					name,
				)

				return 1
			}
		}
	}

	return 0
}

func repoPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(repoRoot(), path)
}
