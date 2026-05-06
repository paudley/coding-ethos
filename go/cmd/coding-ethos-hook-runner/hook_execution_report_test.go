// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"strings"
	"testing"
)

func TestFormatHookExecutionSummaryJSONIncludesTiming(t *testing.T) {
	t.Parallel()

	output := formatHookExecutionSummary([]hookGroupResult{
		{
			Name:       "syntax",
			Status:     statusPass,
			ExitCode:   0,
			DurationMS: 12,
			Commands: []hookCommandResult{
				{Name: "yamllint", Status: statusPass, ExitCode: 0, DurationMS: 5},
			},
		},
		{
			Name:       "python-static",
			Status:     statusFail,
			ExitCode:   1,
			DurationMS: 42,
			Commands: []hookCommandResult{
				{Name: "pyright", Status: statusFail, ExitCode: 1, DurationMS: 42},
			},
		},
	}, hookOutputFormatJSON)

	for _, fragment := range []string{
		`"status": "FAIL"`,
		`"duration_ms": 54`,
		`"failed": 1`,
		`"failed_first": [`,
		`"python-static"`,
		`"commands": [`,
		`"pyright"`,
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("summary JSON missing %q:\n%s", fragment, output)
		}
	}
}

func TestFormatHookExecutionSummaryTOONIncludesOnlyFailures(t *testing.T) {
	t.Parallel()

	output := formatHookExecutionSummary([]hookGroupResult{
		{
			Name:       "syntax",
			Status:     statusFail,
			ExitCode:   1,
			DurationMS: 15,
			Commands: []hookCommandResult{
				{Name: "shellcheck", Status: statusPass, ExitCode: 0, DurationMS: 4},
				{Name: "yamllint", Status: statusFail, ExitCode: 1, DurationMS: 11},
			},
		},
	}, hookOutputFormatTOON)

	for _, fragment := range []string{
		"status: FAIL",
		"failed_checks[1]{name,status}:",
		"yamllint,FAIL",
		"next[1]{action}:",
		"Fix yamllint diagnostics above\\, then rerun the commit.",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("summary TOON missing %q:\n%s", fragment, output)
		}
	}

	for _, unwanted := range []string{
		"shellcheck",
		"duration_ms",
		"failed_groups",
		"commands[",
		"fix_first",
		"syntax,1,15",
	} {
		if strings.Contains(output, unwanted) {
			t.Fatalf("summary TOON included low-value field %q:\n%s", unwanted, output)
		}
	}
}
