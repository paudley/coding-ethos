// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

var allZeroSHA = strings.Repeat("0", 40)

func runGitHookCommand(cfg Config, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook git-hook <pre-commit|pre-push|commit-msg|validate> [args...]")

		return 1
	}
	switch args[0] {
	case "pre-commit":
		return runPreCommitHook(cfg, args[1:])
	case "pre-push":
		return runPrePushHook(cfg, os.Stdin)
	case "commit-msg":
		if len(args) < 2 || strings.TrimSpace(args[1]) == "" {
			fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook git-hook commit-msg <message-file>")

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
		var changed []string
		var err error
		if fields[3] == allZeroSHA {
			changed, err = gitLines("diff-tree", "--no-commit-id", "--name-only", "-r", fields[1])
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
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read pre-push refs: %w", err)
	}
	if len(files) > 0 {
		return files, nil
	}
	if upstream := gitOutput("rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}"); upstream != "" {
		if changed, err := gitLines("diff", "--name-only", upstream+"...HEAD"); err == nil {
			return changed, nil
		}
	}

	return gitLines("diff", "--name-only", "HEAD")
}

func gitLines(args ...string) ([]string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoRoot()
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	lines := []string{}
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
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
	if result := runExternalTool(externalToolRequest{Name: "git-add", Dir: repoRoot(), Command: append([]string{"git"}, args...)}); result.ExitCode != 0 {
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
	if _, err := findBundleRoot(); err != nil {
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
