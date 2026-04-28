// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

var (
	errAdminBranchNameRequired = errors.New("admin branch name is required")
	errAdminBranchArgCount     = errors.New("admin-start-branch accepts exactly one branch name")
	errAdminBranchDirty        = errors.New("worktree must be clean before admin branch start")
	errAdminBranchInvalid      = errors.New("invalid admin branch name")
)

func AdminStartBranch(realGit string, cwd string, args []string) error {
	if len(args) == 0 {
		return errAdminBranchNameRequired
	}
	if len(args) != 1 {
		return errAdminBranchArgCount
	}

	branch := strings.TrimSpace(args[0])
	resolvedGit, err := ResolveRealGit(realGit)
	if err != nil {
		return err
	}

	if !validAdminBranchName(resolvedGit, cwd, branch) {
		return fmt.Errorf("%w: %q", errAdminBranchInvalid, args[0])
	}

	err = ensureCleanWorktree(resolvedGit, cwd)
	if err != nil {
		return err
	}

	for _, command := range [][]string{
		{"checkout", "main"},
		{"pull", "--ff-only"},
		{"checkout", "-b", branch},
	} {
		err = runRealGit(resolvedGit, cwd, command...)
		if err != nil {
			return err
		}
	}

	return nil
}

func ensureCleanWorktree(realGit string, cwd string) error {
	output, err := realGitOutput(
		realGit,
		cwd,
		"status",
		"--porcelain=v1",
		"--untracked-files=all",
	)
	if err != nil {
		return err
	}

	if strings.TrimSpace(output) != "" {
		return errAdminBranchDirty
	}

	return nil
}

func validAdminBranchName(realGit string, cwd string, branch string) bool {
	if branch == "" {
		return false
	}

	err := runRealGit(realGit, cwd, "check-ref-format", "--branch", branch)

	return err == nil
}

func realGitOutput(realGit string, cwd string, args ...string) (string, error) {
	cmd := realGitCommand(realGit, cwd, args...)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}

func runRealGit(realGit string, cwd string, args ...string) error {
	cmd := realGitCommand(realGit, cwd, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return ExitCodeError{Code: exitError.ExitCode()}
		}

		return fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return nil
}

func realGitCommand(realGit string, cwd string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(context.Background(), realGit, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Env = gitExecutionEnv(true)

	return cmd
}
