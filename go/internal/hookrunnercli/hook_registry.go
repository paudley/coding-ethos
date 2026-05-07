// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

type hookCommandRegistry struct {
	Commands map[string]CommandFunc
	Groups   map[string]hookGroup
}

func defaultHookCommandRegistry() hookCommandRegistry {
	commands := canonicalHookCommands()

	return hookCommandRegistry{
		Commands: commands,
		Groups:   canonicalHookGroupsFromCommands(commands),
	}
}

func canonicalHookCommands() map[string]CommandFunc {
	return map[string]CommandFunc{
		"actionlint":                       runActionlint,
		"check-catch-and-silence":          checkCatchAndSilenceCommand,
		"check-comment-suppressions":       checkCommentSuppressionsCommand,
		"check-conditional-imports":        checkConditionalImportsCommand,
		"check-direct-imports":             checkDirectImportsCommand,
		"check-docstring-coverage":         checkDocstringCoverageCommand,
		"check-file-docstrings":            checkFileDocstringsCommand,
		"check-module-docs":                checkModuleDocsCommand,
		"check-optional-returns":           checkOptionalReturnsCommand,
		"check-plan-completion":            checkPlanCompletionCommand,
		"check-pytest-gate":                checkPytestGateCommand,
		"check-python-version-consistency": checkPythonVersionConsistencyCommand,
		"check-runtime-ignores":            checkRuntimeIgnoresCommand,
		"check-security-patterns":          checkSecurityPatternsCommand,
		"check-sql-centralization":         checkSQLCentralizationCommand,
		"check-structured-logging":         checkStructuredLoggingCommand,
		"check-type-checkers":              checkTypeCheckersCommand,
		"check-type-checking-imports":      checkTypeCheckingImportsCommand,
		"check-util-centralization":        checkUtilCentralizationCommand,
		"config-get":                       configGet,
		"fix-text":                         fixText,
		"format":                           runFormatGroupCommand,
		"gemini-check":                     runGeminiCheck,
		"git-hook":                         runGitHookCommand,
		"go-format":                        runGoFormatCheck,
		"go-test":                          runGoTests,
		"go-vet":                           runGoVet,
		"hadolint":                         runHadolint,
		"hook-log-analyze":                 hookLogAnalyzeCommand,
		"hook-log-summary":                 hookLogSummaryCommand,
		"hook-plan":                        runHookPlanCommand,
		"policy-bandit":                    runBandit,
		"policy-dotenv-linter":             runDotenvLinter,
		"policy-golangci-lint":             runGolangciLint,
		"policy-sqlfluff":                  runSQLFluff,
		"policy-tombi":                     runTombi,
		"python-complexity":                runPythonComplexity,
		"python-maintainability":           runPythonMaintainability,
		"python-vulture":                   runPythonVulture,
		"quiet-filter":                     quietFilter,
		"run-group":                        runHookGroupCommand,
		"shellcheck":                       runShellcheck,
		"shfmt":                            runShfmt,
		"validate-manifest":                validateManifestCommand,
		"yamllint":                         runYamllint,
	}
}

func canonicalHookGroups() map[string]hookGroup {
	return defaultHookCommandRegistry().Groups
}

func canonicalHookGroupsFromCommands(
	commands map[string]CommandFunc,
) map[string]hookGroup {
	return map[string]hookGroup{
		"format": groupFromCommandNames(commands, "format", []string{"format"}),
		"syntax": groupFromCommandNames(commands, "syntax", []string{
			"check-runtime-ignores",
			"yamllint",
			"policy-tombi",
			"policy-dotenv-linter",
		}),
		"python-policy": groupFromCommandNames(commands, "python-policy", []string{
			"check-optional-returns",
			"check-comment-suppressions",
			"check-direct-imports",
			"check-util-centralization",
			"check-security-patterns",
			"check-catch-and-silence",
			"check-conditional-imports",
			"check-type-checking-imports",
			"check-structured-logging",
			"check-sql-centralization",
			"check-python-version-consistency",
			"check-file-docstrings",
			"check-pytest-gate",
		}),
		"python-static": groupFromCommandNames(commands, "python-static", []string{
			"check-type-checkers",
		}),
		"docs": groupFromCommandNames(commands, "docs", []string{
			"check-docstring-coverage",
			"check-module-docs",
			"validate-manifest",
			"check-plan-completion",
		}),
		"security": groupFromCommandNames(commands, "security", []string{
			"check-security-patterns",
			"policy-bandit",
		}),
		"sql": groupFromCommandNames(commands, "sql", []string{"policy-sqlfluff"}),
		"shell": groupFromCommandNames(
			commands,
			"shell",
			[]string{"shfmt", "shellcheck"},
		),
		"docker":   groupFromCommandNames(commands, "docker", []string{"hadolint"}),
		"workflow": groupFromCommandNames(commands, "workflow", []string{"actionlint"}),
		"python-quality": groupFromCommandNames(commands, "python-quality", []string{
			"python-complexity",
			"python-maintainability",
			"python-vulture",
		}),
		"go": groupFromCommandNames(commands, "go", []string{
			"go-format",
			"go-vet",
			"go-test",
			"policy-golangci-lint",
		}),
		"ai": groupFromCommandNames(commands, "ai", []string{"gemini-check"}),
	}
}

func groupFromCommandNames(
	commands map[string]CommandFunc,
	groupName string,
	commandNames []string,
) hookGroup {
	group := hookGroup{
		Name:     groupName,
		Commands: make([]hookCommand, 0, len(commandNames)),
	}
	for _, name := range commandNames {
		group.Commands = append(group.Commands, hookCommand{
			Name: displayHookCommandName(name),
			Run:  commands[name],
		})
	}

	return group
}

func displayHookCommandName(name string) string {
	switch name {
	case "policy-bandit":
		return "bandit"
	case "policy-dotenv-linter":
		return "dotenv-linter"
	case "policy-golangci-lint":
		return "golangci-lint"
	case "policy-sqlfluff":
		return "sqlfluff"
	case "policy-tombi":
		return "tombi"
	default:
		return name
	}
}
