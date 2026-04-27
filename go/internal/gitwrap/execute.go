// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package gitwrap

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
)

func Execute(realGit string, options Options) error {
	resolvedGit, err := ResolveRealGit(realGit)
	if err != nil {
		return err
	}

	normalized := normalizeArgv(options.Argv)

	cmd := exec.CommandContext(context.Background(), resolvedGit, normalized[1:]...)
	if options.Cwd != "" {
		cmd.Dir = options.Cwd
	}

	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout

	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
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

func evaluatePostPolicies(
	bundle policy.Bundle,
	options Options,
	scope string,
) (Result, error) {
	return evaluatePostPoliciesWithRegistry(
		bundle,
		options,
		scope,
		evaluators.DefaultRegistry(),
	)
}

func evaluatePostPoliciesWithRegistry(
	bundle policy.Bundle,
	options Options,
	scope string,
	registry evaluators.Registry,
) (Result, error) {
	parsed := ParseArgv(options.Argv)
	argv := parsed.Argv

	operation := parsed.Operation
	if operation == "" {
		return Result{Argv: argv, Status: statusAllowed}, nil
	}

	policyIDs := gitPostPolicyIDs(bundle, operation)
	if len(policyIDs) == 0 {
		return Result{Argv: argv, Operation: operation, Status: statusAllowed}, nil
	}

	decisions := make([]policy.Decision, 0, len(policyIDs))
	for _, policyID := range policyIDs {
		policyDef, ok := bundle.Policies[policyID]
		if !ok {
			return Result{}, fmt.Errorf(
				"%w: %q post references %q",
				errUnknownGitPolicy,
				operation,
				policyID,
			)
		}

		evaluated, err := evaluateGitPolicy(
			policyDef,
			argv,
			options.Cwd,
			scope,
			options.AdminApproved,
			registry,
		)
		if err != nil {
			return Result{}, fmt.Errorf("evaluate post policy %q: %w", policyID, err)
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
