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
	errSettingsMismatch    = errors.New(
		"agent hook settings do not contain expected hooks for all providers",
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

type allSettings struct {
	Claude claudeSettings   `json:"claude"`
	Codex  providerManifest `json:"codex"`
	Gemini providerManifest `json:"gemini"`
}

type SettingsPaths struct {
	Claude string
	Codex  string
	Gemini string
}

func DefaultSettingsPaths(root string) SettingsPaths {
	if root == "" {
		root = "."
	}

	return SettingsPaths{
		Claude: filepath.Join(root, ".claude", "settings.local.json"),
		Codex:  filepath.Join(root, ".codex", "coding-ethos-hooks.json"),
		Gemini: filepath.Join(root, ".gemini", "coding-ethos-hooks.json"),
	}
}

func WriteSettings(writer io.Writer, hookCommand string) error {
	settings, err := buildAllSettings(hookCommand)
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

func SyncSettings(root string, hookCommand string) error {
	settings, err := buildAllSettings(hookCommand)
	if err != nil {
		return err
	}

	paths := DefaultSettingsPaths(root)
	for _, spec := range []struct {
		merge func(map[string]any)
		path  string
	}{
		{path: paths.Claude, merge: func(payload map[string]any) {
			payload["hooks"] = settings.Claude.Hooks
		}},
		{path: paths.Codex, merge: func(payload map[string]any) {
			payload[string(ProviderCodex)] = settings.Codex
		}},
		{path: paths.Gemini, merge: func(payload map[string]any) {
			payload[string(ProviderGemini)] = settings.Gemini
		}},
	} {
		err = syncSettingsFile(spec.path, spec.merge)
		if err != nil {
			return err
		}
	}

	return nil
}

func syncSettingsFile(path string, merge func(map[string]any)) error {
	payload, err := existingSettingsPayload(path)
	if err != nil {
		return err
	}

	merge(payload)

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

func DoctorSettings(root string, hookCommand string) error {
	expected, err := buildAllSettings(hookCommand)
	if err != nil {
		return err
	}

	paths := DefaultSettingsPaths(root)
	checks := []struct {
		ok   func(map[string]any) bool
		path string
	}{
		{path: paths.Claude, ok: func(payload map[string]any) bool {
			return claudePayloadContainsExpectedHooks(payload, expected.Claude)
		}},
		{path: paths.Codex, ok: func(payload map[string]any) bool {
			return manifestPayloadMatches(payload, ProviderCodex, expected.Codex)
		}},
		{path: paths.Gemini, ok: func(payload map[string]any) bool {
			return manifestPayloadMatches(payload, ProviderGemini, expected.Gemini)
		}},
	}

	for _, check := range checks {
		payload, readErr := existingSettingsPayload(check.path)
		if readErr != nil {
			return readErr
		}

		if !check.ok(payload) {
			return errSettingsMismatch
		}
	}

	return nil
}

func buildAllSettings(hookCommand string) (allSettings, error) {
	if hookCommand == "" {
		return allSettings{}, errHookCommandRequired
	}

	return allSettings{
		Claude: buildClaudeSettings(RuntimeHookSpecs(), hookCommand),
		Codex:  buildProviderManifest(RuntimeHookSpecs(), hookCommand),
		Gemini: buildProviderManifest(RuntimeHookSpecs(), hookCommand),
	}, nil
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

func claudePayloadContainsExpectedHooks(
	payload map[string]any,
	expected claudeSettings,
) bool {
	raw, err := json.Marshal(payload)
	if err != nil {
		return false
	}

	var actual claudeSettings

	err = json.Unmarshal(raw, &actual)
	if err != nil {
		return false
	}

	return settingsContainExpectedHooks(actual, expected)
}

func manifestPayloadMatches(
	payload map[string]any,
	provider Provider,
	expected providerManifest,
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

	return reflect.DeepEqual(actual, expected)
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
