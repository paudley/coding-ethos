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
	"slices"
)

const (
	settingsDirMode  = 0o755
	settingsFileMode = 0o600
)

var (
	errHookCommandRequired = errors.New("hook command is required")
	errSettingsRequired    = errors.New("settings path is required")
	errSettingsMismatch    = errors.New(
		"claude settings do not contain agent hook command",
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

func WriteSettings(writer io.Writer, hookCommand string) error {
	settings, err := buildSettings(hookCommand)
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

func buildSettings(hookCommand string) (claudeSettings, error) {
	if hookCommand == "" {
		return claudeSettings{}, errHookCommandRequired
	}

	return claudeSettings{
		Hooks: map[string][]matcherHook{
			"PreToolUse": {
				commandMatcher("Bash", hookCommand),
				commandMatcher("Write", hookCommand),
				commandMatcher("Edit", hookCommand),
				commandMatcher("MultiEdit", hookCommand),
			},
			"PostToolUse": {
				commandMatcher("Bash", hookCommand),
			},
		},
	}, nil
}

func SyncSettings(path string, hookCommand string) error {
	if path == "" {
		return errSettingsRequired
	}

	settings, err := buildSettings(hookCommand)
	if err != nil {
		return err
	}

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

	err = encoder.Encode(settings)
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}

	return nil
}

func DoctorSettings(path string, hookCommand string) error {
	if path == "" {
		return errSettingsRequired
	}

	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open settings: %w", err)
	}
	defer file.Close()

	var settings claudeSettings

	err = json.NewDecoder(file).Decode(&settings)
	if err != nil {
		return fmt.Errorf("decode settings: %w", err)
	}

	expected, err := buildSettings(hookCommand)
	if err != nil {
		return err
	}

	if !settingsContainExpectedHooks(settings, expected) {
		return errSettingsMismatch
	}

	return nil
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
