// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"
)

type hookGroup struct {
	Name     string
	Commands []CommandFunc
}

func canonicalHookGroups() map[string]hookGroup {
	return map[string]hookGroup{
		"format": {
			Name:     "format",
			Commands: []CommandFunc{runFormatGroupCommand},
		},
		"syntax": {
			Name: "syntax",
			Commands: []CommandFunc{
				checkSyntax,
				checkMergeConflict,
				checkShebangs,
				detectPrivateKey,
				checkLargeFiles,
				runYamllint,
			},
		},
		"python-policy": {
			Name: "python-policy",
			Commands: []CommandFunc{
				checkOptionalReturnsCommand,
				checkCommentSuppressionsCommand,
				checkDirectImportsCommand,
				checkUtilCentralizationCommand,
				checkSecurityPatternsCommand,
				checkCatchAndSilenceCommand,
				checkConditionalImportsCommand,
				checkTypeCheckingImportsCommand,
				checkStructuredLoggingCommand,
				checkSQLCentralizationCommand,
				checkPyprojectIgnoresCommand,
				checkPythonVersionConsistencyCommand,
				checkFileDocstringsCommand,
				checkPytestGateCommand,
			},
		},
		"python-static": {
			Name:     "python-static",
			Commands: []CommandFunc{checkTypeCheckersCommand},
		},
		"docs": {
			Name: "docs",
			Commands: []CommandFunc{
				checkDocstringCoverageCommand,
				checkModuleDocsCommand,
				validateManifestCommand,
				checkPlanCompletionCommand,
			},
		},
		"security": {
			Name: "security",
			Commands: []CommandFunc{
				detectPrivateKey,
				checkSecurityPatternsCommand,
				checkForbiddenStrings,
			},
		},
		"shell": {
			Name: "shell",
			Commands: []CommandFunc{
				runShellcheck,
				checkShellBestPractices,
			},
		},
		"docker": {
			Name:     "docker",
			Commands: []CommandFunc{runHadolint},
		},
		"workflow": {
			Name:     "workflow",
			Commands: []CommandFunc{runActionlint},
		},
		"python-quality": {
			Name: "python-quality",
			Commands: []CommandFunc{
				runPythonComplexity,
				runPythonMaintainability,
				runPythonVulture,
			},
		},
		"go": {
			Name: "go",
			Commands: []CommandFunc{
				runGoFormatCheck,
				runGoVet,
				runGoTests,
				runGolangciLint,
			},
		},
		"ai": {
			Name:     "ai",
			Commands: []CommandFunc{runGeminiCheck},
		},
		"commit-msg": {
			Name: "commit-msg",
			Commands: []CommandFunc{
				checkCommitLint,
				checkCommitAttribution,
			},
		},
	}
}

func runHookGroupCommand(cfg Config, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "Usage: coding-ethos-hook run-group <group> [files...]")

		return 1
	}
	groupName := args[0]
	group, ok := canonicalHookGroups()[groupName]
	if !ok {
		fmt.Fprintf(os.Stderr, "FATAL: unknown hook group %q\n", groupName)

		return 1
	}
	files := args[1:]
	exit := 0
	for _, command := range group.Commands {
		if command(cfg, files) != 0 {
			exit = 1
		}
	}

	return exit
}
