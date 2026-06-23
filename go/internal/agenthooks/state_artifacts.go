// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"fmt"

	"blackcat.ca/coding-ethos/go/internal/syncstate"
)

func StateArtifacts(root, hookCommand string) ([]syncstate.Artifact, error) {
	settings, err := buildAllSettings(hookCommand)
	if err != nil {
		return nil, err
	}

	serverConfig, err := mcpServerConfig(hookCommand)
	if err != nil {
		return nil, err
	}

	paths := DefaultSettingsPaths(root)

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

	artifacts, err := syncstate.Artifacts(
		root,
		agentHookStateArtifactInputs(paths, claude, claudeMCP, codex, gemini),
	)
	if err != nil {
		return nil, fmt.Errorf("build agent hook state artifacts: %w", err)
	}

	return artifacts, nil
}

func agentHookStateArtifactInputs(
	paths SettingsPaths,
	claude,
	claudeMCP,
	codex,
	gemini string,
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
	}
}
