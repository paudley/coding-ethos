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

	_, err := agenthooks.ParseProvider("unknown")
	if err == nil {
		t.Fatal("expected unsupported provider error")
	}
}

func TestParseProviderAcceptsSupportedProviders(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude", "codex", "gemini"} {
		provider, err := agenthooks.ParseProvider(name)
		if err != nil {
			t.Fatalf("ParseProvider(%q): %v", name, err)
		}

		if string(provider) != name {
			t.Fatalf("ParseProvider(%q) = %q", name, provider)
		}
	}
}

func TestWriteProviderSettingsIncludesProviderManifests(t *testing.T) {
	t.Parallel()

	for _, provider := range []agenthooks.Provider{
		agenthooks.ProviderCodex,
		agenthooks.ProviderGemini,
	} {
		buffer := bytes.Buffer{}

		err := agenthooks.WriteProviderSettings(&buffer, provider, testHookCommand)
		if err != nil {
			t.Fatalf("write %s settings: %v", provider, err)
		}

		output := buffer.String()
		for _, expected := range []string{
			`"` + string(provider) + `": {`,
			`"version": 1`,
			`"event": "PreToolUse"`,
			`"tool": "Bash"`,
			`"event": "PreCompact"`,
			`"command": "/repo/pre-commit/hooks/run-go-hook.sh agent-hook"`,
		} {
			if !strings.Contains(output, expected) {
				t.Fatalf("missing %s in %s settings:\n%s", expected, provider, output)
			}
		}

		if strings.Contains(output, `"hooks": {`) {
			t.Fatalf("%s settings used Claude-shaped hooks map:\n%s", provider, output)
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

func TestSyncAndDoctorProviderSettings(t *testing.T) {
	t.Parallel()

	for _, provider := range []agenthooks.Provider{
		agenthooks.ProviderCodex,
		agenthooks.ProviderGemini,
	} {
		path := filepath.Join(t.TempDir(), string(provider), "settings.json")

		err := agenthooks.SyncProviderSettings(path, provider, testHookCommand)
		if err != nil {
			t.Fatalf("sync %s settings: %v", provider, err)
		}

		payload, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s settings: %v", provider, err)
		}

		output := string(payload)
		if !strings.Contains(output, `"`+string(provider)+`": {`) ||
			!strings.Contains(output, `"version": 1`) {
			t.Fatalf("provider manifest missing from %s settings:\n%s", provider, output)
		}

		err = agenthooks.DoctorProviderSettings(path, provider, testHookCommand)
		if err != nil {
			t.Fatalf("doctor %s settings: %v", provider, err)
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

func TestDoctorProviderSettingsRejectsWrongCommand(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "codex-settings.json")

	err := agenthooks.SyncProviderSettings(
		path,
		agenthooks.ProviderCodex,
		testHookCommand,
	)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	err = agenthooks.DoctorProviderSettings(
		path,
		agenthooks.ProviderCodex,
		"/other/run-go-hook.sh agent-hook",
	)
	if err == nil {
		t.Fatal("expected doctor mismatch")
	}
}
