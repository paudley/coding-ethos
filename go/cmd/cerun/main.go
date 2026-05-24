// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"blackcat.ca/coding-ethos/go/internal/execguard"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/processstatus"
	"blackcat.ca/coding-ethos/go/internal/realgit"
	"blackcat.ca/coding-ethos/go/internal/safeexec"
)

const cerunMissingRuntimeExitCode = 127

func main() {
	execguard.Enter("cerun")

	exitCode := runCerun(os.Args[1:])
	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

func runCerun(args []string) int {
	if cerunHelpRequested(args) {
		emitCerunHelp()

		return 0
	}

	runner, err := siblingRunner()
	if err != nil {
		emitCerunError("cerun: " + err.Error())

		return cerunMissingRuntimeExitCode
	}

	return runCerunWithRunner(args, runner)
}

func runCerunWithRunner(args []string, runner string) int {
	command := safeexec.CommandContext(
		context.Background(),
		runner,
		cerunRuntimeArgs(args)...,
	)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	command.Env = cerunEnvironment()

	err := command.Run()
	if err != nil {
		var execErr *exec.Error
		if errors.As(err, &execErr) {
			emitCerunError(fmt.Sprintf("cerun: exec %s: %s", runner, err))
		}

		return cerunExitCode(err)
	}

	return 0
}

func emitCerunError(message string) {
	feedback.Emit(os.Stderr, feedback.Error{Message: message}, feedback.FormatTOON)
}

func emitCerunHelp() {
	feedback.Emit(os.Stdout, cerunHelpMessage(), feedback.FormatTOON)
}

func cerunHelpRequested(args []string) bool {
	return len(args) == 1 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help")
}

func cerunHelpMessage() feedback.Message {
	return feedback.Message{
		Scalars: []feedback.Scalar{
			feedback.S("command", "cerun"),
			feedback.S(
				"summary",
				"Run agent shell commands through the managed coding-ethos boundary.",
			),
		},
		Tables: []feedback.Table{
			feedback.T(
				"usage",
				[]string{"command", "purpose"},
				[][]string{
					{"cerun -- <command>", "Execute with managed rewrites enabled."},
					{"cerun --check -- <command>", "Preflight without executing."},
					{"cerun --no-rewrite -- <command>", "Diagnostic execution without rewrites."},
					{"cerun git <args>", "Shortcut for managed git routing."},
					{"cerun python <args>", "Shortcut for managed Python routing."},
					{"cerun lint <args>", "Run managed lint."},
				},
			),
		},
	}
}

func cerunRuntimeArgs(args []string) []string {
	if len(args) == 0 {
		return []string{"agent-shell"}
	}

	switch filepath.Base(args[0]) {
	case "git", "python":
		commandArgs := append([]string{filepath.Base(args[0])}, args[1:]...)

		return append([]string{"agent-shell", "--rewrite", "--"}, commandArgs...)
	case "lint":
		return append([]string{"policy-lint"}, args[1:]...)
	default:
		return append([]string{"agent-shell"}, args...)
	}
}

func cerunExitCode(err error) int {
	if err == nil {
		return 0
	}

	var execErr *exec.Error
	if errors.As(err, &execErr) || errors.Is(err, os.ErrNotExist) {
		return cerunMissingRuntimeExitCode
	}

	return processstatus.ExitCode(err, 1)
}

func cerunEnvironment() []string {
	env := os.Environ()
	if os.Getenv(realgit.Env) != "" {
		return env
	}

	gitPath, err := realgit.Resolve(context.Background(), "git")
	if err != nil {
		gitPath = systemGitPath()
		if gitPath == "" {
			return env
		}
	}

	return append(env, realgit.Env+"="+gitPath)
}

func systemGitPath() string {
	for _, candidate := range []string{
		"/usr/bin/git",
		"/bin/git",
		"/usr/local/bin/git",
		"/opt/homebrew/bin/git",
	} {
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() && info.Mode().Perm()&0o111 != 0 {
			return candidate
		}
	}

	return ""
}

func siblingRunner() (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable: %w", err)
	}

	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return "", fmt.Errorf("resolve executable symlink: %w", err)
	}

	return filepath.Join(filepath.Dir(executable), "coding-ethos-run"), nil
}
