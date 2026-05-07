// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks_test

import (
	"bytes"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
)

func TestSettingsWireKnownNoopToolsToChecker(t *testing.T) {
	t.Parallel()

	buffer := bytes.Buffer{}

	err := agenthooks.WriteSettings(&buffer, testHookCommand)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	output := buffer.String()

	for _, expected := range []string{
		`"matcher": "Read"`,
		`"matcher": "functions\\.update_plan"`,
		`"matcher": "read_file"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("settings missing known no-op matcher %s:\n%s", expected, output)
		}
	}
}
