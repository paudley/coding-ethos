// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
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

	return append([]string(nil), tool.Command...)
}

func toolchainCommandWithFiles(name string, files []string) []string {
	command := toolchainCommand(name)

	return append(command, files...)
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

	if len(tool.BaseNamePrefixes) > 0 && !baseNameMatches(path, tool.BaseNamePrefixes) {
		return false
	}

	if len(tool.FilePrefixes) > 0 && !pathPrefixMatches(path, tool.FilePrefixes) {
		return false
	}

	if len(tool.FileExtensions) > 0 && !extensionMatches(path, tool.FileExtensions) {
		return false
	}

	return len(tool.BaseNamePrefixes) > 0 ||
		len(tool.FilePrefixes) > 0 ||
		len(tool.FileExtensions) > 0
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

func toolchainCommandWithRepoConfig(name string, configPath string) []string {
	tool := toolchainCatalogTool(name)

	command := append([]string(nil), tool.Command...)
	if configPath != "" && len(tool.ConfigFlags) > 0 {
		command = append(command, tool.ConfigFlags[0], configPath)
	}

	command = append(command, tool.PostConfigArgs...)

	return command
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

func parseCatalogFindings(tool string, output string) []hookFinding {
	catalogTool := toolchainCatalogTool(tool)
	diagnostics := diag.Parse(catalogTool.Parser, output, "")
	diagnostics = diag.Enrich(diagnostics, loadHookEvidenceMaps())

	return diagnosticsToHookFindings(diagnostics)
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
	_, _, rootConfig, err := loadBundleConsumerAndConfig()
	if err != nil {
		return defaultPolicyEvidenceMaps()
	}

	maps := make([]diag.EvidenceMap, 0, len(defaultPolicyEvidenceMaps()))

	err = decodeConfigSection(rootConfig, "policy.evidence_maps", &maps)
	if err != nil || len(maps) == 0 {
		return defaultPolicyEvidenceMaps()
	}

	return append(maps, defaultPolicyEvidenceMaps()...)
}
