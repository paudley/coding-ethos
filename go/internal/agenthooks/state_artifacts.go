// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/syncstate"
)

func StateArtifacts(root, hookCommand string) ([]syncstate.Artifact, error) {
	return StateArtifactsWithMCPCommand(root, hookCommand, "")
}

// StateArtifactsWithMCPCommand renders hook settings while keeping Coding
// Ethos MCP ownership independent from an external supervisor hook command.
func StateArtifactsWithMCPCommand(
	root string,
	hookCommand string,
	mcpCommand string,
) ([]syncstate.Artifact, error) {
	return StateArtifactsForRootsWithMCPCommand(
		root,
		root,
		root,
		hookCommand,
		mcpCommand,
	)
}

// StateArtifactsForRootsWithMCPCommand renders settings that keep generated
// provider configuration, repository inspection, and durable state separate.
func StateArtifactsForRootsWithMCPCommand(
	settingsRoot string,
	repoRoot string,
	stateRoot string,
	hookCommand string,
	mcpCommand string,
) ([]syncstate.Artifact, error) {
	settings, err := buildAllSettings(hookCommand)
	if err != nil {
		return nil, err
	}

	serverConfig, err := mcpServerConfigForRoots(
		hookCommand,
		mcpCommand,
		settingsRoot,
		repoRoot,
		stateRoot,
	)
	if err != nil {
		return nil, err
	}

	inputs, err := renderProviderStateArtifactInputs(
		settingsRoot,
		settings,
		serverConfig,
	)
	if err != nil {
		return nil, err
	}

	artifacts, err := syncstate.Artifacts(settingsRoot, inputs)
	if err != nil {
		return nil, fmt.Errorf("build agent hook state artifacts: %w", err)
	}

	return artifacts, nil
}

func renderProviderStateArtifactInputs(
	settingsRoot string,
	settings allSettings,
	serverConfig mcpServer,
) ([]syncstate.ArtifactInput, error) {
	paths := DefaultSettingsPaths(settingsRoot)

	claude, err := renderSettingsFileContent(paths.Claude, func(payload map[string]any) {
		payload["hooks"] = settings.Claude.Hooks
	})
	if err != nil {
		return nil, err
	}

	claudeMCP, err := renderSettingsFileContent(
		paths.ClaudeMCP,
		func(payload map[string]any) {
			syncMCPServers(payload, serverConfig.claudeJSON())
		},
	)
	if err != nil {
		return nil, err
	}

	codex, err := renderTextSettingsFileContent(
		paths.CodexConfig,
		func(content string) string {
			return ensureCodexConfig(content, settings.Codex, serverConfig)
		},
	)
	if err != nil {
		return nil, err
	}

	gemini, err := renderSettingsFileContent(paths.Gemini, func(payload map[string]any) {
		payload["hooksConfig"] = settings.Gemini.HooksConfig
		payload["hooks"] = settings.Gemini.Hooks
		syncMCPServers(payload, serverConfig.geminiJSON())
	})
	if err != nil {
		return nil, err
	}

	kimiConfig, kimiMCP, err := renderKimiStateArtifacts(paths, settings, serverConfig)
	if err != nil {
		return nil, err
	}

	return agentHookStateArtifactInputs(
		paths,
		claude,
		claudeMCP,
		codex,
		gemini,
		kimiConfig,
		kimiMCP,
	), nil
}

func renderKimiStateArtifacts(
	paths SettingsPaths,
	settings allSettings,
	serverConfig mcpServer,
) (string, string, error) {
	config, err := renderTextSettingsFileContent(
		paths.KimiConfig,
		func(content string) string {
			return ensureKimiConfig(content, settings.Kimi)
		},
	)
	if err != nil {
		return "", "", err
	}

	mcp, err := renderSettingsFileContent(paths.KimiMCP, func(payload map[string]any) {
		syncMCPServers(payload, serverConfig.geminiJSON())
	})
	if err != nil {
		return "", "", err
	}

	return config, mcp, nil
}

func agentHookStateArtifactInputs(
	paths SettingsPaths,
	claude,
	claudeMCP,
	codex,
	gemini,
	kimiConfig,
	kimiMCP string,
) []syncstate.ArtifactInput {
	const verifyCommand = "bin/coding-ethos-run agent-hooks doctor"

	return []syncstate.ArtifactInput{
		{
			RelativePath:        paths.Claude,
			Content:             claude,
			Provider:            "agent-hooks",
			Surface:             "claude-settings",
			VerificationCommand: verifyCommand,
		},
		{
			RelativePath:        paths.ClaudeMCP,
			Content:             claudeMCP,
			Provider:            "agent-hooks",
			Surface:             "claude-mcp",
			VerificationCommand: verifyCommand,
		},
		{
			RelativePath:        paths.CodexConfig,
			Content:             codex,
			Provider:            "agent-hooks",
			Surface:             "codex-config",
			VerificationCommand: verifyCommand,
		},
		{
			RelativePath:        paths.Gemini,
			Content:             gemini,
			Provider:            "agent-hooks",
			Surface:             "gemini-settings",
			VerificationCommand: verifyCommand,
		},
		{
			RelativePath:        paths.KimiConfig,
			Content:             kimiConfig,
			Provider:            "agent-hooks",
			Surface:             "kimi-config",
			VerificationCommand: verifyCommand,
		},
		{
			RelativePath:        paths.KimiMCP,
			Content:             kimiMCP,
			Provider:            "agent-hooks",
			Surface:             "kimi-mcp",
			VerificationCommand: verifyCommand,
		},
	}
}
