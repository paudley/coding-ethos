// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

import (
	"fmt"
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
	return []string{".pre-commit", "pre-commit", "bin"}
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

	if context.AdminApproved {
		decision := policy.NewDecision(recordDecision, policyDef)
		decision.Severity = recordDecision
		decision.Message = "Administrative staged files approved by coding-ethos admin gate."
		decision.Evidence = stagedAdminEvidence(blockedFiles, context.Cwd)

		return []policy.Decision{decision}, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Suggestion = stagedAdminHandoff(context.Cwd, context.Argv)
	decision.Evidence = stagedAdminEvidence(blockedFiles, context.Cwd)

	return []policy.Decision{decision}, nil
}

func stagedAdminHandoff(cwd string, argv []string) string {
	command := "git commit"
	if len(argv) > 0 {
		command = shellCommand(argv)
	}

	lines := []string{
		"Administrative staged files require a human/admin commit.",
		"Agent action: stop trying to commit these files.",
	}
	if cwd != "" {
		lines = append(lines, "Human/admin handoff: from "+cwd+", run: "+command)
	} else {
		lines = append(lines, "Human/admin handoff: run: "+command)
	}

	lines = append(
		lines,
		"--admin-approved is only valid inside the coding-ethos repo admin wrapper.",
	)

	return strings.Join(lines, " ")
}

func shellCommand(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, arg := range argv {
		parts = append(parts, shellQuote(arg))
	}

	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}

	if !strings.ContainsAny(value, " \t\n'\"\\$`!*?[]{}();<>|&") {
		return value
	}

	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

func stagedAdminEvidence(blockedFiles []string, cwd string) map[string]any {
	evidence := map[string]any{
		"staged_files": blockedFiles,
	}

	if cwd != "" {
		evidence["cwd"] = cwd
	}

	return evidence
}

func stagedFiles(cwd string) ([]string, error) {
	cmd := GitCommand(cwd, "diff", "--cached", "--name-only")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"list staged files: %w: %s",
			err,
			strings.TrimSpace(string(output)),
		)
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
