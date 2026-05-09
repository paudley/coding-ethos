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

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

var (
	errAdminBranchNameRequired = apperror.StaticError("admin branch name is required")
	errAdminBranchArgCount     = apperror.StaticError(
		"admin-start-branch accepts exactly one branch name",
	)
	errAdminBranchDirty = apperror.StaticError(
		"worktree must be clean before admin branch start",
	)
	errAdminBranchInvalid = apperror.StaticError("invalid admin branch name")
)

func AdminStartBranch(realGit, cwd string, args []string) error {
	if len(args) == 0 {
		return errAdminBranchNameRequired
	}

	if len(args) != 1 {
		return errAdminBranchArgCount
	}

	branch := strings.TrimSpace(args[0])

	if !validAdminBranchName(realGit, cwd, branch) {
		return fmt.Errorf("%w: %q", errAdminBranchInvalid, args[0])
	}

	err := ensureCleanWorktree(realGit, cwd)
	if err != nil {
		return err
	}

	for _, command := range [][]string{
		{"checkout", "main"},
		{"pull", "--ff-only"},
		{"checkout", "-b", branch},
	} {
		err = runRealGit(realGit, cwd, command...)
		if err != nil {
			return err
		}
	}

	return nil
}

func ensureCleanWorktree(realGit, cwd string) error {
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

func validAdminBranchName(realGit, cwd, branch string) bool {
	if branch == "" {
		return false
	}

	err := runRealGit(realGit, cwd, "check-ref-format", "--branch", branch)

	return err == nil
}

func realGitOutput(realGit, cwd string, args ...string) (string, error) {
	cmd := realGitCommand(realGit, cwd, args...)

	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}

	return string(output), nil
}

func runRealGit(realGit, cwd string, args ...string) error {
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

func realGitCommand(realGit, cwd string, args ...string) *exec.Cmd {
	cmd := realgit.CommandFor(context.Background(), realGit, false, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}

	cmd.Env = gitExecutionEnv(true)

	return cmd
}
