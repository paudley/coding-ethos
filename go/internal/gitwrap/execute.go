// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func Execute(realGit string, options Options) error {
	if realGit == "" {
		realGit = "git"
	}
	normalized := normalizeArgv(options.Argv)
	cmd := exec.Command(realGit, normalized[1:]...)
	if options.Cwd != "" {
		cmd.Dir = options.Cwd
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return ExitCodeError{Code: exitError.ExitCode()}
		}
		return fmt.Errorf("execute git: %w", err)
	}
	return nil
}

func PreparePost(bundle policy.Bundle, options Options) error {
	_, err := evaluatePostPolicies(bundle, options, "PreToolUse")
	return err
}

func VerifyPost(bundle policy.Bundle, options Options) (Result, error) {
	return evaluatePostPolicies(bundle, options, "PostToolUse")
}

func evaluatePostPolicies(bundle policy.Bundle, options Options, scope string) (Result, error) {
	return evaluatePostPoliciesWithRegistry(bundle, options, scope, evaluators.DefaultRegistry())
}

func evaluatePostPoliciesWithRegistry(
	bundle policy.Bundle,
	options Options,
	scope string,
	registry evaluators.Registry,
) (Result, error) {
	argv := normalizeArgv(options.Argv)
	operation := gitOperation(argv)
	if operation == "" {
		return Result{Argv: argv, Status: "allowed"}, nil
	}
	dispatch, ok := bundle.Dispatch.Git[operation]
	if !ok || len(dispatch.Post) == 0 {
		return Result{Argv: argv, Operation: operation, Status: "allowed"}, nil
	}
	decisions := make([]policy.Decision, 0, len(dispatch.Post))
	for _, policyID := range dispatch.Post {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return Result{}, fmt.Errorf("git dispatch %q post references unknown policy %q", operation, policyID)
		}
		evaluated, err := evaluateGitPolicy(policyDef, argv, options.Cwd, scope, registry)
		if err != nil {
			return Result{}, err
		}
		decisions = append(decisions, evaluated...)
	}
	return Result{
		Argv:      argv,
		Operation: operation,
		Status:    resultStatus(decisions),
		Decisions: decisions,
	}, nil
}

type ExitCodeError struct {
	Code int
}

func (err ExitCodeError) Error() string {
	return fmt.Sprintf("git exited with status %d", err.Code)
}
