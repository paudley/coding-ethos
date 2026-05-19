// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/debuglog"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
)

func requiredRuntimeIgnorePaths() []string {
	return []string{
		".code-ethos/cache/",
		".coding-ethos/",
		".coding-ethos/hook-runs/example/stdout.log",
	}
}

func checkRuntimeIgnoresCommand(_ Config, _ []string) int {
	findings := runtimeIgnoreFindings(requiredRuntimeIgnorePaths())
	if len(findings) == 0 {
		return 0
	}

	emitHookReport(os.Stderr, hookReport{
		Tool:     "runtime-ignores",
		Title:    "RUNTIME IGNORE CHECK FAILED",
		Summary:  "Coding-ethos runtime paths must be ignored before hook logs are written.",
		Findings: runtimeIgnoreHookFindings(findings),
		Guidance: []string{
			"Add the reported coding-ethos runtime paths to the repository .gitignore.",
			"Regenerate or sync managed configuration before retrying the hook.",
		},
	}, selectedHookOutputFormat())

	return 1
}

func runtimeIgnoreFindings(paths []string) []string {
	findings := []string{}

	for _, path := range paths {
		cleanPath := strings.TrimSpace(path)
		if cleanPath == "" {
			continue
		}

		if runtimePathIgnored(cleanPath) {
			continue
		}

		findings = append(
			findings,
			cleanPath+" is not ignored; add coding-ethos runtime paths to the repo "+
				".gitignore before hook logs are written",
		)
	}

	return findings
}

func runtimeIgnoreHookFindings(findings []string) []hookFinding {
	hookFindings := make([]hookFinding, 0, len(findings))
	for _, finding := range findings {
		path, _, _ := strings.Cut(finding, " ")
		hookFindings = append(hookFindings, hookFinding{
			Tool:     "runtime-ignores",
			File:     path,
			Severity: "error",
			Code:     "missing-runtime-ignore",
			PolicyID: "runtime.ignored_paths",
			SkillID:  "managed-toolchain",
			Message:  "Coding-ethos runtime path is not ignored.",
			Advice:   "Add this path to .gitignore before hook logs are written.",
			Detail:   finding,
		})
	}

	return hookFindings
}

func runtimePathIgnored(path string) bool {
	cmd := evaluators.GitCommand(repoRoot(), "check-ignore", "--quiet", path)
	argv := []string{"git", "check-ignore", "--quiet", path}

	startedAt := debuglog.ProcessEnter(argv, repoRoot())
	err := cmd.Run()
	debuglog.ProcessExit(startedAt, argv, repoRoot(), commandExitCode(err), err)

	return err == nil || gitignoreContainsPath(repoRoot(), path)
}

func gitignoreContainsPath(root, path string) bool {
	payload, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return false
	}

	normalized := strings.Trim(path, "/")
	for line := range strings.SplitSeq(string(payload), "\n") {
		entry := strings.Trim(strings.TrimSpace(line), "/")
		if entry == "" || strings.HasPrefix(entry, "#") {
			continue
		}

		if entry == normalized || strings.HasPrefix(normalized+"/", entry+"/") {
			return true
		}
	}

	return false
}
