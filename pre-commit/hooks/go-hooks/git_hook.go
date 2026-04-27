// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

var errGitCommandFailed = errors.New("git command failed")

const (
	allZeroSHA   = "0000000000000000000000000000000000000000"
	emptyTreeSHA = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
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
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(
				os.Stderr,
				"Usage: coding-ethos-hook git-hook commit-msg <message-file>",
			)

			return 1
		}

		return runNamedHookGroups(cfg, []string{"commit-msg"}, []string{args[1]})
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

func runNamedHookGroups(cfg Config, names []string, files []string) int {
	groups := canonicalHookGroups()
	exit := 0

	for _, name := range enabledHookGroupNames(names) {
		group, ok := groups[name]
		if !ok {
			fmt.Fprintf(os.Stderr, "FATAL: unknown hook group %q\n", name)

			return 1
		}

		for _, command := range group.Commands {
			if command(cfg, files) != 0 {
				exit = 1
			}
		}
	}

	return exit
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
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), result.RunnerFailure)
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
	}

	return 0
}

func repoPath(path string) string {
	if filepath.IsAbs(path) {
		return path
	}

	return filepath.Join(repoRoot(), path)
}
