// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

// Package toolaliases centralizes provider-native agent tool names.
package toolaliases

import "strings"

const (
	CanonicalShell     = "Bash"
	CanonicalWrite     = "Write"
	CanonicalEdit      = "Edit"
	CanonicalMultiEdit = "MultiEdit"
	CanonicalNoop      = "Noop"

	ProviderClaude = "claude"
	ProviderCodex  = "codex"
	ProviderGemini = "gemini"
)

type Alias struct {
	Provider  string
	Canonical string
	Name      string
	Note      string
	Active    bool
	Regex     bool
}

func KnownAliases() []Alias {
	return []Alias{
		{Provider: ProviderClaude, Canonical: CanonicalShell, Name: "Bash", Active: true},
		{Provider: ProviderClaude, Canonical: CanonicalWrite, Name: "Write", Active: true},
		{Provider: ProviderClaude, Canonical: CanonicalEdit, Name: "Edit", Active: true},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalMultiEdit,
			Name:      "MultiEdit",
			Active:    true,
		},
		// Known Claude tools wired to the checker as no-ops; no enforcement dispatch uses them yet.
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "Read",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "Grep",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "Glob",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "LS",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "Task",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "TodoWrite",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "NotebookRead",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "NotebookEdit",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "WebFetch",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderClaude,
			Canonical: CanonicalNoop,
			Name:      "WebSearch",
			Note:      "not used for enforcement yet",
		},

		{Provider: ProviderCodex, Canonical: CanonicalShell, Name: "Bash", Active: true},
		{Provider: ProviderCodex, Canonical: CanonicalShell, Name: "bash", Active: true},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "exec_command",
			Active:    true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "functions.exec_command",
			Active:    true,
			Regex:     true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "run_command",
			Active:    true,
		},
		{Provider: ProviderCodex, Canonical: CanonicalShell, Name: "run_shell", Active: true},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "run_shell_command",
			Active:    true,
		},
		{Provider: ProviderCodex, Canonical: CanonicalShell, Name: "shell", Active: true},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "shell_command",
			Active:    true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "write_stdin",
			Active:    true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "functions.write_stdin",
			Active:    true,
			Regex:     true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalShell,
			Name:      "multi_tool_use.parallel",
			Active:    true,
			Regex:     true,
		},
		{Provider: ProviderCodex, Canonical: CanonicalWrite, Name: "Write", Active: true},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalWrite,
			Name:      "create_file",
			Active:    true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalWrite,
			Name:      "write_file",
			Active:    true,
		},
		{Provider: ProviderCodex, Canonical: CanonicalEdit, Name: "Edit", Active: true},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalEdit,
			Name:      "apply_patch",
			Active:    true,
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalEdit,
			Name:      "functions.apply_patch",
			Active:    true,
			Regex:     true,
		},
		{Provider: ProviderCodex, Canonical: CanonicalEdit, Name: "edit_file", Active: true},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalMultiEdit,
			Name:      "MultiEdit",
			Active:    true,
		},
		// Known Codex tools wired to the checker as no-ops; no enforcement dispatch uses them yet.
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "functions.update_plan",
			Regex:     true,
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "update_plan",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "functions.request_user_input",
			Regex:     true,
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "request_user_input",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "functions.view_image",
			Regex:     true,
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "view_image",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "functions.list_mcp_resources",
			Regex:     true,
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "functions.list_mcp_resource_templates",
			Regex:     true,
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderCodex,
			Canonical: CanonicalNoop,
			Name:      "functions.read_mcp_resource",
			Regex:     true,
			Note:      "not used for enforcement yet",
		},

		{
			Provider:  ProviderGemini,
			Canonical: CanonicalShell,
			Name:      "run_shell_command",
			Active:    true,
		},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalWrite,
			Name:      "write_file",
			Active:    true,
		},
		{Provider: ProviderGemini, Canonical: CanonicalEdit, Name: "replace", Active: true},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalMultiEdit,
			Name:      "MultiEdit",
			Active:    true,
		},
		// Known Gemini-style tools wired to the checker as no-ops; no enforcement dispatch uses them yet.
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalNoop,
			Name:      "read_file",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalNoop,
			Name:      "list_directory",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalNoop,
			Name:      "search_file_content",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalNoop,
			Name:      "glob",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalNoop,
			Name:      "web_fetch",
			Note:      "not used for enforcement yet",
		},
		{
			Provider:  ProviderGemini,
			Canonical: CanonicalNoop,
			Name:      "google_web_search",
			Note:      "not used for enforcement yet",
		},
	}
}

func ProviderAliases(provider, canonical string) []Alias {
	matches := []Alias{}

	for _, alias := range KnownAliases() {
		if alias.Provider == provider && alias.Canonical == canonical {
			matches = append(matches, alias)
		}
	}

	return matches
}

func ActiveCanonical(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}

	for _, alias := range KnownAliases() {
		if alias.Active && alias.Name == name {
			return alias.Canonical, true
		}
	}

	if strings.HasSuffix(name, ".apply_patch") {
		return CanonicalEdit, true
	}

	if strings.HasSuffix(name, ".exec_command") ||
		strings.HasSuffix(name, ".write_stdin") {
		return CanonicalShell, true
	}

	return "", false
}

func NoopCanonical(name string) bool {
	name = strings.TrimSpace(name)
	for _, alias := range KnownAliases() {
		if !alias.Active && alias.Name == name {
			return true
		}
	}

	return false
}
