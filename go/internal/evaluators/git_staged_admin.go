// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"os/exec"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

var adminOnlyBasenames = map[string]bool{
	".pre-commit-config.yaml": true,
	"pre-commit-config.yaml":  true,
	".importlinter":           true,
	"importlinter":            true,
	".pylintrc":               true,
	"pylintrc":                true,
	"pyproject.toml":          true,
}

var adminOnlyDirs = []string{
	".pre-commit",
	"pre-commit",
}

func EvaluateGitStagedAdminFiles(policyDef policy.Policy, context Context) ([]policy.Decision, error) {
	if len(context.Argv) > 0 && !isGitSubcommand(context.Argv, "commit") {
		return nil, nil
	}
	stagedFiles, err := stagedFiles(context.Cwd)
	if err != nil {
		return nil, err
	}
	blockedFiles := blockedAdminFiles(stagedFiles)
	if len(blockedFiles) == 0 {
		return nil, nil
	}
	decision := policy.NewDecision("block", policyDef)
	decision.Evidence = map[string]any{
		"staged_files": blockedFiles,
	}
	if context.Cwd != "" {
		decision.Evidence["cwd"] = context.Cwd
	}
	return []policy.Decision{decision}, nil
}

func stagedFiles(cwd string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	if cwd != "" {
		cmd.Dir = cwd
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil, nil
	}
	return strings.Split(trimmed, "\n"), nil
}

func blockedAdminFiles(files []string) []string {
	blocked := []string{}
	for _, file := range files {
		if file == "" {
			continue
		}
		if adminOnlyBasenames[filepath.Base(file)] {
			blocked = append(blocked, file)
			continue
		}
		for _, dir := range adminOnlyDirs {
			if strings.HasPrefix(file, dir+"/") || strings.Contains(file, "/"+dir+"/") {
				blocked = append(blocked, file)
				break
			}
		}
	}
	return blocked
}
