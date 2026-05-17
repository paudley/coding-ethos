// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package geminiprompts

type selectorSpec struct {
	ExcludePrefixes             []string
	ExcludeSubstrings           []string
	IncludeExtensions           []string
	ShebangMarkers              []string
	AllowExtensionlessInScripts bool
}

type checkSpec struct {
	FileScope     string
	Selector      selectorSpec
	BatchSize     int
	MaxFileSizeKB int
}

type principle struct {
	AgentHints map[string]string
	ID         string
	Title      string
	Directive  string
	Summary    string
	QuickRef   []string
	Order      int
}

type repoData struct {
	Name        string
	Overview    string
	Commands    []repoCommand
	Paths       []repoPath
	Notes       []string
	GeminiNotes []string
}

type repoCommand struct {
	Name     string   `json:"name"`
	Examples []string `json:"examples"`
}

type repoPath struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type principlePayload struct {
	AgentHint string   `json:"agent_hint"`
	Directive string   `json:"directive"`
	ID        string   `json:"id"`
	Summary   string   `json:"summary"`
	Title     string   `json:"title"`
	QuickRef  []string `json:"quick_ref"`
	Order     int      `json:"order"`
}

type promptContext struct {
	ProjectName      string
	ProjectContext   string
	RepoOverview     string
	Principles       []principlePayload
	RepoCommands     []repoCommand
	RepoPaths        []repoPath
	RepoNotes        []string
	GeminiNotes      []string
	EnforcementNotes []string
}
