// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"strings"
	"testing"
)

func TestFormatHookPlanJSONUsesBooleanFields(t *testing.T) {
	t.Parallel()

	plan := hookPlan{
		OutputFormat:   hookOutputFormatJSON,
		SuccessOutput:  hookSuccessSilent,
		ParallelGroups: hookPlanBoolTrue,
		Groups: []hookPlanGroup{
			{
				Name:     "syntax",
				Enabled:  hookPlanBoolTrue,
				Commands: []string{"yamllint"},
			},
		},
	}

	output := formatHookPlan(plan, hookOutputFormatJSON)
	for _, fragment := range []string{
		`"parallel_groups": true`,
		`"enabled": true`,
		`"commands": [`,
		`"yamllint"`,
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("formatHookPlan() missing %q:\n%s", fragment, output)
		}
	}
}

func TestFormatHookPlanTOONIncludesGroups(t *testing.T) {
	t.Parallel()

	plan := hookPlan{
		OutputFormat:   hookOutputFormatTOON,
		SuccessOutput:  hookSuccessSilent,
		ParallelGroups: hookPlanBoolTrue,
		Groups: []hookPlanGroup{
			{
				Name:     "syntax",
				Enabled:  hookPlanBoolTrue,
				Commands: []string{"yamllint", "shellcheck"},
			},
		},
	}

	output := formatHookPlan(plan, hookOutputFormatTOON)
	for _, fragment := range []string{
		"format: toon",
		"groups[1]{name,enabled,commands}:",
		"syntax,true,yamllint shellcheck",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("formatHookPlan() missing %q:\n%s", fragment, output)
		}
	}
}
