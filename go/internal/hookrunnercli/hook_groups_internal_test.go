// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"strings"
	"testing"
)

func TestFormatHookPlanJSONOmitsEnabledState(t *testing.T) {
	t.Parallel()

	plan := hookPlan{
		OutputFormat:   hookOutputFormatJSON,
		SuccessOutput:  hookSuccessSilent,
		ParallelGroups: hookPlanBoolTrue,
		Groups: []hookPlanGroup{
			{
				Name:     "syntax",
				Commands: []string{"yamllint"},
			},
		},
	}

	output := formatHookPlan(plan, hookOutputFormatJSON)
	for _, fragment := range []string{
		`"parallel_groups": true`,
		`"commands": [`,
		`"yamllint"`,
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("formatHookPlan() missing %q:\n%s", fragment, output)
		}
	}
	if strings.Contains(output, `"enabled"`) {
		t.Fatalf("formatHookPlan() should not expose enabled state:\n%s", output)
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
				Commands: []string{"yamllint", "shellcheck"},
			},
		},
	}

	output := formatHookPlan(plan, hookOutputFormatTOON)
	for _, fragment := range []string{
		"groups[1]{name,commands}:",
		"syntax,yamllint shellcheck",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("formatHookPlan() missing %q:\n%s", fragment, output)
		}
	}
}
