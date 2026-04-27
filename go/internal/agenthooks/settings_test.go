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
		`"PreCompact"`,
		`"SessionStart"`,
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

func TestRuntimeHookSpecsAreProviderNeutral(t *testing.T) {
	t.Parallel()

	specs := agenthooks.RuntimeHookSpecs()
	expected := []agenthooks.HookSpec{
		{Event: "PreToolUse", Tool: "Bash"},
		{Event: "PreToolUse", Tool: "Write"},
		{Event: "PreToolUse", Tool: "Edit"},
		{Event: "PreToolUse", Tool: "MultiEdit"},
		{Event: "PostToolUse", Tool: "Bash"},
		{Event: "PreCompact"},
		{Event: "SessionStart"},
	}

	if len(specs) != len(expected) {
		t.Fatalf("expected %d hook specs, got %d: %#v", len(expected), len(specs), specs)
	}

	for index, expectedSpec := range expected {
		if specs[index] != expectedSpec {
			t.Fatalf("spec %d: expected %#v, got %#v", index, expectedSpec, specs[index])
		}
	}
}

func TestParseProviderRejectsUnsupportedProvider(t *testing.T) {
	t.Parallel()

	_, err := agenthooks.ParseProvider("codex")
	if err == nil {
		t.Fatal("expected unsupported provider error")
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

func TestSyncSettingsPreservesNonHookSettings(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), ".claude", "settings.local.json")

	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		t.Fatalf("create settings dir: %v", err)
	}

	err = os.WriteFile(
		path,
		[]byte(`{"permissions":{"allow":["WebSearch"]},"outputStyle":"Explanatory"}`),
		0o600,
	)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err = agenthooks.SyncSettings(path, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}

	output := string(payload)
	for _, expected := range []string{
		`"permissions"`,
		`"WebSearch"`,
		`"outputStyle": "Explanatory"`,
		`"PreToolUse"`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("missing %s in settings:\n%s", expected, output)
		}
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
