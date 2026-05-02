// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"errors"
	"fmt"
	"os/exec"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

var errGitCheckIgnore = errors.New("git check-ignore failed")

func EvaluateRequiredIgnores(
	policyDef policy.Policy,
	evalContext Context,
) ([]policy.Decision, error) {
	paths := stringSliceOption(
		evalContext.EvaluatorOptions,
		"paths",
		defaultRequiredIgnorePaths(),
	)

	missing := make([]string, 0, len(paths))
	for _, path := range paths {
		ignored, err := gitCheckIgnore(evalContext.Cwd, path)
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
	if evalContext.Cwd != "" {
		decision.Evidence["cwd"] = evalContext.Cwd
	}

	return []policy.Decision{decision}, nil
}

func defaultRequiredIgnorePaths() []string {
	return []string{
		".code-ethos/cache/",
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	}
}

func gitCheckIgnore(cwd string, path string) (bool, error) {
	cmd := gitCommand(cwd, "check-ignore", "--quiet", path)
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
