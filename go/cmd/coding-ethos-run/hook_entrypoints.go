// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"fmt"
	"os/exec"
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
		return errors.New("git-hook requires a hook name")
	}
	requirePolicyBundle(paths)
	if args[0] == "validate" {
		requireRuntimeFile(paths.PolicyMetadata, "compiled policy metadata")
		runTool(paths, "coding-ethos-policy", "validate-metadata", "--metadata", paths.PolicyMetadata)
	}
	switch args[0] {
	case "pre-commit", "pre-push", "commit-msg", "validate":
	default:
		return fmt.Errorf("unknown git hook %q", args[0])
	}
	requireRuntimeBinary(paths.GitHookRunner, "bundled Go hook runner")
	installLintToolShims(paths)
	execTool(paths, "coding-ethos-git-hook", append([]string{
		"--bundle", paths.PolicyBundle,
		"--runner", paths.GitHookRunner,
		"--cwd", paths.Root,
	}, args...)...)

	return nil
}

func runLFSHook(paths runtimePaths, args []string) error {
	if len(args) == 0 {
		return errors.New("lfs-hook requires a hook name")
	}
	if !isLFSHookName(args[0]) {
		return fmt.Errorf("unknown LFS hook %q", args[0])
	}
	if err := exec.Command(paths.RealGit, "lfs", "version").Run(); err != nil {
		return nil
	}
	execExternal(paths.RealGit, append([]string{"lfs", args[0]}, args[1:]...)...)

	return nil
}
