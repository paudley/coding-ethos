// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agenthooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

const (
	settingsDirMode  = 0o755
	settingsFileMode = 0o600
	manifestVersion  = 1
)

var (
	errHookCommandRequired = errors.New("hook command is required")
	errSettingsRequired    = errors.New("settings path is required")
	errUnsupportedProvider = errors.New("unsupported agent hook provider")
	errSettingsMismatch    = errors.New(
		"agent hook settings do not contain expected provider hook command",
	)
)

type commandHook struct {
	Type    string `json:"type"`
	Command string `json:"command"`
}

type matcherHook struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []commandHook `json:"hooks"`
}

type claudeSettings struct {
	Hooks map[string][]matcherHook `json:"hooks"`
}

type manifestHook struct {
	Event   string `json:"event"`
	Tool    string `json:"tool,omitempty"`
	Command string `json:"command"`
}

type providerManifest struct {
	Hooks   []manifestHook `json:"hooks"`
	Version int            `json:"version"`
}

type codexSettings struct {
	Codex providerManifest `json:"codex"`
}

type geminiSettings struct {
	Gemini providerManifest `json:"gemini"`
}

func WriteSettings(writer io.Writer, hookCommand string) error {
	err := WriteProviderSettings(writer, ProviderClaude, hookCommand)
	if err != nil {
		return err
	}

	return nil
}

func WriteProviderSettings(
	writer io.Writer,
	provider Provider,
	hookCommand string,
) error {
	settings, err := buildProviderSettings(provider, hookCommand)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(settings)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	return nil
}

func buildProviderSettings(provider Provider, hookCommand string) (any, error) {
	if hookCommand == "" {
		return nil, errHookCommandRequired
	}

	switch provider {
	case ProviderClaude:
		return buildClaudeSettings(RuntimeHookSpecs(), hookCommand), nil
	case ProviderCodex:
		return codexSettings{
			Codex: buildProviderManifest(RuntimeHookSpecs(), hookCommand),
		}, nil
	case ProviderGemini:
		return geminiSettings{
			Gemini: buildProviderManifest(RuntimeHookSpecs(), hookCommand),
		}, nil
	default:
		return nil, errUnsupportedProvider
	}
}

func SyncSettings(path string, hookCommand string) error {
	err := SyncProviderSettings(path, ProviderClaude, hookCommand)
	if err != nil {
		return err
	}

	return nil
}

func SyncProviderSettings(path string, provider Provider, hookCommand string) error {
	if path == "" {
		return errSettingsRequired
	}

	settings, err := buildProviderSettings(provider, hookCommand)
	if err != nil {
		return err
	}

	payload, err := existingSettingsPayload(path)
	if err != nil {
		return err
	}

	mergeProviderSettings(payload, settings)

	err = os.MkdirAll(filepath.Dir(path), settingsDirMode)
	if err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	file, err := os.OpenFile(
		path,
		os.O_WRONLY|os.O_CREATE|os.O_TRUNC,
		settingsFileMode,
	)
	if err != nil {
		return fmt.Errorf("open settings: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	err = encoder.Encode(payload)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	return nil
}

func existingSettingsPayload(path string) (map[string]any, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}

	if err != nil {
		return nil, fmt.Errorf("open existing settings: %w", err)
	}

	defer file.Close()

	payload := map[string]any{}

	err = json.NewDecoder(file).Decode(&payload)
	if err != nil {
		return nil, fmt.Errorf("decode existing settings: %w", err)
	}

	return payload, nil
}

func DoctorSettings(path string, hookCommand string) error {
	err := DoctorProviderSettings(path, ProviderClaude, hookCommand)
	if err != nil {
		return err
	}

	return nil
}

func DoctorProviderSettings(path string, provider Provider, hookCommand string) error {
	if path == "" {
		return errSettingsRequired
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open settings: %w", err)
	}
	defer file.Close()

	payload := map[string]any{}

	err = json.NewDecoder(file).Decode(&payload)
	if err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}

	expected, err := buildProviderSettings(provider, hookCommand)
	if err != nil {
		return err
	}

	if !providerSettingsContainExpectedHooks(payload, provider, expected) {
		return errSettingsMismatch
	}

	return nil
}

func buildClaudeSettings(specs []HookSpec, hookCommand string) claudeSettings {
	hooks := make(map[string][]matcherHook)
	for _, spec := range specs {
		hooks[spec.Event] = append(
			hooks[spec.Event],
			commandMatcher(spec.Tool, hookCommand),
		)
	}

	return claudeSettings{Hooks: hooks}
}

func buildProviderManifest(
	specs []HookSpec,
	hookCommand string,
) providerManifest {
	hooks := make([]manifestHook, 0, len(specs))
	for _, spec := range specs {
		hooks = append(hooks, manifestHook{
			Event:   spec.Event,
			Tool:    spec.Tool,
			Command: hookCommand,
		})
	}

	return providerManifest{
		Version: manifestVersion,
		Hooks:   hooks,
	}
}

func mergeProviderSettings(
	payload map[string]any,
	settings any,
) {
	switch typed := settings.(type) {
	case claudeSettings:
		payload["hooks"] = typed.Hooks
	case codexSettings:
		payload[string(ProviderCodex)] = typed.Codex
	case geminiSettings:
		payload[string(ProviderGemini)] = typed.Gemini
	}
}

func providerSettingsContainExpectedHooks(
	payload map[string]any,
	provider Provider,
	expected any,
) bool {
	switch provider {
	case ProviderClaude:
		return claudePayloadContainsExpectedHooks(payload, expected)
	case ProviderCodex:
		return manifestPayloadMatches(payload, ProviderCodex, expected)
	case ProviderGemini:
		return manifestPayloadMatches(payload, ProviderGemini, expected)
	default:
		return false
	}
}

func claudePayloadContainsExpectedHooks(payload map[string]any, expected any) bool {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	var actual claudeSettings

	err = json.Unmarshal(raw, &actual)
	if err != nil {
		return false
	}

	expectedSettings, ok := expected.(claudeSettings)
	if !ok {
		return false
	}

	return settingsContainExpectedHooks(actual, expectedSettings)
}

func manifestPayloadMatches(
	payload map[string]any,
	provider Provider,
	expected any,
) bool {
	raw, err := json.Marshal(payload[string(provider)])
	if err != nil {
		return false
	}

	var actual providerManifest

	err = json.Unmarshal(raw, &actual)
	if err != nil {
		return false
	}

	expectedManifest, ok := expectedManifestForProvider(provider, expected)
	if !ok {
		return false
	}

	return reflect.DeepEqual(actual, expectedManifest)
}

func expectedManifestForProvider(
	provider Provider,
	expected any,
) (providerManifest, bool) {
	switch provider {
	case ProviderClaude:
		return providerManifest{}, false
	case ProviderCodex:
		settings, ok := expected.(codexSettings)

		return settings.Codex, ok
	case ProviderGemini:
		settings, ok := expected.(geminiSettings)

		return settings.Gemini, ok
	default:
		return providerManifest{}, false
	}
}

func commandMatcher(matcher string, hookCommand string) matcherHook {
	return matcherHook{
		Matcher: matcher,
		Hooks: []commandHook{{
			Type:    "command",
			Command: hookCommand,
		}},
	}
}

func settingsContainExpectedHooks(
	actual claudeSettings,
	expected claudeSettings,
) bool {
	for event, expectedMatchers := range expected.Hooks {
		actualMatchers := actual.Hooks[event]
		for _, expectedMatcher := range expectedMatchers {
			if !containsMatcher(actualMatchers, expectedMatcher) {
				return false
			}
		}
	}

	return true
}

func containsMatcher(actual []matcherHook, expected matcherHook) bool {
	for _, candidate := range actual {
		if candidate.Matcher != expected.Matcher {
			continue
		}

		if containsCommandHook(candidate.Hooks, expected.Hooks[0]) {
			return true
		}
	}

	return false
}

func containsCommandHook(actual []commandHook, expected commandHook) bool {
	return slices.Contains(actual, expected)
}
