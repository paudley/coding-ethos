// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package agenthooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type codexHookTrustEntry struct {
	Key         string
	TrustedHash string
}

// DefaultCodexUserConfigPath returns the user-level Codex config path that
// stores project hook trust state.
func DefaultCodexUserConfigPath() (string, error) {
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve Codex home: %w", err)
		}

		codexHome = filepath.Join(home, ".codex")
	}

	return filepath.Join(codexHome, "config.toml"), nil
}

// SyncCodexTrustState records trust hashes for generated project Codex hooks.
func SyncCodexTrustState(root, hookCommand, userConfigPath string) error {
	entries, err := expectedCodexTrustEntries(root, hookCommand)
	if err != nil {
		return err
	}

	if userConfigPath == "" {
		userConfigPath, err = DefaultCodexUserConfigPath()
		if err != nil {
			return err
		}
	}

	return syncTextSettingsFile(userConfigPath, func(content string) string {
		return ensureCodexTrustState(content, root, entries)
	})
}

// VerifyCodexTrustState fails when Codex would treat generated project hooks as
// untrusted or modified.
func VerifyCodexTrustState(root, hookCommand, userConfigPath string) error {
	entries, err := expectedCodexTrustEntries(root, hookCommand)
	if err != nil {
		return err
	}

	if userConfigPath == "" {
		userConfigPath, err = DefaultCodexUserConfigPath()
		if err != nil {
			return err
		}
	}

	content, err := os.ReadFile(userConfigPath)
	if err != nil {
		return fmt.Errorf("read Codex user config: %w", err)
	}

	for _, entry := range entries {
		if !codexTrustContentHasEntry(string(content), entry) {
			return fmt.Errorf("%w: missing or stale %s", errCodexTrustMismatch, entry.Key)
		}
	}

	return nil
}

func ensureCodexTrustState(
	content string,
	root string,
	entries []codexHookTrustEntry,
) string {
	configPath := codexTrustConfigPath(root)
	withoutProjectTrust := removeCodexTrustEntriesForConfig(content, configPath)

	var builder strings.Builder
	builder.WriteString(strings.TrimRight(withoutProjectTrust, "\n"))

	if builder.Len() > 0 {
		builder.WriteString("\n\n")
	}

	if !strings.Contains(withoutProjectTrust, "[hooks.state]") {
		builder.WriteString("[hooks.state]\n\n")
	}

	for _, entry := range entries {
		builder.WriteString("[hooks.state.")
		builder.WriteString(tomlString(entry.Key))
		builder.WriteString("]\ntrusted_hash = ")
		builder.WriteString(tomlString(entry.TrustedHash))
		builder.WriteString("\n\n")
	}

	return strings.TrimRight(builder.String(), "\n") + "\n"
}

func removeCodexTrustEntriesForConfig(content, configPath string) string {
	lines := strings.Split(content, "\n")
	output := make([]string, 0, len(lines))
	inProjectTrust := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[hooks.state.") &&
			strings.HasSuffix(trimmed, "]") {
			key := strings.TrimSuffix(
				strings.TrimPrefix(trimmed, "[hooks.state."),
				"]",
			)

			unquotedKey, err := strconv.Unquote(key)
			if err == nil {
				key = unquotedKey
			}

			inProjectTrust = strings.HasPrefix(
				strings.Trim(key, `"`),
				configPath+":",
			)
		} else if enteringNewSection(line) {
			inProjectTrust = false
		}

		if inProjectTrust {
			continue
		}

		output = append(output, line)
	}

	return strings.TrimRight(strings.Join(output, "\n"), "\n") + "\n"
}

func expectedCodexTrustEntries(
	root, hookCommand string,
) ([]codexHookTrustEntry, error) {
	settings, err := buildAllSettings(hookCommand)
	if err != nil {
		return nil, err
	}

	configPath := codexTrustConfigPath(root)
	entries := make([]codexHookTrustEntry, 0, len(settings.Codex.Hooks))

	for _, event := range codexHookEventOrder() {
		matchers := settings.Codex.Hooks[event]
		for groupIndex, matcher := range matchers {
			for handlerIndex, hook := range matcher.Hooks {
				key := fmt.Sprintf(
					"%s:%s:%d:%d",
					configPath,
					codexHookTrustEventLabel(event),
					groupIndex,
					handlerIndex,
				)
				entries = append(entries, codexHookTrustEntry{
					Key:         key,
					TrustedHash: codexHookTrustHash(event, matcher.Matcher, hook),
				})
			}
		}
	}

	return entries, nil
}

func codexTrustConfigPath(root string) string {
	configPath := DefaultSettingsPaths(root).CodexConfig

	absolute, err := filepath.Abs(configPath)
	if err != nil {
		return filepath.Clean(configPath)
	}

	return filepath.Clean(absolute)
}

func codexHookTrustEventLabel(event string) string {
	switch event {
	case eventPreToolUse:
		return "pre_tool_use"
	case eventPermissionRequest:
		return "permission_request"
	case eventPostToolUse:
		return "post_tool_use"
	case eventSessionStart:
		return "session_start"
	case eventUserPromptSubmit:
		return "user_prompt_submit"
	case eventStop:
		return "stop"
	default:
		return strings.ToLower(event)
	}
}

func codexHookTrustHash(event, matcher string, hook commandHook) string {
	timeout := hook.Timeout
	if timeout == 0 {
		timeout = 30
	}

	handler := map[string]any{
		"async":         false,
		"command":       hook.Command,
		"statusMessage": hook.StatusMessage,
		"timeout":       timeout,
		"type":          "command",
	}

	identity := map[string]any{
		"event_name": codexHookTrustEventLabel(event),
		"hooks":      []any{handler},
	}

	if matcher != "" {
		identity["matcher"] = matcher
	}

	payload, err := json.Marshal(identity)
	if err != nil {
		return "sha256:"
	}

	sum := sha256.Sum256(payload)

	return "sha256:" + hex.EncodeToString(sum[:])
}

func codexTrustContentHasEntry(content string, entry codexHookTrustEntry) bool {
	header := "[hooks.state." + tomlString(entry.Key) + "]"

	start := strings.Index(content, header)
	if start == -1 {
		return false
	}

	next := strings.Index(content[start+len(header):], "\n[")
	section := content[start:]

	if next != -1 {
		section = content[start : start+len(header)+next]
	}

	return strings.Contains(
		section,
		"trusted_hash = "+tomlString(entry.TrustedHash),
	)
}
