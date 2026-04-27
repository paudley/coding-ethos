// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
)

const testHookCommand = "/repo/pre-commit/hooks/run-go-hook.sh agent-hook"

func TestWriteSettingsIncludesRuntimeCoveredClaudeHooks(t *testing.T) {
	t.Parallel()

	buffer := bytes.Buffer{}

	err := agenthooks.WriteSettings(&buffer, testHookCommand)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	output := buffer.String()
	for _, expected := range []string{
		`"PreToolUse"`,
		`"PostToolUse"`,
		`"matcher": "Bash"`,
		`"matcher": "Write"`,
		`"matcher": "Edit"`,
		`"matcher": "MultiEdit"`,
		`"command": "/repo/pre-commit/hooks/run-go-hook.sh agent-hook"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in settings:\n%s", expected, output)
		}
	}
}

func TestSyncAndDoctorSettings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude", "settings.local.json")

	err := agenthooks.SyncSettings(path, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	_, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat settings: %v", statErr)
	}

	err = agenthooks.DoctorSettings(path, testHookCommand)
	if err != nil {
		t.Fatalf("doctor settings: %v", err)
	}
}

func TestDoctorSettingsRejectsWrongCommand(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "settings.json")

	err := agenthooks.SyncSettings(path, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	err = agenthooks.DoctorSettings(path, "/other/run-go-hook.sh agent-hook")
	if err == nil {
		t.Fatal("expected doctor mismatch")
	}
}
