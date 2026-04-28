// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

var errGitCheckIgnore = errors.New("git check-ignore failed")

func EvaluateRequiredIgnores(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	paths := stringSliceOption(
		context.EvaluatorOptions,
		"paths",
		defaultRequiredIgnorePaths(),
	)

	missing := make([]string, 0, len(paths))
	for _, path := range paths {
		ignored, err := gitCheckIgnore(context.Cwd, path)
		if err != nil {
			return nil, err
		}
		if !ignored {
			missing = append(missing, path)
		}
	}

	if len(missing) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Evidence = map[string]any{
		"missing_ignores": missing,
	}
	if context.Cwd != "" {
		decision.Evidence["cwd"] = context.Cwd
	}

	return []policy.Decision{decision}, nil
}

func defaultRequiredIgnorePaths() []string {
	return []string{
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	}
}

func gitCheckIgnore(cwd string, path string) (bool, error) {
	cmd := exec.CommandContext(context.Background(), "git", "check-ignore", "--quiet", path)
	if cwd != "" {
		cmd.Dir = cwd
	}

	err := cmd.Run()
	if err == nil {
		return true, nil
	}

	var exitError *exec.ExitError
	if errors.As(err, &exitError) && exitError.ExitCode() == 1 {
		return false, nil
	}

	return false, fmt.Errorf("%w for %q: %w", errGitCheckIgnore, path, err)
}
