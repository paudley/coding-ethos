// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:funlen // Central hook group table is kept together as the dispatch contract.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const hookPlanBoolTrue = "true"

type hookCommand struct {
	Run  CommandFunc
	Name string
}

type hookGroup struct {
	Name     string
	Commands []hookCommand
}

func canonicalHookGroups() map[string]hookGroup {
	return map[string]hookGroup{
		"format": {
			Name:     "format",
			Commands: []hookCommand{{Name: "format", Run: runFormatGroupCommand}},
		},
		"syntax": {
			Name: "syntax",
			Commands: []hookCommand{
				{Name: "check-runtime-ignores", Run: checkRuntimeIgnoresCommand},
				{Name: "check-syntax", Run: checkSyntax},
				{Name: "check-merge-conflict", Run: checkMergeConflict},
				{Name: "check-shebangs", Run: checkShebangs},
				{Name: "detect-private-key", Run: detectPrivateKey},
				{Name: "check-large-files", Run: checkLargeFiles},
				{Name: "yamllint", Run: runYamllint},
			},
		},
		"python-policy": {
			Name: "python-policy",
			Commands: []hookCommand{
				{Name: "check-optional-returns", Run: checkOptionalReturnsCommand},
				{Name: "check-comment-suppressions", Run: checkCommentSuppressionsCommand},
				{Name: "check-direct-imports", Run: checkDirectImportsCommand},
				{Name: "check-util-centralization", Run: checkUtilCentralizationCommand},
				{Name: "check-security-patterns", Run: checkSecurityPatternsCommand},
				{Name: "check-catch-and-silence", Run: checkCatchAndSilenceCommand},
				{Name: "check-conditional-imports", Run: checkConditionalImportsCommand},
				{Name: "check-type-checking-imports", Run: checkTypeCheckingImportsCommand},
				{Name: "check-structured-logging", Run: checkStructuredLoggingCommand},
				{Name: "check-sql-centralization", Run: checkSQLCentralizationCommand},
				{Name: "check-pyproject-ignores", Run: checkPyprojectIgnoresCommand},
				{
					Name: "check-python-version-consistency",
					Run:  checkPythonVersionConsistencyCommand,
				},
				{Name: "check-file-docstrings", Run: checkFileDocstringsCommand},
				{Name: "check-pytest-gate", Run: checkPytestGateCommand},
			},
		},
		"python-static": {
			Name: "python-static",
			Commands: []hookCommand{
				{Name: "check-type-checkers", Run: checkTypeCheckersCommand},
			},
		},
		"docs": {
			Name: "docs",
			Commands: []hookCommand{
				{Name: "check-docstring-coverage", Run: checkDocstringCoverageCommand},
				{Name: "check-module-docs", Run: checkModuleDocsCommand},
				{Name: "validate-manifest", Run: validateManifestCommand},
				{Name: "check-plan-completion", Run: checkPlanCompletionCommand},
			},
		},
		"security": {
			Name: "security",
			Commands: []hookCommand{
				{Name: "detect-private-key", Run: detectPrivateKey},
				{Name: "check-security-patterns", Run: checkSecurityPatternsCommand},
				{Name: "check-forbidden-strings", Run: checkForbiddenStrings},
			},
		},
		"shell": {
			Name: "shell",
			Commands: []hookCommand{
				{Name: "shellcheck", Run: runShellcheck},
				{Name: "check-shell-best-practices", Run: checkShellBestPractices},
			},
		},
		"docker": {
			Name:     "docker",
			Commands: []hookCommand{{Name: "hadolint", Run: runHadolint}},
		},
		"workflow": {
			Name:     "workflow",
			Commands: []hookCommand{{Name: "actionlint", Run: runActionlint}},
		},
		"python-quality": {
			Name: "python-quality",
			Commands: []hookCommand{
				{Name: "python-complexity", Run: runPythonComplexity},
				{Name: "python-maintainability", Run: runPythonMaintainability},
				{Name: "python-vulture", Run: runPythonVulture},
			},
		},
		"go": {
			Name: "go",
			Commands: []hookCommand{
				{Name: "go-format", Run: runGoFormatCheck},
				{Name: "go-vet", Run: runGoVet},
				{Name: "go-test", Run: runGoTests},
				{Name: "golangci-lint", Run: runGolangciLint},
			},
		},
		"ai": {
			Name:     "ai",
			Commands: []hookCommand{{Name: "gemini-check", Run: runGeminiCheck}},
		},
		"commit-msg": {
			Name: "commit-msg",
			Commands: []hookCommand{
				{Name: "commitlint", Run: checkCommitLint},
				{Name: "commit-attribution", Run: checkCommitAttribution},
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

	result := runHookGroupInProcess(cfg, group, args[1:])
	writeHookGroupResultFile(os.Getenv(hookGroupResultPathEnv), result)

	if os.Getenv(hookGroupChildEnv) != hookPlanBoolTrue &&
		(result.ExitCode != 0 || hookVerboseSuccessOutputEnabled()) {
		writeLine(os.Stdout, formatHookExecutionSummary(
			[]hookGroupResult{result},
			selectedHookOutputFormat(),
		))
	}

	return result.ExitCode
}

type hookPlanGroup struct {
	Name     string   `json:"name"`
	Enabled  string   `json:"enabled"`
	Commands []string `json:"commands"`
}

type hookPlan struct {
	Format         string          `json:"format"`
	OutputFormat   string          `json:"output_format"`
	SuccessOutput  string          `json:"success_output"`
	ParallelGroups string          `json:"parallel_groups"`
	Groups         []hookPlanGroup `json:"groups"`
}

func runHookPlanCommand(_ Config, _ []string) int {
	settings := loadHookSettings()
	plan := buildHookPlan(settings)

	writeLine(os.Stdout, formatHookPlan(plan, selectedHookOutputFormat()))

	return 0
}

func buildHookPlan(settings hookSettings) hookPlan {
	groupNames := defaultHookSettings().EnabledGroups

	enabledNames := map[string]bool{}
	for _, name := range enabledHookGroupNames(groupNames) {
		enabledNames[name] = true
	}

	groups := canonicalHookGroups()

	planGroups := make([]hookPlanGroup, 0, len(groupNames))
	for _, name := range groupNames {
		group, ok := groups[name]
		if !ok {
			continue
		}

		commands := make([]string, 0, len(group.Commands))
		for _, command := range group.Commands {
			commands = append(commands, command.Name)
		}

		planGroups = append(planGroups, hookPlanGroup{
			Name:     name,
			Enabled:  strconv.FormatBool(enabledNames[name]),
			Commands: commands,
		})
	}

	return hookPlan{
		OutputFormat:   selectedHookOutputFormat(),
		SuccessOutput:  selectedHookSuccessOutput(),
		ParallelGroups: strconv.FormatBool(settings.ParallelGroups),
		Groups:         planGroups,
	}
}

func formatHookPlan(plan hookPlan, format string) string {
	plan.Format = format

	switch format {
	case hookOutputFormatJSON:
		data, err := json.MarshalIndent(hookPlanJSONPayload(plan), "", "  ")
		if err != nil {
			return "{}"
		}

		return string(data)
	case hookOutputFormatTOON:
		return formatHookPlanTOON(plan)
	default:
		return formatHookPlanHuman(plan)
	}
}

func hookPlanJSONPayload(plan hookPlan) map[string]any {
	groups := make([]map[string]any, 0, len(plan.Groups))
	for _, group := range plan.Groups {
		groups = append(groups, map[string]any{
			"name":     group.Name,
			"enabled":  group.Enabled == hookPlanBoolTrue,
			"commands": group.Commands,
		})
	}

	return map[string]any{
		"format":          plan.Format,
		"output_format":   plan.OutputFormat,
		"success_output":  plan.SuccessOutput,
		"parallel_groups": plan.ParallelGroups == hookPlanBoolTrue,
		"groups":          groups,
	}
}

func formatHookPlanHuman(plan hookPlan) string {
	lines := []string{
		"HOOK PLAN",
		"output_format: " + plan.OutputFormat,
		"success_output: " + plan.SuccessOutput,
		"parallel_groups: " + plan.ParallelGroups,
		"",
	}

	for _, group := range plan.Groups {
		status := "disabled"
		if group.Enabled == hookPlanBoolTrue {
			status = "enabled"
		}

		lines = append(lines, fmt.Sprintf("%s (%s)", group.Name, status))
		for _, command := range group.Commands {
			lines = append(lines, "  - "+command)
		}
	}

	return strings.Join(lines, "\n")
}

func formatHookPlanTOON(plan hookPlan) string {
	const hookPlanTOONHeaderLines = 5

	lines := make([]string, 0, hookPlanTOONHeaderLines+len(plan.Groups))
	lines = append(lines,
		"format: "+toonCell(plan.Format),
		"output_format: "+toonCell(plan.OutputFormat),
		"success_output: "+toonCell(plan.SuccessOutput),
		"parallel_groups: "+toonCell(plan.ParallelGroups),
		fmt.Sprintf("groups[%d]{name,enabled,commands}:", len(plan.Groups)),
	)

	for _, group := range plan.Groups {
		lines = append(lines, fmt.Sprintf(
			"  %s,%t,%s",
			toonCell(group.Name),
			group.Enabled == hookPlanBoolTrue,
			toonCell(strings.Join(group.Commands, " ")),
		))
	}

	return strings.Join(lines, "\n")
}
