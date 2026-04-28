// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
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

	return diagnosticsToHookFindings(diag.Parse(catalogTool.Parser, output, ""))
}

func diagnosticsToHookFindings(items []diag.Diagnostic) []hookFinding {
	if len(items) == 0 {
		return nil
	}

	findings := make([]hookFinding, 0, len(items))
	for _, item := range items {
		findings = append(findings, hookFinding{
			Tool:     item.Tool,
			File:     item.File,
			Line:     item.Line,
			Column:   item.Column,
			Severity: item.Severity,
			Code:     item.Code,
			Message:  item.Message,
		})
	}

	return findings
}
