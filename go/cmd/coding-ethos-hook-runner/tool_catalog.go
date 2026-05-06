// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	diag "blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/toolcatalog"
)

func toolchainCatalogTool(name string) toolcatalog.Tool {
	tool, ok := toolcatalog.ToolchainTool(name)
	if !ok {
		return toolcatalog.Tool{Name: name, Parser: name, Command: []string{name}}
	}

	return tool
}

func toolchainCommand(name string) []string {
	tool := toolchainCatalogTool(name)
	runtime := tool.RuntimeSpec()
	command := append([]string(nil), runtime.Command...)

	if len(command) == 0 {
		return command
	}

	if managed := managedToolchainBinaryPath(tool); managed != "" {
		command[0] = managed

		return command
	}

	return command
}

func toolchainCommandWithFiles(name string, files []string) []string {
	command := toolchainCommand(name)

	return append(command, files...)
}

func toolchainCommandForFiles(name string, files []string) []string {
	tool := toolchainCatalogTool(name)
	runtime := tool.RuntimeSpec()
	fileSpec := tool.FileMatchSpec()

	if runtime.Runtime == toolcatalog.RuntimeUV ||
		runtime.Runtime == toolcatalog.RuntimePython {
		if !fileSpec.PassFilesAsArgs {
			files = nil
		}

		return uvToolchainCommandWithRepoConfig(name, toolchainRepoConfig(name), files)
	}

	if !fileSpec.PassFilesAsArgs {
		return toolchainCommand(name)
	}

	return toolchainCommandWithFiles(name, files)
}

func toolchainFiles(name string, paths []string) []string {
	tool := toolchainCatalogTool(name)
	files := make([]string, 0, len(paths))

	for _, path := range paths {
		if toolchainFileMatches(tool, path) {
			files = append(files, path)
		}
	}

	return files
}

func toolchainFileMatches(tool toolcatalog.Tool, path string) bool {
	if tool.Name == "shellcheck" && isShellFile(path) {
		return true
	}

	spec := tool.FileMatchSpec()

	if len(spec.BaseNamePrefixes) > 0 && !baseNameMatches(path, spec.BaseNamePrefixes) {
		return false
	}

	if len(spec.Prefixes) > 0 && !pathPrefixMatches(path, spec.Prefixes) {
		return false
	}

	if len(spec.Extensions) > 0 && !extensionMatches(path, spec.Extensions) {
		return false
	}

	return len(spec.BaseNamePrefixes) > 0 ||
		len(spec.Prefixes) > 0 ||
		len(spec.Extensions) > 0
}

func baseNameMatches(path string, prefixes []string) bool {
	name := filepath.Base(path)
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}

	return false
}

func pathPrefixMatches(path string, prefixes []string) bool {
	normalized := filepath.ToSlash(path)
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}

	return false
}

func extensionMatches(path string, extensions []string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	for _, allowed := range extensions {
		if ext == strings.ToLower(allowed) {
			return true
		}
	}

	return false
}

func toolchainCommandWithRepoConfig(name, configPath string) []string {
	tool := toolchainCatalogTool(name)
	runtime := tool.RuntimeSpec()
	config := tool.ConfigSpec()

	command := append([]string(nil), runtime.Command...)
	if configPath != "" && len(config.Flags) > 0 {
		command = append(command, config.Flags[0], configPath)
	}

	command = append(command, config.PostArgs...)

	return command
}

func toolchainRepoConfig(name string) string {
	config := toolchainCatalogTool(name).ConfigSpec()
	if config.RepoConfig != "" {
		return config.RepoConfig
	}

	return config.FallbackBundleConfig
}

func uvToolchainCommandWithRepoConfig(
	name string,
	configPath string,
	files []string,
) []string {
	uvPrefix := []string{
		"uv",
		"run",
		"--quiet",
		"--project",
		hooksProjectPath(),
		"--with",
		name,
	}
	command := make([]string, 0, len(uvPrefix)+len(files))
	command = append(command,
		uvPrefix...,
	)
	command = append(command, toolchainCommandWithRepoConfig(name, configPath)...)
	command = append(command, files...)

	return command
}

func parseCatalogFindings(tool, output string) []hookFinding {
	catalogTool := toolchainCatalogTool(tool)
	diagnostics := diag.Parse(catalogTool.Parser, output, "")
	diagnostics = diag.Enrich(diagnostics, loadHookEvidenceMaps())

	return diagnosticsToHookFindings(diagnostics)
}

func managedToolchainBinaryPath(tool toolcatalog.Tool) string {
	if tool.Name == "" {
		return ""
	}

	bundleRoot, err := findBundleRoot()
	if err != nil {
		return ""
	}

	ethosRoot := filepath.Dir(bundleRoot)
	runtime := tool.RuntimeSpec().Runtime

	switch runtime {
	case toolcatalog.RuntimeGo:
		return filepath.Join(ethosRoot, "build", "toolchain", "go-bin", tool.Name)
	case toolcatalog.RuntimeBinary:
		return filepath.Join(ethosRoot, "build", "toolchain", "github-bin", tool.Name)
	case toolcatalog.RuntimePython, toolcatalog.RuntimeUV:
		return ""
	default:
		return ""
	}
}

func diagnosticsToHookFindings(items []diag.Diagnostic) []hookFinding {
	if len(items) == 0 {
		return nil
	}

	findings := make([]hookFinding, 0, len(items))
	for _, item := range items {
		findings = append(findings, hookFinding{
			Metadata:     item.Metadata,
			Advice:       item.Advice,
			Confidence:   item.Confidence,
			Tool:         item.Tool,
			File:         item.File,
			Line:         item.Line,
			Column:       item.Column,
			Severity:     item.Severity,
			Code:         item.Code,
			PolicyID:     item.PolicyID,
			SkillID:      item.SkillID,
			Message:      item.Message,
			Meaning:      item.Meaning,
			AdviceSteps:  append([]string(nil), item.AdviceSteps...),
			PrincipleIDs: append([]string(nil), item.PrincipleIDs...),
			Rerun:        append([]string(nil), item.Rerun...),
			Tags:         append([]string(nil), item.Tags...),
		})
	}

	return findings
}

func loadHookEvidenceMaps() []diag.EvidenceMap {
	bundleRoot, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return nil
	}

	maps, err := loadCompiledEvidenceMaps(bundleRoot)
	if err != nil || len(maps) == 0 {
		maps = configuredEvidenceMaps(rootConfig)
	}

	if len(maps) == 0 {
		return nil
	}

	return maps
}

func loadCompiledEvidenceMaps(bundleRoot string) ([]diag.EvidenceMap, error) {
	bundlePath := filepath.Join(
		filepath.Dir(bundleRoot),
		"build",
		"policy",
		"policy-bundle.json",
	)

	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read policy bundle evidence maps: %w", err)
	}

	var payload struct {
		EvidenceMaps []diag.EvidenceMap `json:"evidence_maps"`
	}

	err = json.Unmarshal(data, &payload)
	if err != nil {
		return nil, fmt.Errorf("decode policy bundle evidence maps: %w", err)
	}

	return payload.EvidenceMaps, nil
}

type hookSkill struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	ShortHint    string   `json:"short_hint"`
	PrincipleIDs []string `json:"principle_ids"`
}

func loadHookSkills() map[string]hookSkill {
	bundleRoot, _, _, err := loadBundleConsumerAndConfig()
	if err != nil {
		return nil
	}

	skills, err := loadCompiledSkills(bundleRoot)
	if err != nil {
		return nil
	}

	return skills
}

func loadCompiledSkills(bundleRoot string) (map[string]hookSkill, error) {
	bundlePath := filepath.Join(
		filepath.Dir(bundleRoot),
		"build",
		"policy",
		"policy-bundle.json",
	)

	data, err := os.ReadFile(bundlePath)
	if err != nil {
		return nil, fmt.Errorf("read policy bundle skills: %w", err)
	}

	var payload struct {
		Skills map[string]hookSkill `json:"skills"`
	}

	err = json.Unmarshal(data, &payload)
	if err != nil {
		return nil, fmt.Errorf("decode policy bundle skills: %w", err)
	}

	return payload.Skills, nil
}

func configuredEvidenceMaps(rootConfig map[string]any) []diag.EvidenceMap {
	var maps []diag.EvidenceMap

	err := decodeConfigSection(rootConfig, "policy.evidence_maps", &maps)
	if err != nil {
		return nil
	}

	return maps
}
