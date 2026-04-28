// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // Provider hook schemas use mixed native JSON naming.
package agenthooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	codexConfigGrowth = 2
	settingsDirMode   = 0o755
	settingsFileMode  = 0o600
)

var (
	errHookCommandRequired = errors.New("hook command is required")
	errSettingsMismatch    = errors.New(
		"agent hook settings do not contain expected hooks for all providers",
	)
)

type commandHook struct {
	Name          string `json:"name,omitempty"`
	Type          string `json:"type"`
	Command       string `json:"command"`
	StatusMessage string `json:"statusMessage,omitempty"`
	Timeout       int    `json:"timeout,omitempty"`
}

type matcherHook struct {
	Matcher string        `json:"matcher,omitempty"`
	Hooks   []commandHook `json:"hooks"`
}

type claudeSettings struct {
	Hooks map[string][]matcherHook `json:"hooks"`
}

type allSettings struct {
	Claude claudeSettings `json:"claude"`
	Codex  claudeSettings `json:"codex"`
	Gemini claudeSettings `json:"gemini"`
}

type SettingsPaths struct {
	Claude      string
	CodexConfig string
	CodexHooks  string
	Gemini      string
}

func DefaultSettingsPaths(root string) SettingsPaths {
	if root == "" {
		root = "."
	}

	return SettingsPaths{
		Claude:      filepath.Join(root, ".claude", "settings.local.json"),
		CodexConfig: filepath.Join(root, ".codex", "config.toml"),
		CodexHooks:  filepath.Join(root, ".codex", "hooks.json"),
		Gemini:      filepath.Join(root, ".gemini", "settings.json"),
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

	err = syncSettingsFile(paths.Claude, func(payload map[string]any) {
		payload["hooks"] = settings.Claude.Hooks
	})
	if err != nil {
		return err
	}

	err = syncSettingsFile(paths.CodexHooks, func(payload map[string]any) {
		payload["hooks"] = settings.Codex.Hooks
	})
	if err != nil {
		return err
	}

	err = syncCodexConfig(paths.CodexConfig)
	if err != nil {
		return err
	}

	err = syncSettingsFile(paths.Gemini, func(payload map[string]any) {
		payload["hooks"] = settings.Gemini.Hooks
	})
	if err != nil {
		return err
	}

	return nil
}

func syncCodexConfig(path string) error {
	return syncTextSettingsFile(path, ensureCodexHookFeature)
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

func syncTextSettingsFile(path string, mutate func(string) string) error {
	content := ""

	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read settings: %w", err)
	}

	if err == nil {
		content = string(data)
	}

	err = os.MkdirAll(filepath.Dir(path), settingsDirMode)
	if err != nil {
		return fmt.Errorf("create settings directory: %w", err)
	}

	err = os.WriteFile(path, []byte(mutate(content)), settingsFileMode)
	if err != nil {
		return fmt.Errorf("write settings: %w", err)
	}

	return nil
}

func ensureCodexHookFeature(content string) string {
	lines := strings.Split(content, "\n")
	rewrite := codexConfigRewrite{
		output: make([]string, 0, len(lines)+codexConfigGrowth),
	}

	for _, line := range lines {
		rewrite.accept(line)
	}

	rewrite.finish()

	return strings.TrimRight(strings.Join(rewrite.output, "\n"), "\n") + "\n"
}

type codexConfigRewrite struct {
	output      []string
	inFeatures  bool
	sawFeatures bool
	wroteFlag   bool
}

func (rewrite *codexConfigRewrite) accept(line string) {
	if enteringNewSection(line) {
		rewrite.closeFeaturesSection()
		rewrite.inFeatures = strings.TrimSpace(line) == "[features]"
		rewrite.sawFeatures = rewrite.sawFeatures || rewrite.inFeatures
	}

	if rewrite.inFeatures && isCodexHookFlag(line) {
		rewrite.output = append(rewrite.output, codexHookFeatureLine())
		rewrite.wroteFlag = true

		return
	}

	if line != "" || len(rewrite.output) > 0 {
		rewrite.output = append(rewrite.output, line)
	}
}

func (rewrite *codexConfigRewrite) finish() {
	if rewrite.sawFeatures && !rewrite.wroteFlag {
		rewrite.output = append(rewrite.output, codexHookFeatureLine())
	}

	if !rewrite.sawFeatures {
		rewrite.appendFeaturesSection()
	}
}

func (rewrite *codexConfigRewrite) closeFeaturesSection() {
	if rewrite.inFeatures && !rewrite.wroteFlag {
		rewrite.output = append(rewrite.output, codexHookFeatureLine())
		rewrite.wroteFlag = true
	}
}

func (rewrite *codexConfigRewrite) appendFeaturesSection() {
	if len(rewrite.output) > 0 && rewrite.output[len(rewrite.output)-1] != "" {
		rewrite.output = append(rewrite.output, "")
	}

	rewrite.output = append(rewrite.output, "[features]", codexHookFeatureLine())
}

func enteringNewSection(line string) bool {
	trimmed := strings.TrimSpace(line)

	return strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")
}

func isCodexHookFlag(line string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), "codex_hooks")
}

func codexHookFeatureLine() string {
	return "codex_hooks = true"
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
		{path: paths.CodexHooks, ok: func(payload map[string]any) bool {
			return nativePayloadContainsExpectedHooks(payload, expected.Codex)
		}},
		{path: paths.Gemini, ok: func(payload map[string]any) bool {
			return nativePayloadContainsExpectedHooks(payload, expected.Gemini)
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

	config, readErr := os.ReadFile(paths.CodexConfig)
	if readErr != nil {
		return fmt.Errorf("read Codex config: %w", readErr)
	}

	if !codexHookFeatureEnabled(string(config)) {
		return errSettingsMismatch
	}

	return nil
}

func buildAllSettings(hookCommand string) (allSettings, error) {
	if hookCommand == "" {
		return allSettings{}, errHookCommandRequired
	}

	return allSettings{
		Claude: buildClaudeSettings(RuntimeHookSpecs(), hookCommand),
		Codex:  buildCodexSettings(RuntimeHookSpecs(), hookCommand),
		Gemini: buildGeminiSettings(RuntimeHookSpecs(), hookCommand),
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

func buildCodexSettings(specs []HookSpec, hookCommand string) claudeSettings {
	hooks := make(map[string][]matcherHook)
	for _, spec := range specs {
		hooks[spec.Event] = append(
			hooks[spec.Event],
			codexMatcher(spec.Tool, hookCommand),
		)
	}

	return claudeSettings{Hooks: hooks}
}

func buildGeminiSettings(specs []HookSpec, hookCommand string) claudeSettings {
	hooks := make(map[string][]matcherHook)

	for _, spec := range specs {
		event, matcher, ok := geminiHookSpec(spec)
		if !ok {
			continue
		}

		hooks[event] = append(
			hooks[event],
			geminiMatcher(matcher, hookCommand),
		)
	}

	return claudeSettings{Hooks: hooks}
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

func nativePayloadContainsExpectedHooks(
	payload map[string]any,
	expected claudeSettings,
) bool {
	return claudePayloadContainsExpectedHooks(payload, expected)
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

func codexMatcher(matcher string, hookCommand string) matcherHook {
	return matcherHook{
		Matcher: matcher,
		Hooks: []commandHook{{
			Type:          "command",
			Command:       hookCommand,
			StatusMessage: "coding-ethos policy",
		}},
	}
}

func geminiMatcher(matcher string, hookCommand string) matcherHook {
	return matcherHook{
		Matcher: matcher,
		Hooks: []commandHook{{
			Name:    "coding-ethos",
			Type:    "command",
			Command: hookCommand,
		}},
	}
}

func geminiHookSpec(spec HookSpec) (string, string, bool) {
	switch {
	case spec.Event == "PreToolUse" && spec.Tool == "Bash":
		return "BeforeTool", "run_shell_command", true
	case spec.Event == "PreToolUse" && spec.Tool == "Write":
		return "BeforeTool", "write_file", true
	case spec.Event == "SessionStart":
		return "SessionStart", "startup", true
	default:
		return "", "", false
	}
}

func codexHookFeatureEnabled(content string) bool {
	inFeatures := false

	for line := range strings.SplitSeq(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			inFeatures = trimmed == "[features]"

			continue
		}

		if inFeatures && strings.HasPrefix(trimmed, "codex_hooks") {
			return strings.Contains(trimmed, "true")
		}
	}

	return false
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
