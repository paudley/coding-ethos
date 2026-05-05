// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedExitCode = 2
const adminApprovedEnv = "CODE_ETHOS_ADMIN_APPROVED"

var (
	errBundleRequired = errors.New("--bundle is required")
	errHookRequired   = errors.New("git hook name is required")
	errRunnerRequired = errors.New("--runner is required")
	errInvalidBundle  = errors.New("invalid policy bundle")
)

func main() {
	os.Exit(runWithArgs(os.Args[1:]))
}

func runWithArgs(args []string) int {
	flags := flag.NewFlagSet("coding-ethos-git-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	runnerPath := flags.String("runner", "", "Path to the hook group runner")
	cwd := flags.String("cwd", "", "Repository root")

	err := flags.Parse(args)
	if err != nil {
		printErr(err)
		return 1
	}

	if *bundlePath == "" {
		printErr(errBundleRequired)
		return 1
	}

	if *runnerPath == "" {
		printErr(errRunnerRequired)
		return 1
	}

	hookArgs := flags.Args()
	if len(hookArgs) == 0 {
		printErr(errHookRequired)
		return 1
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		printErr(err)
		return 1
	}

	err = bundle.Validate()
	if err != nil {
		printErr(fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)))
		return 1
	}

	hookName := hookArgs[0]
	if hookName == "commit-msg" {
		if len(hookArgs) < 2 || strings.TrimSpace(hookArgs[1]) == "" {
			printErr(errors.New("commit-msg hook requires a message file"))
			return 1
		}

		result, runErr := lint.Run(bundle, lint.Options{
			Scope: lint.ScopeCommit,
			Cwd:   *cwd,
			Files: []string{hookArgs[1]},
		})
		if runErr != nil {
			printErr(runErr)
			return 1
		}
		lint.EnsureTraceID(&result)
		logLintResult(*cwd, result)

		if result.Blocked() {
			encodeLintResult(result)
			return blockedExitCode
		}

		return 0
	}

	if hookName == "pre-commit" || hookName == "pre-push" {
		files, err := hookFiles(*cwd, hookName)
		if err != nil {
			printErr(err)
			return 1
		}

		result, runErr := lint.Run(bundle, lint.Options{
			AdminApproved: os.Getenv(adminApprovedEnv) == "1",
			Scope:         lint.ScopeStaged,
			Cwd:           *cwd,
			Files:         files,
		})
		if runErr != nil {
			printErr(runErr)
			return 1
		}
		lint.EnsureTraceID(&result)
		logLintResult(*cwd, result)

		if result.Blocked() {
			encodeLintResult(result)
			return blockedExitCode
		}
	}

	return runLegacyRunner(*runnerPath, hookArgs)
}

func hookFiles(cwd string, hookName string) ([]string, error) {
	if hookName != "pre-commit" {
		return nil, nil
	}

	command := exec.CommandContext(
		context.Background(),
		"git",
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
		"--",
	)
	command.Dir = cwd

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}

	files := []string{}
	for _, line := range strings.Split(string(output), "\n") {
		file := strings.TrimSpace(line)
		if file != "" {
			files = append(files, file)
		}
	}

	return files, nil
}

func readBundle(path string) (policy.Bundle, error) {
	file, err := os.Open(path)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("open bundle: %w", err)
	}
	defer file.Close()

	bundle, err := policy.DecodeBundle(file)
	if err != nil {
		return policy.Bundle{}, fmt.Errorf("decode bundle: %w", err)
	}

	return bundle, nil
}

func encodeLintResult(result lint.Result) {
	if err := encodeLintResultTo(os.Stderr, result); err != nil {
		fmt.Fprintf(os.Stderr, "coding-ethos policy blocked %s\n", result.Scope)
	}
}

func logLintResult(cwd string, result lint.Result) {
	if _, err := lint.LogResult(cwd, result); err != nil {
		fmt.Fprintf(os.Stderr, "WARN: lint trace not written: %v\n", err)
	}
}

func encodeLintResultTo(writer io.Writer, result lint.Result) error {
	if result.Blocked() {
		result = blockedOnlyResult(result)
	}

	return hookoutput.EncodeLintResult(writer, result, hookoutput.SelectedFormat())
}

func blockedOnlyResult(result lint.Result) lint.Result {
	filtered := lint.Result{
		TraceID:    result.TraceID,
		Scope:      result.Scope,
		Status:     result.Status,
		SkillHints: append([]lint.SkillHint(nil), result.SkillHints...),
	}

	for _, decision := range result.Decisions {
		if decision.Decision != "block" && decision.Severity != "block" {
			continue
		}

		filtered.Decisions = append(filtered.Decisions, decision)
		filtered.Diagnostics = append(filtered.Diagnostics, decision.Diagnostics...)
	}
	for _, finding := range result.Findings {
		if !finding.Blocking {
			continue
		}

		filtered.Findings = append(filtered.Findings, finding)
	}

	return filtered
}

func runLegacyRunner(runnerPath string, args []string) int {
	commandArgs := append([]string{"git-hook"}, args...)
	command := exec.CommandContext(context.Background(), runnerPath, commandArgs...)
	command.Stdin = os.Stdin
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr

	err := command.Run()
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	fmt.Fprintf(os.Stderr, "run hook group runner: %v\n", err)

	return 1
}

func exitErr(err error) {
	printErr(err)
	os.Exit(1)
}

func printErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
}
