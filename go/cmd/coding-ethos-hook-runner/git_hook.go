// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

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

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

var (
	errGitCommandFailed  = apperror.StaticError("git command failed")
	errHookGroupTimedOut = apperror.StaticError("hook group timed out")
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
	case "pre-commit":
		return runPreCommitHook(cfg, args[1:])
	case "pre-push":
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
		fmt.Fprintf(os.Stderr, "FATAL: unknown git hook %q\n", args[0])

		return 1
	}
}

func runPreCommitHook(cfg Config, args []string) int {
	allFiles := slices.Contains(args, "--all-files")

	files, err := hookFilesForPreCommit(allFiles)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if runFormatGroup(cfg, files, !allFiles) != 0 {
		return 1
	}

	return runNamedHookGroups(cfg, []string{
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
		"ai",
	}, files)
}

func runPrePushHook(cfg Config, input io.Reader) int {
	files, err := pushedFiles(input)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

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
	if slices.Contains(enabledHookGroupNames([]string{"ai"}), "ai") &&
		runGeminiCheck(cfg, []string{"--full-check"}) != 0 {
		exit = 1
	}

	return exit
}

func runNamedHookGroups(cfg Config, names, files []string) int {
	groups := canonicalHookGroups()
	selectedNames := enabledHookGroupNames(names)
	selectedGroups := make([]hookGroup, 0, len(selectedNames))

	for _, name := range selectedNames {
		group, ok := groups[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "FATAL: unknown hook group %q\n", name)

			return 1
		}

		selectedGroups = append(selectedGroups, group)
	}

	if len(selectedGroups) <= 1 || !hookParallelGroupsEnabled() {
		return runHookGroupsInProcess(cfg, selectedGroups, files)
	}

	return runHookGroupsInSubprocesses(selectedGroups, files)
}

func runHookGroupsInProcess(cfg Config, groups []hookGroup, files []string) int {
	exit := 0

	for _, group := range groups {
		result := runHookGroupInProcess(cfg, group, files)
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

	for _, command := range group.Commands {
		commandStart := time.Now()

		commandExit := command.Run(cfg, files)
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

	return hookGroupResult{
		Name:       group.Name,
		Status:     hookStatusForExitCode(exit),
		ExitCode:   exit,
		DurationMS: durationMilliseconds(start),
		Commands:   commandResults,
	}
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

func runHookGroupsInSubprocesses(groups []hookGroup, files []string) int {
	return runHookGroupsInSubprocessesWithExecutable(groups, files, os.Executable)
}

func runHookGroupsInSubprocessesWithExecutable(
	groups []hookGroup,
	files []string,
	executablePath func() (string, error),
) int {
	results := make([]hookGroupResult, len(groups))
	waitGroup := sync.WaitGroup{}
	childConsumerRoot := repoRoot()

	for index, group := range groups {
		waitGroup.Add(1)

		go func(index int, group hookGroup) {
			defer waitGroup.Done()

			results[index] = runHookGroupSubprocess(
				group.Name,
				files,
				executablePath,
				childConsumerRoot,
			)
		}(index, group)
	}

	waitGroup.Wait()

	exit := 0
	verboseSuccess := hookVerboseSuccessOutputEnabled()

	for _, result := range results {
		if result.ExitCode != 0 || result.RunnerFailure != nil {
			exit = 1
		}
	}

	if verboseSuccess {
		writeLine(os.Stdout, formatHookExecutionSummary(
			results,
			selectedHookOutputFormat(),
		))
	}

	for _, result := range results {
		if result.ExitCode != 0 || result.RunnerFailure != nil || verboseSuccess {
			writeText(os.Stdout, result.Output)
		}

		if result.RunnerFailure != nil {
			fmt.Fprintf(
				os.Stderr,
				"FATAL: hook group %q failed: %v\n",
				result.Name,
				result.RunnerFailure,
			)
		}
	}

	return exit
}

func runHookGroupSubprocess(
	name string,
	files []string,
	executablePath func() (string, error),
	childConsumerRoot string,
) hookGroupResult {
	executable, err := executablePath()
	if err != nil {
		return hookGroupResult{
			RunnerFailure: fmt.Errorf("resolve hook executable: %w", err),
			Name:          name,
			ExitCode:      1,
		}
	}

	resultFile, err := os.CreateTemp("", "coding-ethos-hook-group-*.json")
	if err != nil {
		return hookGroupResult{
			RunnerFailure: fmt.Errorf("create hook result file: %w", err),
			Name:          name,
			Status:        statusFail,
			ExitCode:      1,
		}
	}

	resultPath := resultFile.Name()

	_ = resultFile.Close()

	defer os.Remove(resultPath)

	args := append([]string{"run-group", name}, files...)
	toolResult := runExternalTool(externalToolRequest{
		Name:           "hook group " + name,
		Command:        append([]string{executable}, args...),
		Env:            hookGroupChildEnvironment(resultPath, childConsumerRoot),
		TimeoutSeconds: loadHookSettings().ToolTimeoutSeconds,
	})

	result := hookGroupResult{
		Name:       name,
		Status:     hookStatusForExitCode(toolResult.ExitCode),
		Output:     toolResult.Combined,
		ExitCode:   toolResult.ExitCode,
		DurationMS: toolResult.DurationMS,
	}

	if toolResult.TimedOut {
		result.ExitCode = 1
		result.Status = statusFail
		result.RunnerFailure = fmt.Errorf(
			"%w after %d seconds: %s",
			errHookGroupTimedOut,
			loadHookSettings().ToolTimeoutSeconds,
			name,
		)

		return result
	}

	result.RunnerFailure = toolResult.RunnerFailure
	if childResult, ok := readHookGroupResultFile(resultPath); ok {
		result.Commands = childResult.Commands
		if result.ExitCode == childResult.ExitCode {
			result.Status = childResult.Status
		}
	}

	return result
}

func hookGroupChildEnvironment(resultPath, childConsumerRoot string) []string {
	env := []string{
		hookGroupChildEnv + "=1",
		hookGroupResultPathEnv + "=" + resultPath,
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
			changed, err = gitLines("diff", "--name-only", emptyTreeSHA+".."+fields[1])
		} else {
			changed, err = gitLines("diff", "--name-only", fields[3]+".."+fields[1])
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

	upstream := gitOutput(
		"rev-parse",
		"--abbrev-ref",
		"--symbolic-full-name",
		"@{upstream}",
	)
	if upstream != "" {
		changed, err := gitLines("diff", "--name-only", upstream+"...HEAD")
		if err == nil {
			return changed, nil
		}
	}

	return gitLines("diff", "--name-only", "HEAD")
}

func gitLines(args ...string) ([]string, error) {
	result := runExternalTool(externalToolRequest{
		Name:    "git",
		Dir:     repoRoot(),
		Command: append([]string{"git"}, args...),
	})
	if result.RunnerFailure != nil {
		return nil, fmt.Errorf(
			"git %s: %w",
			strings.Join(args, " "),
			result.RunnerFailure,
		)
	}

	if result.ExitCode != 0 {
		return nil, fmt.Errorf(
			"git %s: %w: %s",
			strings.Join(args, " "),
			errGitCommandFailed,
			result.Combined,
		)
	}

	lines := []string{}
	seen := map[string]bool{}

	for line := range strings.SplitSeq(strings.TrimSpace(result.Combined), "\n") {
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

	result := runExternalTool(externalToolRequest{
		Name:    "git-add",
		Dir:     repoRoot(),
		Command: append([]string{"git"}, args...),
	})
	if result.ExitCode != 0 {
		message := result.Combined
		if message == "" && result.RunnerFailure != nil {
			message = result.RunnerFailure.Error()
		}

		fmt.Fprintln(os.Stderr, message)

		return 1
	}

	return 0
}

func validateGoHookRuntime() int {
	_, err := findBundleRoot()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	for name, group := range canonicalHookGroups() {
		if strings.TrimSpace(name) == "" || len(group.Commands) == 0 {
			fmt.Fprintf(os.Stderr, "FATAL: invalid empty hook group %q\n", name)

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
