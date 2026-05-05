// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const blockedExitCode = 2

var (
	errBundleRequired        = errors.New("--bundle is required")
	errInvalidBundle         = errors.New("invalid policy bundle")
	errAdminApprovedRequired = errors.New(
		"admin-start-branch requires --admin-approved",
	)
)

func main() {
	err := run()
	if err == nil {
		return
	}

	var exitError gitwrap.ExitCodeError
	if errors.As(err, &exitError) {
		os.Exit(exitError.Code)
	}

	exitErr(err)
}

func run() error {
	return runWithArgs(os.Args[1:])
}

func runWithArgs(args []string) error {
	flags := flag.NewFlagSet("coding-ethos-git", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	realGit := flags.String("real-git", "", "Real git executable")
	checkOnly := flags.Bool("check-only", false, "Check policy without executing git")
	jsonOutput := flags.Bool("json", false, "Emit JSON result")
	adminApproved := flags.Bool(
		"admin-approved",
		false,
		"Allow admin-protected coding-ethos commits when process ancestry is approved",
	)

	err := flags.Parse(args)
	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if *bundlePath == "" {
		return errBundleRequired
	}

	bundle, err := readBundle(*bundlePath)
	if err != nil {
		return err
	}

	err = bundle.Validate()
	if err != nil {
		return fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	argv := flags.Args()

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	if len(argv) > 0 && argv[0] == "admin-start-branch" {
		if !*adminApproved {
			return errAdminApprovedRequired
		}

		err := gitwrap.VerifyAdminApproved(cwd)
		if err != nil {
			return fmt.Errorf("verify admin approval: %w", err)
		}

		return gitwrap.AdminStartBranch(*realGit, cwd, argv[1:])
	}

	options, err := gitOptions(argv, cwd, *adminApproved)
	if err != nil {
		return err
	}

	result, err := gitwrap.Check(bundle, options)
	if err != nil {
		return fmt.Errorf("check git policy: %w", err)
	}

	err = maybePrintJSON(*jsonOutput, result)
	if err != nil {
		return err
	}

	if result.Blocked() {
		printBlocked(result)
		os.Exit(blockedExitCode)
	}

	if *checkOnly {
		if !*jsonOutput {
			fmt.Fprintln(os.Stdout, "git policy check allowed")
		}

		return nil
	}

	return executeGitWithPostChecks(bundle, *realGit, options, *jsonOutput)
}

func gitOptions(
	argv []string,
	cwd string,
	adminApproved bool,
) (gitwrap.Options, error) {
	if adminApproved {
		err := gitwrap.VerifyAdminApproved(cwd)
		if err != nil {
			return gitwrap.Options{}, fmt.Errorf("verify admin approval: %w", err)
		}
	}
	stdin, err := stdinForGitArgv(argv)
	if err != nil {
		return gitwrap.Options{}, err
	}

	return gitwrap.Options{
		AdminApproved: adminApproved,
		Argv:          argv,
		Cwd:           cwd,
		Stdin:         stdin,
	}, nil
}

func stdinForGitArgv(argv []string) ([]byte, error) {
	if !gitCommitReadsMessageFromStdin(argv) {
		return nil, nil
	}

	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return nil, fmt.Errorf("read git commit message from stdin: %w", err)
	}

	return data, nil
}

func gitCommitReadsMessageFromStdin(argv []string) bool {
	parsed := gitwrap.ParseArgv(argv)
	if parsed.Operation != "commit" {
		return false
	}

	args := parsed.Argv
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "-F" || arg == "--file" {
			return index+1 < len(args) && args[index+1] == "-"
		}
		if arg == "-F-" || arg == "--file=-" {
			return true
		}
	}

	return false
}

func executeGitWithPostChecks(
	bundle policy.Bundle,
	realGit string,
	options gitwrap.Options,
	jsonOutput bool,
) error {
	err := gitwrap.PreparePost(bundle, options)
	if err != nil {
		return fmt.Errorf("prepare post-git policy: %w", err)
	}

	resolvedGit, err := gitwrap.ResolveRealGit(realGit)
	if err != nil {
		return fmt.Errorf("resolve real git: %w", err)
	}

	err = gitwrap.Execute(resolvedGit, options)
	if err != nil {
		return fmt.Errorf("execute real git: %w", err)
	}

	postResult, err := gitwrap.VerifyPost(bundle, options)
	if err != nil {
		return fmt.Errorf("verify post-git policy: %w", err)
	}

	err = maybePrintJSON(jsonOutput, postResult)
	if err != nil {
		return err
	}

	if postResult.Blocked() {
		printBlocked(postResult)
		os.Exit(blockedExitCode)
	}

	return nil
}

func maybePrintJSON(jsonOutput bool, result gitwrap.Result) error {
	if !jsonOutput {
		return nil
	}

	err := gitwrap.EncodeResult(os.Stdout, result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}

	return nil
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

func printBlocked(result gitwrap.Result) {
	for _, decision := range result.Decisions {
		if decision.Decision == "block" || decision.Severity == "block" {
			fmt.Fprintf(
				os.Stderr,
				"[coding-ethos:%s] %s\n",
				decision.PolicyID,
				decision.Message,
			)

			if decision.Suggestion != "" {
				fmt.Fprintf(os.Stderr, "Suggestion: %s\n", decision.Suggestion)
			}
		}
	}
}

func exitErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
	os.Exit(1)
}
