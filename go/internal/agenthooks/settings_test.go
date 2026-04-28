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

func TestWriteSettingsIncludesAllProviders(t *testing.T) {
	t.Parallel()

	buffer := bytes.Buffer{}

	err := agenthooks.WriteSettings(&buffer, testHookCommand)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	output := buffer.String()
	for _, expected := range []string{
		`"claude": {`,
		`"codex": {`,
		`"gemini": {`,
		`"PreToolUse"`,
		`"PostToolUse"`,
		`"PreCompact"`,
		`"SessionStart"`,
		`"BeforeTool"`,
		`"run_shell_command"`,
		`"write_file"`,
		`"matcher": "Bash"`,
		`"command": "/repo/pre-commit/hooks/run-go-hook.sh agent-hook"`,
		`"statusMessage": "coding-ethos policy"`,
		`"name": "coding-ethos"`,
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

func TestSyncAndDoctorSettingsWritesAllProviderFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(root)
	for _, path := range []string{
		paths.Claude,
		paths.CodexConfig,
		paths.CodexHooks,
		paths.Gemini,
	} {
		_, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat settings %s: %v", path, statErr)
		}
	}

	err = agenthooks.DoctorSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("doctor settings: %v", err)
	}
}

func TestSyncSettingsPreservesNonHookSettings(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	paths := agenthooks.DefaultSettingsPaths(root)

	err := os.MkdirAll(filepath.Dir(paths.Claude), 0o755)
	if err != nil {
		t.Fatalf("create settings dir: %v", err)
	}

	err = os.WriteFile(
		paths.Claude,
		[]byte(`{"permissions":{"allow":["WebSearch"]},"outputStyle":"Explanatory"}`),
		0o600,
	)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	err = agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	payload, err := os.ReadFile(paths.Claude)
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

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	err = agenthooks.DoctorSettings(root, "/other/run-go-hook.sh agent-hook")
	if err == nil {
		t.Fatal("expected doctor mismatch")
	}
}

func TestDoctorSettingsRejectsDisabledCodexHooksFeature(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(root)

	payload, err := os.ReadFile(paths.CodexConfig)
	if err != nil {
		t.Fatalf("read codex config: %v", err)
	}

	mutated := strings.Replace(
		string(payload),
		`codex_hooks = true`,
		`codex_hooks = false`,
		1,
	)

	overwriteAgentSettings(t, paths.CodexConfig, mutated)

	err = agenthooks.DoctorSettings(root, testHookCommand)
	if err == nil {
		t.Fatal("expected disabled Codex hooks feature mismatch")
	}
}

func TestDoctorSettingsRejectsMissingGeminiHook(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(root)

	payload, err := os.ReadFile(paths.Gemini)
	if err != nil {
		t.Fatalf("read gemini settings: %v", err)
	}

	mutated := strings.Replace(
		string(payload),
		`"command": "/repo/pre-commit/hooks/run-go-hook.sh agent-hook"`,
		`"command": "/other/run-go-hook.sh agent-hook"`,
		1,
	)

	overwriteAgentSettings(t, paths.Gemini, mutated)

	err = agenthooks.DoctorSettings(root, testHookCommand)
	if err == nil {
		t.Fatal("expected wrong Gemini hook command mismatch")
	}
}

func overwriteAgentSettings(t *testing.T, path string, content string) {
	t.Helper()

	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open settings for overwrite: %v", err)
	}
	defer file.Close()

	_, err = file.WriteString(content)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}
}
