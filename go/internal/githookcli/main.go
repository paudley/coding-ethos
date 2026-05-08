// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package githookcli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/gitwrap"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/hookrunnercli"
	"blackcat.ca/coding-ethos/go/internal/lint"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

const (
	blockedExitCode  = 2
	adminApprovedEnv = "CODE_ETHOS_ADMIN_APPROVED"
)

var (
	errBundleRequired = apperror.StaticError("--bundle is required")
	errHookRequired   = apperror.StaticError("git hook name is required")
	errRunnerRequired = apperror.StaticError("--runner is required")
	errInvalidBundle  = apperror.StaticError("invalid policy bundle")
)

type gitHookConfig struct {
	BundlePath string
	Cwd        string
	RunnerPath string
	HookArgs   []string
}

func runWithArgs(args []string) int {
	config, err := parseGitHookConfig(args)
	if err != nil {
		printErr(err)

		return 1
	}

	bundle, err := validatedBundle(config.BundlePath)
	if err != nil {
		printErr(err)

		return 1
	}

	hookName := config.HookArgs[0]
	switch hookName {
	case "commit-msg":
		return runCommitMsgHook(bundle, config.Cwd, config.HookArgs)
	case "pre-commit", "pre-push":
		code, blocked := runStagedHook(bundle, config.Cwd, hookName)
		if blocked {
			return code
		}
	}

	return runHookGroupRunner(config.Cwd, config.HookArgs)
}

func parseGitHookConfig(args []string) (gitHookConfig, error) {
	flags := flag.NewFlagSet("coding-ethos-git-hook", flag.ExitOnError)
	bundlePath := flags.String("bundle", "", "Path to policy-bundle.json")
	runnerPath := flags.String("runner", "", "Path to the hook group runner")
	cwd := flags.String("cwd", "", "Repository root")

	err := flags.Parse(args)
	if err != nil {
		return gitHookConfig{}, fmt.Errorf("parse git hook args: %w", err)
	}

	if *bundlePath == "" {
		return gitHookConfig{}, errBundleRequired
	}

	if *runnerPath == "" {
		return gitHookConfig{}, errRunnerRequired
	}

	hookArgs := flags.Args()
	if len(hookArgs) == 0 {
		return gitHookConfig{}, errHookRequired
	}

	return gitHookConfig{
		BundlePath: *bundlePath,
		Cwd:        *cwd,
		HookArgs:   hookArgs,
		RunnerPath: *runnerPath,
	}, nil
}

func validatedBundle(bundlePath string) (policy.Bundle, error) {
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

func runCommitMsgHook(bundle policy.Bundle, cwd string, hookArgs []string) int {
	if len(hookArgs) < 2 || strings.TrimSpace(hookArgs[1]) == "" {
		printErr(apperror.StaticError("commit-msg hook requires a message file"))

		return 1
	}

	result, err := runHookPolicy(bundle, cwd, lint.ScopeCommit, []string{hookArgs[1]})
	if err != nil {
		printErr(err)

		return 1
	}

	return policyResultExitCode(cwd, result)
}

func runStagedHook(bundle policy.Bundle, cwd, hookName string) (int, bool) {
	files, err := hookFiles(cwd, hookName)
	if err != nil {
		printErr(err)

		return 1, true
	}

	result, err := runHookPolicy(bundle, cwd, lint.ScopeStaged, files)
	if err != nil {
		printErr(err)

		return 1, true
	}

	code := policyResultExitCode(cwd, result)

	return code, code != 0
}

func runHookPolicy(
	bundle policy.Bundle,
	cwd string,
	scope string,
	files []string,
) (lint.Result, error) {
	result, err := lint.Run(bundle, lint.Options{
		AdminApproved: adminApproved(cwd),
		Scope:         scope,
		Cwd:           cwd,
		Files:         files,
	})
	if err != nil {
		return lint.Result{}, fmt.Errorf("run hook policy: %w", err)
	}

	return result, nil
}

func policyResultExitCode(cwd string, result lint.Result) int {
	lint.EnsureTraceID(&result)
	logLintResult(cwd, result)

	if result.Blocked() {
		encodeLintResult(result)

		return blockedExitCode
	}

	return 0
}

func adminApproved(cwd string) bool {
	return adminApprovedWithVerifier(cwd, gitwrap.VerifyAdminApproved)
}

func adminApprovedWithVerifier(cwd string, verifier func(string) error) bool {
	if os.Getenv(adminApprovedEnv) == "1" {
		return true
	}

	return verifier(cwd) == nil
}

func hookFiles(cwd, hookName string) ([]string, error) {
	if hookName != "pre-commit" {
		return nil, nil
	}

	command := evaluators.GitCommand(
		cwd,
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
		"--",
	)

	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}

	files := []string{}

	for line := range strings.SplitSeq(string(output), "\n") {
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
	err := encodeLintResultTo(os.Stderr, result)
	if err != nil {
		fmt.Fprintf(os.Stderr, "coding-ethos policy blocked %s\n", result.Scope)
	}
}

func logLintResult(cwd string, result lint.Result) {
	_, inlineErrA := lint.LogResult(cwd, result)
	if inlineErrA != nil {
		fmt.Fprintf(os.Stderr, "WARN: lint trace not written: %v\n", inlineErrA)
	}
}

func encodeLintResultTo(writer io.Writer, result lint.Result) error {
	if result.Blocked() {
		result = blockedOnlyResult(result)
	}

	err := hookoutput.EncodeLintResult(writer, result, hookoutput.SelectedFormat())
	if err != nil {
		return fmt.Errorf("encode lint result: %w", err)
	}

	return nil
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

func runHookGroupRunner(cwd string, args []string) int {
	restore := setenvForHookRunner("CODE_ETHOS_CONSUMER_ROOT", cwd)
	defer restore()

	commandArgs := append([]string{"git-hook"}, args...)

	return hookrunnercli.Run(commandArgs)
}

func setenvForHookRunner(name, value string) func() {
	previous, existed := os.LookupEnv(name)
	if strings.TrimSpace(value) != "" {
		_ = os.Setenv(name, value)
	}

	return func() {
		if existed {
			_ = os.Setenv(name, previous)

			return
		}

		_ = os.Unsetenv(name)
	}
}

func printErr(err error) {
	fmt.Fprintf(os.Stderr, "%s\n", err)
}
