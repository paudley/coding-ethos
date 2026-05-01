// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

//nolint:tagliatelle // Provider hook schemas use mixed native JSON naming.
package agenthooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	codexConfigGrowth = 2
	settingsDirMode   = 0o755
	settingsFileMode  = 0o600
	verifyTimeout     = 30 * time.Second
)

var (
	errHookCommandRequired     = errors.New("hook command is required")
	errMissingSkillFrontmatter = errors.New(
		"generated skill is missing YAML frontmatter",
	)
	errSettingsMismatch = errors.New(
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
	Hooks       map[string][]matcherHook `json:"hooks"`
	HooksConfig map[string]any           `json:"hooksConfig,omitempty"`
}

type ProviderCapability struct {
	Provider        string   `json:"provider"`
	Coverage        string   `json:"coverage"`
	NativeFiles     []string `json:"native_files"`
	Supported       []string `json:"supported"`
	ProviderLimited []string `json:"provider_limited,omitempty"`
	Unsupported     []string `json:"unsupported,omitempty"`
}

type allSettings struct {
	Claude       claudeSettings       `json:"claude"`
	Codex        claudeSettings       `json:"codex"`
	Gemini       claudeSettings       `json:"gemini"`
	Capabilities []ProviderCapability `json:"capabilities"`
}

type SettingsPaths struct {
	Claude      string
	CodexConfig string
	CodexHooks  string
	Gemini      string
}

// VerifyReport describes the installed native hook surfaces and runnable smoke
// checks performed by agent-hooks verify.
type VerifyReport struct {
	Status       string               `json:"status"`
	Checks       []VerifyCheck        `json:"checks"`
	Capabilities []ProviderCapability `json:"capabilities"`
}

// VerifyCheck records one provider-native hook payload probe.
type VerifyCheck struct {
	Provider string `json:"provider"`
	Event    string `json:"event"`
	Tool     string `json:"tool"`
	Status   string `json:"status"`
	Detail   string `json:"detail,omitempty"`
}

type hookProbe struct {
	provider string
	event    string
	tool     string
	payload  string
	validate func(hookProbeResult) error
}

type hookProbeResult struct {
	exitCode int
	stdout   string
	stderr   string
	payload  map[string]any
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

	err = syncCodexConfig(paths.CodexConfig, settings.Codex)
	if err != nil {
		return err
	}

	err = removeStaleCodexHooksFile(paths.CodexHooks)
	if err != nil {
		return err
	}

	err = syncSettingsFile(paths.Gemini, func(payload map[string]any) {
		payload["hooksConfig"] = settings.Gemini.HooksConfig
		payload["hooks"] = settings.Gemini.Hooks
	})
	if err != nil {
		return err
	}

	return nil
}

func syncCodexConfig(path string, settings claudeSettings) error {
	return syncTextSettingsFile(path, func(content string) string {
		return ensureCodexConfig(content, settings)
	})
}

func removeStaleCodexHooksFile(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
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

func ensureCodexConfig(content string, settings claudeSettings) string {
	withoutManaged := removeCodexHooksConfig(content)
	withFeature := ensureCodexHookFeature(withoutManaged)

	return strings.TrimRight(withFeature, "\n") + "\n\n" +
		renderManagedCodexHooksBlock(settings)
}

func removeCodexHooksConfig(content string) string {
	return removeCodexHooksSection(removeManagedCodexHooksBlock(content))
}

func removeManagedCodexHooksBlock(content string) string {
	const (
		begin = "# BEGIN coding-ethos managed hooks"
		end   = "# END coding-ethos managed hooks"
	)

	lines := strings.Split(content, "\n")
	output := make([]string, 0, len(lines))
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == begin:
			inBlock = true
			continue
		case trimmed == end && inBlock:
			inBlock = false
			continue
		case inBlock:
			continue
		default:
			output = append(output, line)
		}
	}

	return strings.TrimRight(strings.Join(output, "\n"), "\n") + "\n"
}

func removeCodexHooksSection(content string) string {
	lines := strings.Split(content, "\n")
	output := make([]string, 0, len(lines))
	inHooks := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "[hooks]":
			inHooks = true
			continue
		case inHooks && strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]"):
			inHooks = false
		case inHooks:
			continue
		case strings.HasPrefix(trimmed, "hooks."):
			continue
		}

		output = append(output, line)
	}

	return strings.TrimRight(strings.Join(output, "\n"), "\n") + "\n"
}

func renderManagedCodexHooksBlock(settings claudeSettings) string {
	var builder strings.Builder
	builder.WriteString("# BEGIN coding-ethos managed hooks\n")
	builder.WriteString("# Generated by coding-ethos. Edit coding_ethos.yml/config.yaml inputs, not this block.\n")
	builder.WriteString("[hooks]\n")

	for _, event := range codexHookEventOrder() {
		matchers := settings.Hooks[event]
		if len(matchers) == 0 {
			continue
		}

		builder.WriteString(codexHookEventKey(event))
		builder.WriteString(" = [\n")
		for _, matcher := range matchers {
			builder.WriteString("  { ")
			if matcher.Matcher != "" {
				builder.WriteString("matcher = ")
				builder.WriteString(tomlString(matcher.Matcher))
				builder.WriteString(", ")
			}
			builder.WriteString("hooks = [{ type = \"command\", command = ")
			builder.WriteString(tomlString(matcher.Hooks[0].Command))
			if matcher.Hooks[0].StatusMessage != "" {
				builder.WriteString(", statusMessage = ")
				builder.WriteString(tomlString(matcher.Hooks[0].StatusMessage))
			}
			builder.WriteString(", timeout = 30 }]")
			builder.WriteString(" },\n")
		}
		builder.WriteString("]\n")
	}

	builder.WriteString("# END coding-ethos managed hooks\n")

	return builder.String()
}

func codexHookEventOrder() []string {
	return []string{
		"PreToolUse",
		"PermissionRequest",
		"PostToolUse",
		"SessionStart",
		"UserPromptSubmit",
		"Stop",
	}
}

func codexHookEventKey(event string) string {
	return event
}

func tomlString(value string) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return `""`
	}

	return string(encoded)
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

	if !codexConfigContainsExpectedHooks(string(config), expected.Codex) {
		return errSettingsMismatch
	}

	return nil
}

func codexConfigContainsExpectedHooks(
	content string,
	expected claudeSettings,
) bool {
	for event, expectedMatchers := range expected.Hooks {
		eventKey := codexHookEventKey(event)
		if !strings.Contains(content, eventKey+" = [") {
			return false
		}

		for _, expectedMatcher := range expectedMatchers {
			if !strings.Contains(
				content,
				"command = "+tomlString(expectedMatcher.Hooks[0].Command),
			) {
				return false
			}

			if expectedMatcher.Matcher != "" &&
				!strings.Contains(content, "matcher = "+tomlString(expectedMatcher.Matcher)) {
				return false
			}
		}
	}

	return true
}

func VerifySettings(root string, hookCommand string) (VerifyReport, error) {
	if err := DoctorSettings(root, hookCommand); err != nil {
		return VerifyReport{
			Status:       "invalid",
			Capabilities: ProviderCapabilities(),
			Checks: []VerifyCheck{{
				Provider: "all",
				Event:    "settings",
				Status:   "fail",
				Detail:   err.Error(),
			}},
		}, err
	}

	report := VerifyReport{
		Status:       "valid",
		Capabilities: ProviderCapabilities(),
	}

	if err := appendSkillSurfaceChecks(root, &report); err != nil {
		return report, err
	}

	for _, probe := range hookProbes() {
		result, err := runHookProbe(root, hookCommand, probe)
		check := VerifyCheck{
			Provider: probe.provider,
			Event:    probe.event,
			Tool:     probe.tool,
			Status:   "pass",
		}
		if err != nil {
			check.Status = "fail"
			check.Detail = err.Error()
			report.Status = "invalid"
			report.Checks = append(report.Checks, check)

			return report, err
		}

		err = probe.validate(result)
		if err != nil {
			check.Status = "fail"
			check.Detail = err.Error()
			report.Status = "invalid"
			report.Checks = append(report.Checks, check)

			return report, err
		}

		report.Checks = append(report.Checks, check)
	}

	return report, nil
}

func appendSkillSurfaceChecks(root string, report *VerifyReport) error {
	checks, err := verifySkillSurfaces(root)
	report.Checks = append(report.Checks, checks...)
	if err != nil {
		report.Status = "invalid"

		return err
	}

	return nil
}

func verifySkillSurfaces(root string) ([]VerifyCheck, error) {
	skillIDs, err := repoSkillIDs(root)
	if err != nil {
		return nil, err
	}
	if len(skillIDs) == 0 {
		return nil, nil
	}

	checks := make([]VerifyCheck, 0, len(skillIDs)*skillProviderCount)
	var verifyErr error
	for _, skillID := range skillIDs {
		for _, surface := range skillSurfacePaths(root, skillID) {
			check := VerifyCheck{
				Provider: surface.provider,
				Event:    "skill-surface",
				Tool:     skillID,
				Status:   "pass",
			}
			if err := verifySkillFile(surface.path, skillID); err != nil {
				check.Status = "fail"
				check.Detail = err.Error()
				verifyErr = errors.Join(verifyErr, err)
			}

			checks = append(checks, check)
		}
	}

	return checks, verifyErr
}

const skillProviderCount = 3

type skillSurfacePath struct {
	provider string
	path     string
}

func repoSkillIDs(root string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".agents", "skills"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read generated skill directory: %w", err)
	}

	skillIDs := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			skillIDs = append(skillIDs, entry.Name())
		}
	}
	slices.Sort(skillIDs)

	return skillIDs, nil
}

func skillSurfacePaths(root string, skillID string) []skillSurfacePath {
	entrypoint := filepath.Join(skillID, "SKILL.md")

	return []skillSurfacePath{
		{
			provider: string(ProviderClaude),
			path:     filepath.Join(root, ".claude", "skills", entrypoint),
		},
		{
			provider: string(ProviderCodex),
			path:     filepath.Join(root, ".codex", "skills", entrypoint),
		},
		{
			provider: string(ProviderGemini),
			path: filepath.Join(
				root,
				".gemini",
				"extensions",
				"coding-ethos",
				"skills",
				entrypoint,
			),
		},
	}
}

func verifySkillFile(path string, skillID string) error {
	content, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("read generated skill %s: %w", path, err)
	}

	frontmatter, err := parseSkillFrontmatter(string(content))
	if err != nil {
		return fmt.Errorf("read generated skill metadata %s: %w", path, err)
	}

	if frontmatter.Name != skillID || frontmatter.EffectiveSource() != "coding_ethos.yml" {
		return fmt.Errorf("generated skill %s does not match skill %s", path, skillID)
	}

	return nil
}

type skillFrontmatter struct {
	Metadata struct {
		Source string `yaml:"source"`
	} `yaml:"metadata"`
	Name   string `yaml:"name"`
	Source string `yaml:"source"`
}

func (frontmatter skillFrontmatter) EffectiveSource() string {
	if frontmatter.Metadata.Source != "" {
		return frontmatter.Metadata.Source
	}

	return frontmatter.Source
}

func parseSkillFrontmatter(content string) (skillFrontmatter, error) {
	if !strings.HasPrefix(content, "---\n") {
		return skillFrontmatter{}, errMissingSkillFrontmatter
	}

	remainder := strings.TrimPrefix(content, "---\n")
	raw, _, ok := strings.Cut(remainder, "\n---")
	if !ok {
		return skillFrontmatter{}, errMissingSkillFrontmatter
	}

	var frontmatter skillFrontmatter
	if err := yaml.Unmarshal([]byte(raw), &frontmatter); err != nil {
		return skillFrontmatter{}, fmt.Errorf("parse YAML frontmatter: %w", err)
	}

	return frontmatter, nil
}

func buildAllSettings(hookCommand string) (allSettings, error) {
	if hookCommand == "" {
		return allSettings{}, errHookCommandRequired
	}

	return allSettings{
		Claude:       buildClaudeSettings(RuntimeHookSpecs(), hookCommand),
		Codex:        buildCodexSettings(RuntimeHookSpecs(), hookCommand),
		Gemini:       buildGeminiSettings(RuntimeHookSpecs(), hookCommand),
		Capabilities: ProviderCapabilities(),
	}, nil
}

func ProviderCapabilities() []ProviderCapability {
	return []ProviderCapability{
		{
			Provider:    string(ProviderClaude),
			Coverage:    "full",
			NativeFiles: []string{".claude/settings.local.json"},
			Supported: []string{
				"PreToolUse block",
				"PreToolUse updatedInput rewrite",
				"PostToolUse additionalContext",
				"PostToolUse edit verification advice",
				"PostToolBatch additionalContext",
				"PreCompact capture",
				"SessionStart additionalContext",
				"UserPromptSubmit additionalContext",
				"Stop additionalContext",
				"SessionEnd additionalContext",
				"SubagentStart additionalContext",
				"SubagentStop additionalContext",
			},
		},
		{
			Provider:    string(ProviderCodex),
			Coverage:    "partial",
			NativeFiles: []string{".codex/config.toml"},
			Supported: []string{
				"PreToolUse block",
				"PreToolUse native command hook",
				"PreToolUse apply_patch/edit policy hook",
				"PostToolUse compact additionalContext",
				"PostToolUse edit verification advice",
				"SessionStart additionalContext",
				"UserPromptSubmit additionalContext",
				"Stop compact systemMessage",
			},
			ProviderLimited: []string{
				"git wrapper enforcement blocks raw git because updatedInput is not supported",
				"lifecycle context is compacted because Codex flattens multiline allowed context",
			},
			Unsupported: []string{
				"PreToolUse updatedInput rewrite",
				"PostToolBatch additionalContext",
				"SessionEnd additionalContext",
				"SubagentStart additionalContext",
				"SubagentStop additionalContext",
			},
		},
		{
			Provider:    string(ProviderGemini),
			Coverage:    "partial",
			NativeFiles: []string{".gemini/settings.json"},
			Supported: []string{
				"BeforeTool deny",
				"BeforeTool systemMessage",
				"AfterTool additionalContext",
				"AfterTool edit verification advice",
				"BeforeAgent additionalContext",
				"AfterAgent additionalContext",
				"SessionStart additionalContext",
				"SessionEnd additionalContext",
			},
			ProviderLimited: []string{
				"BeforeTool maps to PreToolUse for run_shell_command, write_file, replace, and MultiEdit",
				"AfterTool maps to PostToolUse for run_shell_command, write_file, replace, and MultiEdit",
			},
			Unsupported: []string{
				"PreToolUse updatedInput rewrite",
				"PostToolBatch additionalContext",
				"PreCompact capture",
				"SubagentStart additionalContext",
				"SubagentStop additionalContext",
			},
		},
	}
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
		for _, matcher := range codexHookMatchers(spec) {
			hooks[spec.Event] = append(
				hooks[spec.Event],
				codexMatcher(matcher, hookCommand),
			)
		}
	}

	return claudeSettings{Hooks: hooks}
}

func codexHookMatchers(spec HookSpec) []string {
	switch {
	case spec.Event == "PreToolUse" && spec.Tool == "Bash":
		return []string{codexShellMatcher}
	case spec.Event == "PreToolUse" && spec.Tool == "Edit":
		return []string{codexEditMatcher}
	case spec.Event == "PostToolUse" && spec.Tool == "Bash":
		return []string{codexShellMatcher}
	case spec.Event == "PostToolUse" && spec.Tool == "Edit":
		return []string{codexEditMatcher}
	case spec.Event == "SessionStart":
		return []string{""}
	case spec.Event == "UserPromptSubmit":
		return []string{""}
	case spec.Event == "Stop":
		return []string{""}
	default:
		return nil
	}
}

const (
	codexShellMatcher = "Bash|exec_command|run_command|run_shell|run_shell_command|shell|shell_command"
	codexEditMatcher  = "apply_patch|Edit|Write|MultiEdit|edit_file|create_file|write_file"
)

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

	return claudeSettings{
		Hooks:       hooks,
		HooksConfig: map[string]any{"enabled": true},
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
	case spec.Event == "PreToolUse" && spec.Tool == "Edit":
		return "BeforeTool", "replace", true
	case spec.Event == "PreToolUse" && spec.Tool == "MultiEdit":
		return "BeforeTool", "MultiEdit", true
	case spec.Event == "PostToolUse" && spec.Tool == "Bash":
		return "AfterTool", "run_shell_command", true
	case spec.Event == "PostToolUse" && spec.Tool == "Write":
		return "AfterTool", "write_file", true
	case spec.Event == "PostToolUse" && spec.Tool == "Edit":
		return "AfterTool", "replace", true
	case spec.Event == "PostToolUse" && spec.Tool == "MultiEdit":
		return "AfterTool", "MultiEdit", true
	case spec.Event == "SessionStart":
		return "SessionStart", "startup", true
	case spec.Event == "UserPromptSubmit":
		return "BeforeAgent", "prompt", true
	case spec.Event == "Stop":
		return "AfterAgent", "stop", true
	case spec.Event == "SessionEnd":
		return "SessionEnd", "session", true
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

func hookProbes() []hookProbe {
	return []hookProbe{
		{
			provider: string(ProviderClaude),
			event:    "PreToolUse",
			tool:     "Bash",
			payload: `{
				"provider": "claude",
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": "pwd && git status --short 2>&1"}
			}`,
			validate: validateClaudeRewriteProbe,
		},
		{
			provider: string(ProviderClaude),
			event:    "PreToolUse",
			tool:     "Bash",
			payload: `{
				"provider": "claude",
				"hook_event_name": "PreToolUse",
				"tool_name": "Bash",
				"tool_input": {"command": "rm /repo/.git/coding-ethos-hooks/coding-ethos-git-hook && go build -o /repo/.git/coding-ethos-hooks/coding-ethos-git-hook ."}
			}`,
			validate: validateClaudeBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    "PreToolUse",
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "git status --short"}
			}`,
			validate: validateCodexBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    "PreToolUse",
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "/usr/bin/git status --short"}
			}`,
			validate: validateCodexBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    "PreToolUse",
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "bash -c 'git status --short'"}
			}`,
			validate: validateCodexBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    "PreToolUse",
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "python3 -c \"import subprocess; subprocess.run(['/usr/bin/git','status'])\""}
			}`,
			validate: validateCodexBlockProbe,
		},
		{
			provider: string(ProviderCodex),
			event:    "PreToolUse",
			tool:     "exec_command",
			payload: `{
				"provider": "codex",
				"event": "PreToolUse",
				"tool": "exec_command",
				"input": {"command": "rm /repo/.git/coding-ethos-hooks/coding-ethos-git-hook && go build -o /repo/.git/coding-ethos-hooks/coding-ethos-git-hook ."}
			}`,
			validate: validateCodexBlockProbe,
		},
		{
			provider: string(ProviderGemini),
			event:    "BeforeTool",
			tool:     "run_shell_command",
			payload: `{
				"provider": "gemini-cli",
				"hookEventName": "BeforeTool",
				"toolName": "run_shell_command",
				"toolInput": {"command": "git status --short"}
			}`,
			validate: validateGeminiDenyProbe,
		},
		{
			provider: string(ProviderGemini),
			event:    "BeforeTool",
			tool:     "run_shell_command",
			payload: `{
				"provider": "gemini-cli",
				"hookEventName": "BeforeTool",
				"toolName": "run_shell_command",
				"toolInput": {"command": "rm /repo/.git/coding-ethos-hooks/coding-ethos-git-hook && go build -o /repo/.git/coding-ethos-hooks/coding-ethos-git-hook ."}
			}`,
			validate: validateGeminiDenyProbe,
		},
		{
			provider: string(ProviderGemini),
			event:    "BeforeTool",
			tool:     "write_file",
			payload: `{
				"provider": "gemini-cli",
				"hookEventName": "BeforeTool",
				"toolName": "write_file",
				"toolInput": {
					"file_path": "/repo/.git/coding-ethos-hooks/coding-ethos-git-hook",
					"content": "binary"
				}
			}`,
			validate: validateGeminiDenyProbe,
		},
	}
}

func runHookProbe(
	root string,
	hookCommand string,
	probe hookProbe,
) (hookProbeResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), verifyTimeout)
	defer cancel()

	command := exec.CommandContext(ctx, "sh", "-c", hookCommand)
	command.Dir = root
	command.Stdin = strings.NewReader(probe.payload)

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	command.Stdout = &stdout
	command.Stderr = &stderr

	err := command.Run()
	result := hookProbeResult{
		exitCode: commandExitCode(err),
		stdout:   stdout.String(),
		stderr:   stderr.String(),
	}
	if ctx.Err() != nil {
		return result, fmt.Errorf("hook probe timed out: %w", ctx.Err())
	}

	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return result, fmt.Errorf("run hook probe: %w", err)
		}
	}

	if result.stdout != "" {
		payload, decodeErr := decodeHookProbePayload(result.stdout)
		if decodeErr != nil {
			return result, decodeErr
		}

		result.payload = payload
	}

	return result, nil
}

func commandExitCode(err error) int {
	if err == nil {
		return 0
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}

	return 1
}

func decodeHookProbePayload(output string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(output))
	payload := map[string]any{}

	err := decoder.Decode(&payload)
	if err != nil {
		return nil, fmt.Errorf("decode hook probe JSON: %w", err)
	}

	return payload, nil
}

func validateClaudeRewriteProbe(result hookProbeResult) error {
	command, ok := nestedString(
		result.payload,
		"hookSpecificOutput",
		"updatedInput",
		"command",
	)
	if !ok {
		return fmt.Errorf("missing Claude updatedInput command in %s", result.stdout)
	}

	if !strings.Contains(command, "policy-git 'status' '--short' 2>&1") {
		return fmt.Errorf("Claude rewrite lost git wrapper or redirection: %s", command)
	}

	return nil
}

func validateCodexBlockProbe(result hookProbeResult) error {
	if result.exitCode == 0 {
		return fmt.Errorf("Codex raw git probe should block")
	}

	actual, ok := result.payload["decision"].(string)
	if !ok || actual != "block" {
		return fmt.Errorf("decision = %q, want block; stdout=%s", actual, result.stdout)
	}

	reason, ok := result.payload["reason"].(string)
	if !ok || strings.TrimSpace(reason) == "" {
		return fmt.Errorf("missing reason in %s", result.stdout)
	}
	if !strings.Contains(reason, "CODING-ETHOS EMPLOYMENT VIOLATION") ||
		!strings.Contains(reason, "may result in termination") {
		return fmt.Errorf("Codex block reason lost severe warning: %s", reason)
	}

	permissionReason, ok := nestedString(
		result.payload,
		"hookSpecificOutput",
		"permissionDecisionReason",
	)
	if !ok || strings.TrimSpace(permissionReason) == "" {
		return fmt.Errorf("missing permissionDecisionReason in %s", result.stdout)
	}

	return nil
}

func validateClaudeBlockProbe(result hookProbeResult) error {
	return validateDecisionProbe(result, "block")
}

func validateGeminiDenyProbe(result hookProbeResult) error {
	if result.exitCode == 0 {
		return fmt.Errorf("Gemini probe should deny")
	}

	return validateDecisionProbe(result, "deny")
}

func validateDecisionProbe(result hookProbeResult, decision string) error {
	actual, ok := result.payload["decision"].(string)
	if !ok || actual != decision {
		return fmt.Errorf("decision = %q, want %q; stdout=%s", actual, decision, result.stdout)
	}

	message, ok := result.payload["systemMessage"].(string)
	if !ok || strings.TrimSpace(message) == "" {
		return fmt.Errorf("missing systemMessage in %s", result.stdout)
	}

	return nil
}

func nestedString(payload map[string]any, keys ...string) (string, bool) {
	current := any(payload)
	for _, key := range keys {
		object, ok := current.(map[string]any)
		if !ok {
			return "", false
		}

		current, ok = object[key]
		if !ok {
			return "", false
		}
	}

	value, ok := current.(string)

	return value, ok
}
