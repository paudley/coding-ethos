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
				{Name: "check-syntax", Status: statusPass, ExitCode: 0, DurationMS: 5},
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
				{Name: "check-syntax", Status: statusPass, ExitCode: 0, DurationMS: 4},
				{Name: "yamllint", Status: statusFail, ExitCode: 1, DurationMS: 11},
			},
		},
	}, hookOutputFormatTOON)

	for _, fragment := range []string{
		"status: FAIL",
		"failed_groups[1]{name,exit_code,duration_ms}:",
		"syntax,1,15",
		"commands[1]{group,name,status,exit_code,duration_ms}:",
		"syntax,yamllint,FAIL,1,11",
		"fix_first[1]{group}:",
	} {
		if !strings.Contains(output, fragment) {
			t.Fatalf("summary TOON missing %q:\n%s", fragment, output)
		}
	}

	if strings.Contains(output, "check-syntax") {
		t.Fatalf("summary TOON included passing command:\n%s", output)
	}
}
