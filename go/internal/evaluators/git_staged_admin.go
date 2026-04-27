// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func defaultAdminOnlyBasenames() []string {
	return []string{
		".pre-commit-config.yaml",
		"pre-commit-config.yaml",
		".importlinter",
		"importlinter",
		".pylintrc",
		"pylintrc",
		"pyproject.toml",
	}
}

func defaultAdminOnlyDirs() []string {
	return []string{".pre-commit", "pre-commit"}
}

func EvaluateGitStagedAdminFiles(
	policyDef policy.Policy,
	context Context,
) ([]policy.Decision, error) {
	if len(context.Argv) > 0 && !isGitSubcommand(context.Argv, "commit") {
		return nil, nil
	}

	stagedFiles, err := stagedFiles(context.Cwd)
	if err != nil {
		return nil, err
	}

	blockedFiles := BlockedAdminFiles(
		stagedFiles,
		context.EvaluatorOptions,
	)
	if len(blockedFiles) == 0 {
		return nil, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)

	decision.Evidence = map[string]any{
		"staged_files": blockedFiles,
	}
	if context.Cwd != "" {
		decision.Evidence["cwd"] = context.Cwd
	}

	return []policy.Decision{decision}, nil
}

func stagedFiles(cwd string) ([]string, error) {
	cmd := exec.CommandContext(
		context.Background(),
		"git",
		"diff",
		"--cached",
		"--name-only",
	)
	if cwd != "" {
		cmd.Dir = cwd
	}

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list staged files: %w", err)
	}

	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}

	return strings.Split(trimmed, "\n"), nil
}

func BlockedAdminFiles(files []string, options map[string]any) []string {
	blocked := []string{}
	basenames := stringSet(stringSliceOption(
		options,
		"basenames",
		defaultAdminOnlyBasenames(),
	))
	dirs := stringSliceOption(options, "dirs", defaultAdminOnlyDirs())

	for _, file := range files {
		if file == "" {
			continue
		}

		if basenames[filepath.Base(file)] {
			blocked = append(blocked, file)

			continue
		}

		for _, dir := range dirs {
			if strings.HasPrefix(file, dir+"/") || strings.Contains(file, "/"+dir+"/") {
				blocked = append(blocked, file)

				break
			}
		}
	}

	return blocked
}
