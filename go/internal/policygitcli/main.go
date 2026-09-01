// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policygitcli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/feedback"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const blockedExitCode = 2

var (
	errBundleRequired        = apperror.StaticError("--bundle is required")
	errInvalidBundle         = apperror.StaticError("invalid policy bundle")
	errAdminApprovedRequired = apperror.StaticError(
		"admin-start-branch requires --admin-approved",
	)
	errWrapperBoolValue = apperror.StaticError(
		"policy-git boolean option accepts only true or false",
	)
	errWrapperOptionEmpty = apperror.StaticError(
		"policy-git option requires a non-empty value",
	)
	errWrapperOptionUnknown = apperror.StaticError(
		"unknown policy-git option",
	)
	errWrapperValueRequired = apperror.StaticError(
		"policy-git option requires a value",
	)
)

func run() error {
	return runWithArgs(os.Args[1:])
}

func runWithArgs(args []string) error {
	parsed, err := parsePolicyGitArgs(args)
	if err != nil {
		return fmt.Errorf("parse flags: %w", err)
	}

	if parsed.bundlePath == "" {
		return errBundleRequired
	}

	bundle, err := readValidatedBundle(parsed.bundlePath)
	if err != nil {
		return err
	}

	argv := parsed.gitArgv

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get cwd: %w", err)
	}

	if len(argv) > 0 && argv[0] == "admin-start-branch" {
		return startAdminBranch(parsed.realGit, cwd, argv[1:], parsed.adminApproved)
	}

	restoreRealGit := exposeRealGitForPolicyEvaluation(parsed.realGit)
	defer restoreRealGit()

	options, err := gitOptions(argv, cwd, parsed.adminApproved)
	if err != nil {
		return err
	}

	result, err := gitwrap.Check(bundle, options)
	if err != nil {
		return fmt.Errorf("check git policy: %w", err)
	}

	err = maybePrintJSON(parsed.jsonOutput, result)
	if err != nil {
		return err
	}

	if result.Blocked() {
		printBlocked(result)

		return gitwrap.ExitCodeError{Code: blockedExitCode}
	}

	if parsed.checkOnly {
		printAllowedCheck(parsed.jsonOutput)

		return nil
	}

	return executeGitWithPostChecks(bundle, parsed.realGit, options, parsed.jsonOutput)
}

type policyGitArguments struct {
	bundlePath    string
	realGit       string
	gitArgv       []string
	checkOnly     bool
	jsonOutput    bool
	adminApproved bool
}

func parsePolicyGitArgs(args []string) (policyGitArguments, error) {
	parsed := policyGitArguments{}

	for index := 0; index < len(args); {
		argument := args[index]

		if argument == "--" {
			parsed.gitArgv = append([]string(nil), args[index+1:]...)

			return parsed, nil
		}

		if gitGlobalOptionStartsArgv(argument) || !strings.HasPrefix(argument, "-") {
			parsed.gitArgv = append([]string(nil), args[index:]...)

			return parsed, nil
		}

		nextIndex, err := parsed.consumeWrapperOption(args, index)
		if err != nil {
			return policyGitArguments{}, err
		}

		index = nextIndex
	}

	return parsed, nil
}

func (parsed *policyGitArguments) consumeWrapperOption(
	args []string,
	index int,
) (int, error) {
	argument := args[index]
	name, value, hasValue := strings.Cut(argument, "=")

	switch name {
	case "--bundle", "--real-git":
		resolved, nextIndex, err := wrapperStringValue(
			args,
			index,
			name,
			value,
			hasValue,
		)
		if err != nil {
			return 0, err
		}

		parsed.setStringOption(name, resolved)

		return nextIndex, nil
	case "--check-only", "--json", "--admin-approved":
		boolValue, err := wrapperBoolValue(name, value, hasValue)
		if err != nil {
			return 0, err
		}

		parsed.setBoolOption(name, boolValue)

		return index + 1, nil
	default:
		return 0, fmt.Errorf("%w %q", errWrapperOptionUnknown, argument)
	}
}

func wrapperStringValue(
	args []string,
	index int,
	name string,
	value string,
	hasValue bool,
) (string, int, error) {
	if hasValue {
		if value == "" {
			return "", 0, fmt.Errorf("%w: %s", errWrapperOptionEmpty, name)
		}

		return value, index + 1, nil
	}

	nextIndex := index + 1
	if nextIndex >= len(args) {
		return "", 0, fmt.Errorf("%w: %s", errWrapperValueRequired, name)
	}

	value = args[nextIndex]
	if value == "" {
		return "", 0, fmt.Errorf("%w: %s", errWrapperOptionEmpty, name)
	}

	return value, nextIndex + 1, nil
}

func (parsed *policyGitArguments) setStringOption(name, value string) {
	if name == "--bundle" {
		parsed.bundlePath = value

		return
	}

	parsed.realGit = value
}

func (parsed *policyGitArguments) setBoolOption(name string, value bool) {
	switch name {
	case "--check-only":
		parsed.checkOnly = value
	case "--json":
		parsed.jsonOutput = value
	case "--admin-approved":
		parsed.adminApproved = value
	}
}

func wrapperBoolValue(name, value string, hasValue bool) (bool, error) {
	if !hasValue || value == "true" {
		return true, nil
	}

	if value == "false" {
		return false, nil
	}

	return false, fmt.Errorf("%w: %s", errWrapperBoolValue, name)
}

func gitGlobalOptionStartsArgv(argument string) bool {
	if strings.HasPrefix(argument, "-c=") || strings.HasPrefix(argument, "-C=") {
		return true
	}

	name, _, _ := strings.Cut(argument, "=")
	switch name {
	case "-c", "-C", "-p", "-P", "--paginate", "--no-pager", "--no-replace-objects",
		"--bare", "--git-dir", "--work-tree", "--namespace", "--super-prefix",
		"--exec-path", "--html-path", "--man-path", "--info-path", "--config-env",
		"--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs",
		"-v", "--version", "-h", "--help":
		return true
	default:
		return false
	}
}

func exposeRealGitForPolicyEvaluation(path string) func() {
	if path == "" {
		return func() {}
	}

	previous, existed := os.LookupEnv(realgit.Env)
	_ = os.Setenv(realgit.Env, path)

	return func() {
		if existed {
			_ = os.Setenv(realgit.Env, previous)

			return
		}

		_ = os.Unsetenv(realgit.Env)
	}
}

func readValidatedBundle(bundlePath string) (policy.Bundle, error) {
	bundle, err := readBundle(bundlePath)
	if err != nil {
		return policy.Bundle{}, err
	}

	err = bundle.Validate()
	if err != nil {
		return policy.Bundle{}, fmt.Errorf(
			"%w:\n%s",
			errInvalidBundle,
			policy.FormatValidationError(err),
		)
	}

	return bundle, nil
}

func printAllowedCheck(jsonOutput bool) {
	if !jsonOutput {
		feedback.Emit(
			os.Stdout,
			feedback.Message{Scalars: []feedback.Scalar{
				feedback.S("status", "allowed"),
				feedback.S("summary", "git policy check allowed"),
			}},
			feedback.FormatTOON,
		)
	}
}

func startAdminBranch(
	realGit string,
	cwd string,
	args []string,
	adminApproved bool,
) error {
	if !adminApproved {
		return errAdminApprovedRequired
	}

	err := gitwrap.VerifyAdminApproved(cwd)
	if err != nil {
		return fmt.Errorf("verify admin approval: %w", err)
	}

	err = gitwrap.AdminStartBranch(realGit, cwd, args)
	if err != nil {
		return fmt.Errorf("start admin-approved branch: %w", err)
	}

	return nil
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

	argv = withoutInitialRedundantChangeDir(argv)

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

func withoutInitialRedundantChangeDir(argv []string) []string {
	index := 0
	for index+1 < len(argv) && argv[index] == "-C" && argv[index+1] == "." {
		index += 2
	}

	return append([]string(nil), argv[index:]...)
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
	for index := range args {
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

		return gitwrap.ExitCodeError{Code: blockedExitCode}
	}

	return nil
}

func maybePrintJSON(jsonOutput bool, result gitwrap.Result) error {
	if !jsonOutput {
		return nil
	}

	var buffer bytes.Buffer

	err := gitwrap.EncodeResult(&buffer, result)
	if err != nil {
		return fmt.Errorf("encode result: %w", err)
	}

	err = feedback.WriteRendered(
		os.Stdout,
		strings.TrimSuffix(buffer.String(), "\n"),
		feedback.FormatJSON,
	)
	if err != nil {
		return fmt.Errorf("write result: %w", err)
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
			scalars := []feedback.Scalar{
				feedback.S("status", "blocked"),
				feedback.S("policy_id", decision.PolicyID),
				feedback.S("message", decision.Message),
			}
			if decision.Suggestion != "" {
				scalars = append(scalars, feedback.S("suggestion", decision.Suggestion))
			}

			files := decision.EvidenceFiles()

			tables := []feedback.Table{}
			if len(files) > 0 {
				tables = append(tables, feedback.T("files", []string{"path"}, fileRows(files)))
			}

			feedback.Emit(
				os.Stderr,
				feedback.Message{
					Scalars: scalars,
					Tables:  tables,
				},
				feedback.FormatTOON,
			)
		}
	}
}

func fileRows(files []string) [][]string {
	rows := make([][]string, 0, len(files))
	for _, file := range files {
		rows = append(rows, []string{file})
	}

	return rows
}
