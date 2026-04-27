// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedExitCode = 2

var (
	errBundleRequired = errors.New("--bundle is required")
	errHookRequired   = errors.New("git hook name is required")
	errRunnerRequired = errors.New("--runner is required")
	errInvalidBundle  = errors.New("invalid policy bundle")
)

func main() {
	flags := flag.NewFlagSet("coding-ethos-git-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	runnerPath := flags.String("runner", "", "Path to the hook group runner")
	cwd := flags.String("cwd", "", "Repository root")

	err := flags.Parse(os.Args[1:])
	if err != nil {
		exitErr(err)
	}

	if *bundlePath == "" {
		exitErr(errBundleRequired)
	}

	if *runnerPath == "" {
		exitErr(errRunnerRequired)
	}

	args := flags.Args()
	if len(args) == 0 {
		exitErr(errHookRequired)
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		exitErr(err)
	}

	err = bundle.Validate()
	if err != nil {
		exitErr(
			fmt.Errorf("%w:\n%s", errInvalidBundle, policy.FormatValidationError(err)),
		)
	}

	hookName := args[0]
	if hookName == "pre-commit" || hookName == "pre-push" {
		result, runErr := lint.Run(bundle, lint.Options{
			Scope: lint.ScopeStaged,
			Cwd:   *cwd,
		})
		if runErr != nil {
			exitErr(runErr)
		}

		if result.Blocked() {
			encodeLintResult(result)
			os.Exit(blockedExitCode)
		}
	}

	os.Exit(runLegacyRunner(*runnerPath, args))
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
	encoder := json.NewEncoder(os.Stderr)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err := encoder.Encode(result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coding-ethos policy blocked %s\n", result.Scope)
	}
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
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}
