// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
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
		`"capabilities": [`,
		`"coverage": "full"`,
		`"coverage": "partial"`,
		`"provider_limited"`,
		`"unsupported"`,
		`"PreToolUse"`,
		`"PostToolUse"`,
		`"PreCompact"`,
		`"SessionStart"`,
		`"UserPromptSubmit"`,
		`"Stop"`,
		`"SessionEnd"`,
		`"SubagentStart"`,
		`"SubagentStop"`,
		`"run_shell_command"`,
		`"BeforeTool"`,
		`"run_shell_command"`,
		`"write_file"`,
		`"hooksConfig"`,
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

func TestProviderCapabilitiesDocumentProviderLimits(t *testing.T) {
	t.Parallel()

	capabilities := agenthooks.ProviderCapabilities()
	if len(capabilities) != 3 {
		t.Fatalf("capability count mismatch: %#v", capabilities)
	}

	assertCapability(t, capabilities, "claude", "full", "PreToolUse updatedInput rewrite")
	assertCapability(t, capabilities, "claude", "full", "UserPromptSubmit additionalContext")
	assertCapability(t, capabilities, "codex", "partial", "PostToolUse additionalContext")
	assertCapability(t, capabilities, "codex", "partial", "PostToolUse edit verification advice")
	assertCapability(t, capabilities, "codex", "partial", "PreToolUse native command hook")
	assertCapability(t, capabilities, "codex", "partial", "Stop additionalContext")
	assertUnsupported(t, capabilities, "codex", "PreToolUse updatedInput rewrite")
	assertCapability(t, capabilities, "gemini", "partial", "BeforeTool deny")
	assertCapability(t, capabilities, "gemini", "partial", "AfterTool additionalContext")
	assertCapability(t, capabilities, "gemini", "partial", "BeforeAgent additionalContext")
	assertCapability(t, capabilities, "gemini", "partial", "SessionEnd additionalContext")
	assertUnsupported(t, capabilities, "gemini", "PostToolBatch additionalContext")
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
		{Event: "PostToolUse", Tool: "Write"},
		{Event: "PostToolUse", Tool: "Edit"},
		{Event: "PostToolUse", Tool: "MultiEdit"},
		{Event: "PostToolBatch"},
		{Event: "PreCompact"},
		{Event: "SessionStart"},
		{Event: "UserPromptSubmit"},
		{Event: "Stop"},
		{Event: "SessionEnd"},
		{Event: "SubagentStart"},
		{Event: "SubagentStop"},
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

func TestGeminiSettingsDoNotClaimUnsupportedPostToolUse(t *testing.T) {
	t.Parallel()

	buffer := bytes.Buffer{}

	err := agenthooks.WriteSettings(&buffer, testHookCommand)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	output := buffer.String()

	geminiIndex := strings.Index(output, `"gemini": {`)
	if geminiIndex == -1 {
		t.Fatalf("missing gemini settings:\n%s", output)
	}

	geminiSettings := output[geminiIndex:]
	if strings.Contains(geminiSettings, `"PostToolUse"`) {
		t.Fatalf("Gemini must not claim unsupported PostToolUse:\n%s", output)
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
		paths.Gemini,
	} {
		_, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat settings %s: %v", path, statErr)
		}
	}

	if _, statErr := os.Stat(paths.CodexHooks); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("stale Codex hooks JSON should not exist: %v", statErr)
	}

	err = agenthooks.DoctorSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("doctor settings: %v", err)
	}
}

func TestSyncAndVerifySettingsRunsProviderSmokePayloads(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hookCommand := fakeAgentHookCommand(t)
	writeGeneratedSkillSurfaces(t, root, "conditional-imports")

	err := agenthooks.SyncSettings(root, hookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	report, err := agenthooks.VerifySettings(root, hookCommand)
	if err != nil {
		t.Fatalf("verify settings: %v", err)
	}

	if report.Status != "valid" {
		t.Fatalf("status = %q, want valid: %#v", report.Status, report)
	}

	if len(report.Checks) != 14 {
		t.Fatalf("check count = %d, want 14: %#v", len(report.Checks), report.Checks)
	}

	for _, check := range report.Checks {
		if check.Status != "pass" {
			t.Fatalf("failed check: %#v", check)
		}
	}
}

func TestVerifySettingsRejectsMissingProviderSkillSurface(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hookCommand := fakeAgentHookCommand(t)
	writeGeneratedSkillSurfaces(t, root, "managed-toolchain")

	err := agenthooks.SyncSettings(root, hookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	missingPath := filepath.Join(
		root,
		".codex",
		"skills",
		"managed-toolchain",
		"SKILL.md",
	)
	if err := os.Remove(missingPath); err != nil {
		t.Fatalf("remove codex skill surface: %v", err)
	}

	report, err := agenthooks.VerifySettings(root, hookCommand)
	if err == nil {
		t.Fatal("expected missing provider skill surface failure")
	}
	if report.Status != "invalid" {
		t.Fatalf("status = %q, want invalid: %#v", report.Status, report)
	}

	found := false
	for _, check := range report.Checks {
		if check.Event == "skill-surface" &&
			check.Provider == "codex" &&
			check.Tool == "managed-toolchain" &&
			check.Status == "fail" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing failed skill-surface check: %#v", report.Checks)
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

func writeGeneratedSkillSurfaces(t *testing.T, root string, skillID string) {
	t.Helper()

	content := "name: " + skillID + "\nsource: coding_ethos.yml\n"
	paths := []string{
		filepath.Join(root, ".agents", "skills", skillID, "SKILL.md"),
		filepath.Join(root, ".claude", "skills", skillID, "SKILL.md"),
		filepath.Join(root, ".codex", "skills", skillID, "SKILL.md"),
		filepath.Join(
			root,
			".gemini",
			"extensions",
			"coding-ethos",
			"skills",
			skillID,
			"SKILL.md",
		),
	}

	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create skill dir %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write skill surface %s: %v", path, err)
		}
	}
}

func assertCapability(
	t *testing.T,
	capabilities []agenthooks.ProviderCapability,
	provider string,
	coverage string,
	supported string,
) {
	t.Helper()

	capability := findCapability(t, capabilities, provider)
	if capability.Coverage != coverage {
		t.Fatalf("%s coverage = %q, want %q", provider, capability.Coverage, coverage)
	}

	if !containsString(capability.Supported, supported) {
		t.Fatalf("%s missing supported capability %q: %#v", provider, supported, capability)
	}
}

func assertUnsupported(
	t *testing.T,
	capabilities []agenthooks.ProviderCapability,
	provider string,
	unsupported string,
) {
	t.Helper()

	capability := findCapability(t, capabilities, provider)
	if !containsString(capability.Unsupported, unsupported) {
		t.Fatalf(
			"%s missing unsupported capability %q: %#v",
			provider,
			unsupported,
			capability,
		)
	}
}

func findCapability(
	t *testing.T,
	capabilities []agenthooks.ProviderCapability,
	provider string,
) agenthooks.ProviderCapability {
	t.Helper()

	for _, capability := range capabilities {
		if capability.Provider == provider {
			return capability
		}
	}

	t.Fatalf("missing provider capability %q: %#v", provider, capabilities)

	return agenthooks.ProviderCapability{}
}

func containsString(values []string, expected string) bool {
	return slices.Contains(values, expected)
}

func fakeAgentHookCommand(t *testing.T) string {
	t.Helper()

	script := filepath.Join(t.TempDir(), "agent-hook")
	err := os.WriteFile(
		script,
		[]byte(`#!/bin/sh
payload="$(cat)"
case "$payload" in
  *'coding-ethos-git-hook'*)
    case "$payload" in
      *'"provider": "gemini-cli"'*)
        printf '%s\n' '{"decision":"deny","systemMessage":"denied by coding-ethos"}'
        exit 2
        ;;
      *)
        printf '%s\n' '{"decision":"block","systemMessage":"blocked by coding-ethos"}'
        exit 2
        ;;
    esac
    ;;
  *'"provider": "claude"'*)
    printf '%s\n' '{"hookSpecificOutput":{"updatedInput":{"command":"'\''pwd'\'' && /repo/pre-commit/hooks/run-go-hook.sh policy-git '\''status'\'' '\''--short'\'' 2>&1"}}}'
    ;;
  *'"UserPromptSubmit"'*)
    printf '%s\n' '{"hookSpecificOutput":{"additionalContext":"coding-ethos prompt guidance"}}'
    ;;
  *'"provider": "codex"'*)
    printf '%s\n' '{"decision":"block","systemMessage":"blocked by coding-ethos"}'
    exit 2
    ;;
  *'"toolName": "write_file"'*)
    printf '%s\n' '{"decision":"deny","systemMessage":"denied by coding-ethos"}'
    exit 2
    ;;
  *'"provider": "gemini-cli"'*)
    printf '%s\n' '{"decision":"deny","systemMessage":"denied by coding-ethos"}'
    exit 2
    ;;
  *)
    printf '%s\n' '{"decision":"unknown"}'
    exit 1
    ;;
esac
`),
		0o700,
	)
	if err != nil {
		t.Fatalf("write fake agent hook: %v", err)
	}

	return "'" + strings.ReplaceAll(script, "'", "'\\''") + "'"
}
