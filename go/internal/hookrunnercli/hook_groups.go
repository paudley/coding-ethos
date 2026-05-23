// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const hookPlanBoolTrue = "true"

type hookCommand struct {
	Filter hookFileFilter
	Run    CommandFunc
	Name   string
}

type hookGroup struct {
	Name          string
	Commands      []hookCommand
	ParallelAfter int
}

type hookFileFilter func([]string) []string

func (group hookGroup) matchesFiles(files []string) bool {
	for _, command := range group.Commands {
		if command.Filter == nil {
			return true
		}

		if len(command.Filter(files)) > 0 {
			return true
		}
	}

	return false
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
	const hookPlanTOONHeaderLines = 4

	lines := make([]string, 0, hookPlanTOONHeaderLines+len(plan.Groups))
	lines = append(lines,
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
