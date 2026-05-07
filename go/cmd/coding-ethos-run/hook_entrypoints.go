// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

func isGitHookName(name string) bool {
	switch name {
	case "pre-commit", "pre-push", "commit-msg":
		return true
	default:
		return false
	}
}

func isLFSHookName(name string) bool {
	switch name {
	case "post-commit", "post-merge", "post-checkout":
		return true
	default:
		return false
	}
}

func runGitHook(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		return apperror.StaticError("git-hook requires a hook name")
	}

	requirePolicyBundle(paths)

	if args[0] == "validate" {
		requireRuntimeFile(paths.PolicyMetadata, "compiled policy metadata")
		runtimeRunTool(
			paths,
			"coding-ethos-policy",
			"validate-metadata",
			"--metadata",
			paths.PolicyMetadata,
		)
	}

	switch args[0] {
	case "pre-commit", "pre-push", "commit-msg", "validate":
	default:
		return apperror.Wrapf(
			apperror.StaticError("unknown git hook %q"),
			"unknown git hook %q",
			args[0],
		)
	}

	requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
	installLintToolShims(paths)
	runtimeExecTool(paths, "coding-ethos-git-hook", append([]string{
		"--bundle", paths.PolicyBundle,
		"--runner", paths.GitHookRunner,
		"--cwd", paths.Root,
	}, args...)...)

	return nil
}

func runLFSHook(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		return apperror.StaticError("lfs-hook requires a hook name")
	}

	if !isLFSHookName(args[0]) {
		return apperror.Wrapf(
			apperror.StaticError("unknown LFS hook %q"),
			"unknown LFS hook %q",
			args[0],
		)
	}

	err := safeexec.Command(paths.RealGit, "lfs", "version").Run()
	if err != nil {
		return fmt.Errorf("git-lfs is required for lfs-hook: %w", err)
	}

	runtimeExecExternal(
		paths,
		paths.RealGit,
		append([]string{"lfs", args[0]}, args[1:]...)...)

	return nil
}
