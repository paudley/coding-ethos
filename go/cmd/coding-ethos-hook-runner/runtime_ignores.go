// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
	"strings"
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

	for _, finding := range findings {
		fmt.Fprintln(os.Stderr, finding)
	}

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

func runtimePathIgnored(path string) bool {
	result := runExternalTool(externalToolRequest{
		Name:    "git-check-ignore",
		Dir:     repoRoot(),
		Command: []string{"git", "check-ignore", "--quiet", path},
	})

	return result.RunnerFailure == nil && result.ExitCode == 0
}
