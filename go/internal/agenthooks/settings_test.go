// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/hooks"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/syncstate"
)

const testHookCommand = "/repo/bin/coding-ethos-run agent-hook"

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
		`"display_name": "Claude Code"`,
		`"provider": "generic"`,
		`"coverage": "full"`,
		`"coverage": "partial"`,
		`"coverage": "unsupported"`,
		`"block_response_shape"`,
		`"context_advice_shape"`,
		`"verification_fixture"`,
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
		`"command": "/repo/bin/coding-ethos-run agent-hook"`,
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
	if len(capabilities) != 4 {
		t.Fatalf("capability count mismatch: %#v", capabilities)
	}

	for _, expected := range providerCapabilityExpectations() {
		assertCapability(
			t,
			capabilities,
			expected.provider,
			expected.status,
			expected.capability,
		)
	}

	assertUnsupported(
		t,
		capabilities,
		string(agenthooks.ProviderCodex),
		"PreToolUse updatedInput rewrite",
	)
	assertUnsupported(
		t,
		capabilities,
		string(agenthooks.ProviderGemini),
		"PostToolBatch additionalContext",
	)
	assertUnsupported(
		t,
		capabilities,
		string(agenthooks.ProviderGeneric),
		"native hook settings generation",
	)
}

type providerCapabilityExpectation struct {
	provider   string
	status     string
	capability string
}

func providerCapabilityExpectations() []providerCapabilityExpectation {
	return []providerCapabilityExpectation{
		{string(agenthooks.ProviderClaude), "full", "PreToolUse updatedInput rewrite"},
		{string(agenthooks.ProviderClaude), "full", "UserPromptSubmit additionalContext"},
		{string(agenthooks.ProviderClaude), "full", "MCP stdio server"},
		{string(agenthooks.ProviderCodex), "partial", "PreToolUse native command hook"},
		{
			string(agenthooks.ProviderCodex),
			"partial",
			"PreToolUse apply_patch/edit policy hook",
		},
		{
			string(agenthooks.ProviderCodex),
			"partial",
			"PostToolUse compact additionalContext",
		},
		{string(agenthooks.ProviderCodex), "partial", "PostToolUse edit verification advice"},
		{string(agenthooks.ProviderCodex), "partial", "SessionStart additionalContext"},
		{string(agenthooks.ProviderCodex), "partial", "UserPromptSubmit additionalContext"},
		{string(agenthooks.ProviderCodex), "partial", "Stop compact systemMessage"},
		{string(agenthooks.ProviderCodex), "partial", "MCP stdio server"},
		{string(agenthooks.ProviderGemini), "partial", "BeforeTool deny"},
		{string(agenthooks.ProviderGemini), "partial", "PreToolUse updatedInput rewrite"},
		{string(agenthooks.ProviderGemini), "partial", "AfterTool additionalContext"},
		{string(agenthooks.ProviderGemini), "partial", "BeforeAgent additionalContext"},
		{string(agenthooks.ProviderGemini), "partial", "SessionEnd additionalContext"},
		{string(agenthooks.ProviderGemini), "partial", "MCP stdio server"},
		{string(agenthooks.ProviderGeneric), "unsupported", "portable skill surfaces"},
	}
}

func TestProviderCapabilityMatrixSyncAndCheckDetectDrift(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	written, err := agenthooks.SyncProviderCapabilityMatrix(root)
	if err != nil {
		t.Fatalf("sync provider matrix: %v", err)
	}

	if len(written) != 1 ||
		filepath.Base(written[0]) != "PROVIDER_CAPABILITY_MATRIX.md" {
		t.Fatalf("written provider matrix paths = %#v", written)
	}

	info, err := os.Stat(written[0])
	if err != nil {
		t.Fatalf("stat provider matrix: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("provider matrix mode = %s, want -rw-r--r--", info.Mode())
	}

	mismatched, err := agenthooks.CheckProviderCapabilityMatrix(root)
	if err != nil {
		t.Fatalf("check provider matrix: %v", err)
	}

	if len(mismatched) != 0 {
		t.Fatalf("provider matrix drift after sync = %#v", mismatched)
	}

	err = os.WriteFile(written[0], []byte("drift\n"), 0o600)
	if err != nil {
		t.Fatalf("write provider matrix drift: %v", err)
	}

	mismatched, err = agenthooks.CheckProviderCapabilityMatrix(root)
	if err != nil {
		t.Fatalf("check provider matrix after drift: %v", err)
	}

	if len(mismatched) != 1 || mismatched[0] != written[0] {
		t.Fatalf("provider matrix drift = %#v, want %s", mismatched, written[0])
	}
}

func TestProviderCapabilityMatrixDocsStayInSync(t *testing.T) {
	t.Parallel()

	path := filepath.Join(
		"..",
		"..",
		"..",
		filepath.FromSlash(agenthooks.ProviderCapabilityMatrixRelativePath),
	)

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read provider matrix doc: %v", err)
	}

	expected := agenthooks.ProviderCapabilityMatrixMarkdown()
	if string(current) != expected {
		t.Fatalf(
			"provider matrix doc drifted; run make sync-provider-matrix",
		)
	}
}

func TestProviderCapabilitiesMatchUpdatedInputBehavior(t *testing.T) {
	t.Parallel()

	for _, provider := range providersWithCapability(
		agenthooks.ProviderCapabilities(),
		"PreToolUse updatedInput rewrite",
	) {
		t.Run(provider, func(t *testing.T) {
			t.Parallel()

			event, err := hooks.DecodeEvent(strings.NewReader(
				capabilityProbePayload(provider, t.TempDir()),
			))
			if err != nil {
				t.Fatalf("decode %s probe: %v", provider, err)
			}

			result, err := hooks.Run(
				policy.ExampleBundle(),
				hooks.Options{Event: event},
			)
			if err != nil {
				t.Fatalf("run %s probe: %v", provider, err)
			}

			if result.Status != "allowed" ||
				result.HookSpecificOutput == nil ||
				len(result.HookSpecificOutput.UpdatedInput) == 0 {
				t.Fatalf("capability drift for %s: %#v", provider, result)
			}
		})
	}
}

func TestStateArtifactsDescribeManagedHookSurfaces(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	artifacts, err := agenthooks.StateArtifacts(root, testHookCommand)
	if err != nil {
		t.Fatalf("state artifacts: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(root)
	expected := map[string]string{
		filepath.ToSlash(filepath.Join(".claude", "settings.local.json")): "claude-settings",
		filepath.ToSlash(".mcp.json"):                                     "claude-mcp",
		filepath.ToSlash(filepath.Join(".codex", "config.toml")):          "codex-config",
		filepath.ToSlash(filepath.Join(".gemini", "settings.json")):       "gemini-settings",
	}
	if len(artifacts) != len(expected) {
		t.Fatalf(
			"artifact count = %d, want %d: %#v",
			len(artifacts),
			len(expected),
			artifacts,
		)
	}

	for _, artifact := range artifacts {
		surface, found := expected[artifact.Path]
		if !found {
			t.Fatalf("unexpected artifact path %q from %#v", artifact.Path, paths)
		}
		if artifact.Provider != "agent-hooks" ||
			artifact.Surface != surface ||
			artifact.Ownership != syncstate.DefaultOwnership ||
			artifact.VerificationCommand != "bin/coding-ethos-run agent-hooks doctor" ||
			!strings.HasPrefix(artifact.ExpectedSHA256, "sha256:") {
			t.Fatalf("artifact = %#v", artifact)
		}
		delete(expected, artifact.Path)
	}
	if len(expected) != 0 {
		t.Fatalf("missing artifacts: %#v", expected)
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
		t.Fatalf(
			"expected %d hook specs, got %d: %#v",
			len(expected),
			len(specs),
			specs,
		)
	}

	for index, expectedSpec := range expected {
		if specs[index] != expectedSpec {
			t.Fatalf(
				"spec %d: expected %#v, got %#v",
				index,
				expectedSpec,
				specs[index],
			)
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

	geminiSettings := providerSettingsSection(t, output, "gemini", "capabilities")
	if strings.Contains(geminiSettings, `"PostToolUse"`) {
		t.Fatalf("Gemini must not claim unsupported PostToolUse:\n%s", output)
	}
}

func TestCodexSettingsInstallEnforcementAndCompactPostToolHooks(t *testing.T) {
	t.Parallel()

	buffer := bytes.Buffer{}

	err := agenthooks.WriteSettings(&buffer, testHookCommand)
	if err != nil {
		t.Fatalf("write settings: %v", err)
	}

	output := buffer.String()

	codexSettings := providerSettingsSection(t, output, "codex", "gemini")
	codexShellMatcher := `"matcher": "Bash|bash|exec_command|` +
		`functions\\.exec_command|run_command|run_shell|run_shell_command|` +
		`shell|shell_command|write_stdin|functions\\.write_stdin|` +
		`multi_tool_use\\.parallel"`

	for _, expected := range []string{
		`"PreToolUse"`,
		`"PostToolUse"`,
		`"SessionStart"`,
		`"UserPromptSubmit"`,
		`"Stop"`,
		codexShellMatcher,
		`"matcher": "Edit|apply_patch|functions\\.apply_patch|edit_file"`,
		`"matcher": "functions\\.update_plan"`,
		`"statusMessage": "coding-ethos policy"`,
	} {
		if !strings.Contains(codexSettings, expected) {
			t.Fatalf("Codex settings missing %s:\n%s", expected, codexSettings)
		}
	}

	for _, unsupported := range []string{
		`"PostToolBatch"`,
		`"SessionEnd"`,
		`"SubagentStart"`,
		`"SubagentStop"`,
	} {
		if strings.Contains(codexSettings, unsupported) {
			t.Fatalf(
				"Codex must not install context-only hook %s:\n%s",
				unsupported,
				codexSettings,
			)
		}
	}
}

func TestCodexManagedConfigUsesExplicitNonOverlappingHooks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, "bin/coding-ethos-run agent-hook")
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	configPath := agenthooks.DefaultSettingsPaths(root).CodexConfig

	payload, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read Codex config: %v", err)
	}

	config := string(payload)
	if strings.Contains(config, "PATH=") {
		t.Fatalf("generated Codex config must not inline PATH mutations:\n%s", config)
	}

	for _, event := range []string{"PreToolUse", "PostToolUse"} {
		block := codexEventBlock(t, config, event)
		shellMatcher := "Bash|bash|exec_command|functions\\\\.exec_command|" +
			"run_command|run_shell|run_shell_command|shell|shell_command|" +
			"write_stdin|functions\\\\.write_stdin|multi_tool_use\\\\.parallel"
		assertCodexMatcherCount(
			t,
			block,
			event,
			shellMatcher,
		)
		assertCodexMatcherCount(t, block, event, "Write|create_file|write_file")
		assertCodexMatcherCount(
			t,
			block,
			event,
			"Edit|apply_patch|functions\\\\.apply_patch|edit_file",
		)
		assertCodexMatcherCount(t, block, event, "MultiEdit")
		assertCodexMatcherCount(t, block, event, "functions\\\\.update_plan")

		if strings.Contains(block, "{ hooks =") {
			t.Fatalf("%s must not include catch-all command hooks:\n%s", event, block)
		}
	}

	for _, event := range []string{"SessionStart", "UserPromptSubmit", "Stop"} {
		block := codexEventBlock(t, config, event)
		if strings.Contains(block, "matcher =") {
			t.Fatalf(
				"%s lifecycle hook must not have a tool matcher:\n%s",
				event,
				block,
			)
		}

		if count := strings.Count(block, "command = "); count != 1 {
			t.Fatalf("%s command count = %d, want 1:\n%s", event, count, block)
		}
	}
}

func TestSyncSettingsWritesMCPServersForAllProviders(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(root)
	claudeMCP := readJSONSettings(t, paths.ClaudeMCP)
	assertMCPServer(t, claudeMCP, "/repo/bin/coding-ethos-run", true)

	geminiSettings := readJSONSettings(t, paths.Gemini)
	assertMCPServer(t, geminiSettings, "/repo/bin/coding-ethos-run", false)

	codexConfig, err := os.ReadFile(paths.CodexConfig)
	if err != nil {
		t.Fatalf("read Codex config: %v", err)
	}

	codex := string(codexConfig)
	for _, expected := range []string{
		`[mcp_servers.coding-ethos]`,
		`command = "/repo/bin/coding-ethos-run"`,
		`args = ["mcp"]`,
	} {
		if !strings.Contains(codex, expected) {
			t.Fatalf("Codex MCP config missing %s:\n%s", expected, codex)
		}
	}

	if strings.Contains(codex, "enabled = true") {
		t.Fatalf(
			"Codex MCP config should use the documented minimal schema:\n%s",
			codex,
		)
	}
}

func TestSyncCodexTrustStateWritesExpectedProjectHookHashes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userConfig := filepath.Join(t.TempDir(), "config.toml")

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	err = agenthooks.SyncCodexTrustState(root, testHookCommand, userConfig)
	if err != nil {
		t.Fatalf("sync Codex trust: %v", err)
	}

	err = agenthooks.VerifyCodexTrustState(root, testHookCommand, userConfig)
	if err != nil {
		t.Fatalf("verify Codex trust: %v", err)
	}

	payload, err := os.ReadFile(userConfig)
	if err != nil {
		t.Fatalf("read Codex user config: %v", err)
	}

	config := string(payload)
	projectConfig := filepath.Join(root, ".codex", "config.toml")
	for _, expected := range []string{
		`[hooks.state]`,
		`[hooks.state."` + projectConfig + `:pre_tool_use:0:0"]`,
		`[hooks.state."` + projectConfig + `:session_start:0:0"]`,
		`trusted_hash = "sha256:`,
	} {
		if !strings.Contains(config, expected) {
			t.Fatalf("Codex trust config missing %s:\n%s", expected, config)
		}
	}
}

func TestVerifyCodexTrustStateRejectsMissingTrust(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	userConfig := filepath.Join(t.TempDir(), "config.toml")

	inlineErr := os.WriteFile(userConfig, []byte("[hooks.state]\n"), 0o600)
	if inlineErr != nil {
		t.Fatalf("write Codex user config: %v", inlineErr)
	}

	err := agenthooks.VerifyCodexTrustState(root, testHookCommand, userConfig)
	if err == nil ||
		!strings.Contains(err.Error(), "does not trust generated project hooks") {
		t.Fatalf("VerifyCodexTrustState error = %v", err)
	}
}

func providerSettingsSection(
	t *testing.T,
	output string,
	provider string,
	nextProvider string,
) string {
	t.Helper()

	start := strings.Index(output, `"`+provider+`": {`)
	if start == -1 {
		t.Fatalf("missing %s settings:\n%s", provider, output)
	}

	end := strings.Index(output[start:], `"`+nextProvider+`":`)
	if end == -1 {
		t.Fatalf("missing %s settings after %s:\n%s", nextProvider, provider, output)
	}

	return output[start : start+end]
}

func readJSONSettings(t *testing.T, path string) map[string]any {
	t.Helper()

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read JSON settings %s: %v", path, err)
	}

	settings := map[string]any{}

	err = json.Unmarshal(payload, &settings)
	if err != nil {
		t.Fatalf("parse JSON settings %s: %v\n%s", path, err, string(payload))
	}

	return settings
}

func assertMCPServer(
	t *testing.T,
	settings map[string]any,
	command string,
	expectType bool,
) {
	t.Helper()

	servers, found := settings["mcpServers"].(map[string]any)
	if !found {
		t.Fatalf("missing mcpServers: %#v", settings)
	}

	server, found := servers["coding-ethos"].(map[string]any)
	if !found {
		t.Fatalf("missing coding-ethos MCP server: %#v", servers)
	}

	if server["command"] != command {
		t.Fatalf("command = %#v, want %q: %#v", server["command"], command, server)
	}

	args, found := server["args"].([]any)
	if !found || len(args) != 1 || args[0] != "mcp" {
		t.Fatalf("args mismatch: %#v", server)
	}

	if expectType && server["type"] != "stdio" {
		t.Fatalf("type = %#v, want stdio: %#v", server["type"], server)
	}

	if !expectType {
		if _, found := server["type"]; found {
			t.Fatalf("Gemini MCP config should use minimal project schema: %#v", server)
		}
	}
}

func codexEventBlock(t *testing.T, config, event string) string {
	t.Helper()

	start := strings.Index(config, event+" = [")
	if start == -1 {
		t.Fatalf("missing Codex event %s:\n%s", event, config)
	}

	end := strings.Index(config[start:], "]\n")
	if end == -1 {
		t.Fatalf("missing end of Codex event %s:\n%s", event, config[start:])
	}

	return config[start : start+end+2]
}

func assertCodexMatcherCount(
	t *testing.T,
	block string,
	event string,
	matcher string,
) {
	t.Helper()

	needle := `matcher = "` + matcher + `"`
	if count := strings.Count(block, needle); count != 1 {
		t.Fatalf("%s matcher %q count = %d, want %d:\n%s",
			event,
			matcher,
			count,
			1,
			block,
		)
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
		paths.ClaudeMCP,
		paths.CodexConfig,
		paths.Gemini,
	} {
		_, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat settings %s: %v", path, statErr)
		}
	}

	_, statErr := os.Stat(paths.CodexHooks)
	if !errors.Is(statErr, os.ErrNotExist) {
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

	if len(report.Checks) != 15 {
		t.Fatalf("check count = %d, want 15: %#v", len(report.Checks), report.Checks)
	}

	knownProviders := providerIDsByRegistry()
	for _, check := range report.Checks {
		if !knownProviders[check.Provider] {
			t.Fatalf("check uses unregistered provider %q: %#v", check.Provider, check)
		}

		if check.Status != "pass" {
			t.Fatalf("failed check: %#v", check)
		}
	}
}

func TestVerifySettingsRejectsInvalidPortableSkillSurface(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hookCommand := fakeAgentHookCommand(t)
	writeGeneratedSkillSurfaces(t, root, "managed-toolchain")

	err := agenthooks.SyncSettings(root, hookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	portablePath := filepath.Join(
		root,
		".agents",
		"skills",
		"managed-toolchain",
		"SKILL.md",
	)

	inlineErr0 := os.WriteFile(
		portablePath,
		[]byte("# Managed Toolchain\n\nmissing frontmatter\n"),
		0o600,
	)
	if inlineErr0 != nil {
		t.Fatalf("write invalid portable skill: %v", inlineErr0)
	}

	report, err := agenthooks.VerifySettings(root, hookCommand)
	if err == nil {
		t.Fatal("expected invalid portable skill surface failure")
	}

	if report.Status != "invalid" {
		t.Fatalf("status = %q, want invalid: %#v", report.Status, report)
	}

	found := false

	for _, check := range report.Checks {
		if check.Event == "skill-surface" &&
			check.Provider == string(agenthooks.ProviderGeneric) &&
			check.Tool == "managed-toolchain" &&
			check.Status == "fail" &&
			strings.Contains(check.Detail, "missing YAML frontmatter") {
			found = true
		}
	}

	if !found {
		t.Fatalf("missing failed generic skill-surface check: %#v", report.Checks)
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

	inlineErr1 := os.Remove(missingPath)
	if inlineErr1 != nil {
		t.Fatalf("remove codex skill surface: %v", inlineErr1)
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

func TestSyncSettingsImportsProviderMemory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := filepath.Join(root, ".claude", "projects", "repo", "memory", "project.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(source, []byte("session finding\n"), 0o600); err != nil {
		t.Fatalf("write memory source: %v", err)
	}

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	central := filepath.Join(root, ".coding-ethos", "memories", "MEMORY.md")
	payload, err := os.ReadFile(central)
	if err != nil {
		t.Fatalf("read central memory: %v", err)
	}
	if !strings.Contains(string(payload), "session finding") {
		t.Fatalf("central memory missing import:\n%s", payload)
	}
}

func TestDoctorSettingsRejectsWrongCommand(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	err = agenthooks.DoctorSettings(root, "/other/coding-ethos-run agent-hook")
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
		`hooks = true`,
		`hooks = false`,
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
		`"command": "/repo/bin/coding-ethos-run agent-hook"`,
		`"command": "/other/coding-ethos-run agent-hook"`,
		1,
	)

	overwriteAgentSettings(t, paths.Gemini, mutated)

	err = agenthooks.DoctorSettings(root, testHookCommand)
	if err == nil {
		t.Fatal("expected wrong Gemini hook command mismatch")
	}
}

func TestDoctorSettingsRejectsMismatchedMCPServer(t *testing.T) {
	t.Parallel()

	root := t.TempDir()

	err := agenthooks.SyncSettings(root, testHookCommand)
	if err != nil {
		t.Fatalf("sync settings: %v", err)
	}

	paths := agenthooks.DefaultSettingsPaths(root)

	payload, err := os.ReadFile(paths.ClaudeMCP)
	if err != nil {
		t.Fatalf("read Claude MCP settings: %v", err)
	}

	mutated := strings.Replace(
		string(payload),
		`"/repo/bin/coding-ethos-run"`,
		`"/other/coding-ethos-run"`,
		1,
	)

	overwriteAgentSettings(t, paths.ClaudeMCP, mutated)

	err = agenthooks.DoctorSettings(root, testHookCommand)
	if err == nil {
		t.Fatal("expected wrong Claude MCP command mismatch")
	}
}

func overwriteAgentSettings(t *testing.T, path, content string) {
	t.Helper()

	file, err := os.OpenFile(filepath.Clean(path), os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatalf("open settings for overwrite: %v", err)
	}

	_, err = file.WriteString(content)
	if err != nil {
		_ = file.Close()

		t.Fatalf("write settings: %v", err)
	}

	inlineErr2 := file.Close()
	if inlineErr2 != nil {
		t.Fatalf("close settings: %v", inlineErr2)
	}
}

func writeGeneratedSkillSurfaces(t *testing.T, root, skillID string) {
	t.Helper()

	content := strings.Join([]string{
		"---",
		"name: " + skillID,
		"metadata:",
		"  source: coding_ethos.yml",
		"---",
		"",
		"# " + skillID,
		"",
	}, "\n")
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
		err := os.MkdirAll(filepath.Dir(path), 0o755)
		if err != nil {
			t.Fatalf("create skill dir %s: %v", path, err)
		}

		err = os.WriteFile(path, []byte(content), 0o600)
		if err != nil {
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
		t.Fatalf(
			"%s missing supported capability %q: %#v",
			provider,
			supported,
			capability,
		)
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

func providersWithCapability(
	capabilities []agenthooks.ProviderCapability,
	capability string,
) []string {
	providers := []string{}

	for _, provider := range capabilities {
		if containsString(provider.Supported, capability) {
			providers = append(providers, provider.Provider)
		}
	}

	return providers
}

func providerIDsByRegistry() map[string]bool {
	providers := map[string]bool{}

	for _, capability := range agenthooks.ProviderCapabilities() {
		providers[capability.Provider] = true
	}

	return providers
}

func capabilityProbePayload(provider, cwd string) string {
	switch provider {
	case string(agenthooks.ProviderClaude):
		return fmt.Sprintf(`{
			"provider": "claude",
			"hook_event_name": "PreToolUse",
			"cwd": %q,
			"tool_name": "Bash",
			"tool_input": {"command": "git add file.txt"}
		}`, cwd)
	case string(agenthooks.ProviderCodex):
		return fmt.Sprintf(`{
			"provider": "codex",
			"event": "PreToolUse",
			"cwd": %q,
			"tool": "exec_command",
			"input": {"command": "git add file.txt"}
		}`, cwd)
	case string(agenthooks.ProviderGemini):
		return fmt.Sprintf(`{
			"provider": "gemini-cli",
			"hookEventName": "BeforeTool",
			"cwd": %q,
			"toolName": "run_shell_command",
			"toolInput": {"command": "git add file.txt"}
		}`, cwd)
	default:
		panic("unsupported provider " + provider)
	}
}

func containsString(values []string, expected string) bool {
	return slices.Contains(values, expected)
}

func fakeAgentHookCommand(t *testing.T) string {
	t.Helper()

	ethosRoot := t.TempDir()
	binDir := filepath.Join(ethosRoot, "bin")
	bundleDir := filepath.Join(ethosRoot, "build", "policy")

	err := os.MkdirAll(binDir, 0o700)
	if err != nil {
		t.Fatalf("create fake bin dir: %v", err)
	}

	err = os.MkdirAll(bundleDir, 0o700)
	if err != nil {
		t.Fatalf("create fake policy dir: %v", err)
	}

	bundlePath := filepath.Join(bundleDir, "policy-bundle.json")

	file, err := os.Create(bundlePath)
	if err != nil {
		t.Fatalf("create fake policy bundle: %v", err)
	}

	err = policy.EncodeBundle(file, policy.ExampleBundle())
	if err != nil {
		t.Fatalf("encode fake policy bundle: %v", err)
	}

	err = file.Close()
	if err != nil {
		t.Fatalf("close fake policy bundle: %v", err)
	}

	runner := filepath.Join(binDir, "coding-ethos-run")
	script := `#!/bin/sh
payload=$(cat)
case "$payload" in
  *'"provider": "claude"'*'git status --short'*)
    printf '%s\n' '{"hookSpecificOutput":{"updatedInput":{"command":"coding-ethos-run agent-shell -- '\''pwd && git status --short 2>&1'\''"}}}'
    exit 0
    ;;
  *'"provider": "gemini-cli"'*'git status --short'*)
    printf '%s\n' '{"hookSpecificOutput":{"updatedInput":{"command":"coding-ethos-run agent-shell -- '\''git status --short'\''"}}}'
    exit 0
    ;;
  *'"provider": "codex"'*)
    printf '%s\n' '{"decision":"block","reason":"event: PreToolUse\nstatus: blocked\ndecisions[1]{policy_id,message,suggestion}:\n  git.wrapper_required,Direct git execution must use the approved route,Resubmit through cerun --","hookSpecificOutput":{"permissionDecisionReason":"event: PreToolUse\nstatus: blocked\ndecisions[1]{policy_id,message,suggestion}:\n  git.wrapper_required,Direct git execution must use the approved route,Resubmit through cerun --"}}'
    exit 2
    ;;
  *'"provider": "gemini-cli"'*)
    printf '%s\n' '{"decision":"deny","systemMessage":"blocked"}'
    exit 2
    ;;
  *)
    printf '%s\n' '{"decision":"block","systemMessage":"blocked"}'
    exit 2
    ;;
esac
`
	err = os.WriteFile(runner, []byte(script), 0o700)
	if err != nil {
		t.Fatalf("write fake runner: %v", err)
	}

	return "'" + strings.ReplaceAll(runner, "'", "'\\''") + "' agent-hook"
}
