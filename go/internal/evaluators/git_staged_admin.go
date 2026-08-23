// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package evaluators

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
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

	mergeParent, inheritedFiles, divergentFiles := mergeParentAdminFiles(
		context.Cwd,
		blockedFiles,
	)
	if len(inheritedFiles) == len(blockedFiles) {
		decision := policy.NewDecision(recordDecision, policyDef)
		decision.Severity = recordDecision
		decision.Message = "Administrative staged files match the current merge parent."
		decision.Evidence = stagedAdminEvidence(inheritedFiles, context.Cwd)
		addMergeParentEvidence(decision.Evidence, mergeParent, inheritedFiles)
		decision.Diagnostics = inheritedAdminDiagnostics(policyDef, inheritedFiles, decision)

		return []policy.Decision{decision}, nil
	}

	if context.AdminApproved {
		decision := policy.NewDecision(recordDecision, policyDef)
		decision.Severity = recordDecision
		decision.Message = "Administrative staged files approved by coding-ethos admin gate."
		decision.Evidence = stagedAdminEvidence(blockedFiles, context.Cwd)
		addMergeParentEvidence(decision.Evidence, mergeParent, inheritedFiles)
		decision.Diagnostics = stagedAdminDiagnostics(policyDef, blockedFiles, decision)

		return []policy.Decision{decision}, nil
	}

	decision := policy.NewDecision(blockDecision, policyDef)
	decision.Suggestion = stagedAdminHandoff(context.Cwd, context.Argv, divergentFiles)
	decision.Evidence = stagedAdminEvidence(divergentFiles, context.Cwd)
	addMergeParentEvidence(decision.Evidence, mergeParent, inheritedFiles)
	decision.Diagnostics = stagedAdminDiagnostics(policyDef, divergentFiles, decision)

	return []policy.Decision{decision}, nil
}

func mergeParentAdminFiles(cwd string, files []string) (string, []string, []string) {
	divergent := append([]string(nil), files...)

	mergeParent, ok := currentMergeParent(cwd)
	if !ok {
		return "", nil, divergent
	}

	baseArgs := []string{"diff", "--cached", "--name-only", mergeParent, "--"}
	args := make([]string, 0, len(baseArgs)+len(files))
	args = append(args, baseArgs...)
	args = append(args, files...)
	cmd := GitCommand(cwd, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", nil, divergent
	}

	changed := stringSet(nonEmptyLines(string(output)))
	inherited := make([]string, 0, len(files))
	divergent = divergent[:0]

	for _, file := range files {
		if changed[file] {
			divergent = append(divergent, file)
		} else {
			inherited = append(inherited, file)
		}
	}

	return mergeParent, inherited, divergent
}

func currentMergeParent(cwd string) (string, bool) {
	cmd := GitCommand(cwd, "rev-parse", "--verify", "MERGE_HEAD^{commit}")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", false
	}

	parents := nonEmptyLines(string(output))
	if len(parents) != 1 {
		return "", false
	}

	return parents[0], true
}

func nonEmptyLines(output string) []string {
	lines := strings.Split(output, "\n")
	result := make([]string, 0, len(lines))

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}

	return result
}

func addMergeParentEvidence(evidence map[string]any, parent string, files []string) {
	if parent == "" || len(files) == 0 {
		return
	}

	evidence["merge_parent"] = parent

	evidence["merge_parent_files"] = append([]string(nil), files...)
}

func stagedAdminHandoff(cwd string, argv, blockedFiles []string) string {
	command := "git commit"
	if len(argv) > 0 {
		command = shellCommand(argv)
	}

	lines := []string{
		"Administrative staged files require a human/admin commit.",
		"Administrative staged files: " + strings.Join(blockedFiles, ", ") + ".",
		"Agent action: stop trying to commit these files.",
	}
	if cwd != "" {
		lines = append(lines, "Human/admin handoff: from "+cwd+", run: "+command+".")
	} else {
		lines = append(lines, "Human/admin handoff: run: "+command+".")
	}

	if !isCodingEthosCheckout(cwd) {
		lines = append(
			lines,
			"Note: --admin-approved is only valid inside the coding-ethos repo admin wrapper.",
		)
	}

	return strings.Join(lines, " ")
}

func shellCommand(argv []string) string {
	return shellquote.Command(argv...)
}

func isCodingEthosCheckout(cwd string) bool {
	if strings.TrimSpace(cwd) == "" {
		return false
	}

	for _, marker := range codingEthosCheckoutMarkers() {
		_, err := os.Stat(filepath.Join(cwd, marker))
		if err != nil {
			return false
		}
	}

	return true
}

func codingEthosCheckoutMarkers() []string {
	return []string{
		"coding_ethos.yml",
		"config.yaml",
		"go/cmd/coding-ethos-run",
	}
}

func stagedAdminEvidence(blockedFiles []string, cwd string) map[string]any {
	evidence := map[string]any{
		"files":        blockedFiles,
		"staged_files": blockedFiles,
	}

	if cwd != "" {
		evidence["cwd"] = cwd
	}

	return evidence
}

func stagedAdminDiagnostics(
	policyDef policy.Policy,
	blockedFiles []string,
	decision policy.Decision,
) []diagnostics.Diagnostic {
	items := make([]diagnostics.Diagnostic, 0, len(blockedFiles))
	for _, file := range blockedFiles {
		items = append(items, diagnostics.Diagnostic{
			Tool:     "git",
			File:     file,
			PolicyID: policyDef.ID,
			Severity: decision.Severity,
			Message:  "Administrative staged file requires explicit handling.",
			Advice: "Commit this administrative file through the " +
				"human/admin handoff path.",
			PrincipleIDs: append([]string(nil), decision.PrincipleIDs...),
		})
	}

	return items
}

func inheritedAdminDiagnostics(
	policyDef policy.Policy,
	files []string,
	decision policy.Decision,
) []diagnostics.Diagnostic {
	items := make([]diagnostics.Diagnostic, 0, len(files))
	for _, file := range files {
		items = append(items, diagnostics.Diagnostic{
			Tool:         "git",
			File:         file,
			PolicyID:     policyDef.ID,
			Severity:     decision.Severity,
			Message:      "Administrative staged file matches the current merge parent.",
			Advice:       "Preserve the merge-parent entry unchanged.",
			PrincipleIDs: append([]string(nil), decision.PrincipleIDs...),
		})
	}

	return items
}

func stagedFiles(cwd string) ([]string, error) {
	cmd := GitCommand(cwd, "diff", "--cached", "--name-only")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf(
			"list staged files with %s (%s=%q): %w: %s",
			cmd.Path,
			RealGitEnv,
			os.Getenv(RealGitEnv),
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
