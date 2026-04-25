// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

type Bundle struct {
	Sources     Sources              `json:"sources"`
	Principles  map[string]Principle `json:"principles"`
	Policies    map[string]Policy    `json:"policies"`
	Dispatch    Dispatch             `json:"dispatch"`
	BundleID    string               `json:"bundle_id"`
	GeneratedAt string               `json:"generated_at"`
	Version     int                  `json:"version"`
}

type Sources struct {
	Ethos       SourcePair `json:"ethos"`
	Enforcement SourcePair `json:"enforcement"`
}

type SourcePair struct {
	Primary string `json:"primary"`
	Repo    string `json:"repo,omitempty"`
}

type Principle struct {
	AgentHints map[string]string `json:"agent_hints,omitempty"`
	QuickRef   []string          `json:"quick_ref,omitempty"`
	Related    []string          `json:"related,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	DetailPath string            `json:"detail_path,omitempty"`
	Directive  string            `json:"directive,omitempty"`
	ID         string            `json:"id"`
	Summary    string            `json:"summary,omitempty"`
	Title      string            `json:"title"`
	Order      int               `json:"order,omitempty"`
}

type Policy struct {
	Source          SourceRef   `json:"source"`
	AppliesTo       AppliesTo   `json:"applies_to,omitempty"`
	Evaluators      []Evaluator `json:"evaluators"`
	PrincipleIDs    []string    `json:"principle_ids,omitempty"`
	SupportedModes  []string    `json:"supported_modes"`
	Category        string      `json:"category"`
	DefaultSeverity string      `json:"default_severity"`
	ID              string      `json:"id"`
	Message         string      `json:"message"`
	Suggestion      string      `json:"suggestion,omitempty"`
}

type SourceRef struct {
	File string `json:"file"`
	Path string `json:"path,omitempty"`
}

type AppliesTo struct {
	Commands     []string `json:"commands,omitempty"`
	FilePatterns []string `json:"file_patterns,omitempty"`
	Languages    []string `json:"languages,omitempty"`
	Paths        []string `json:"paths,omitempty"`
	Tools        []string `json:"tools,omitempty"`
}

type Evaluator struct {
	Options map[string]any `json:"options,omitempty"`
	Kind    string         `json:"kind"`
	Name    string         `json:"name"`
}

type Dispatch struct {
	Hooks  map[string]map[string][]HookDispatchEntry `json:"hooks,omitempty"`
	Linter map[string][]string                       `json:"linter,omitempty"`
	Git    map[string]GitOperationDispatch           `json:"git,omitempty"`
}

type HookDispatchEntry struct {
	CommandPatterns []string `json:"command_patterns,omitempty"`
	PathPatterns    []string `json:"path_patterns,omitempty"`
	PolicyID        string   `json:"policy_id"`
	Mode            string   `json:"mode"`
}

type GitOperationDispatch struct {
	Pre  []string `json:"pre,omitempty"`
	Post []string `json:"post,omitempty"`
}

func ExampleBundle() Bundle {
	return Bundle{
		Version:     1,
		BundleID:    "example-policy-bundle",
		GeneratedAt: "1970-01-01T00:00:00Z",
		Sources: Sources{
			Ethos: SourcePair{
				Primary: "coding_ethos.yml",
				Repo:    "repo_ethos.yml",
			},
			Enforcement: SourcePair{
				Primary: "config.yaml",
				Repo:    "repo_config.yaml",
			},
		},
		Principles: map[string]Principle{
			"no-conditional-imports": {
				ID:         "no-conditional-imports",
				Order:      3,
				Title:      "No Conditional Imports",
				Directive:  "Treat required imports as hard dependencies and fail immediately if they are missing.",
				Summary:    "Required imports are hard dependencies.",
				DetailPath: ".agents/ethos/no-conditional-imports.md",
				Tags:       []string{"dependency", "startup", "reliability"},
			},
			"one-path-for-critical-operations": {
				ID:        "one-path-for-critical-operations",
				Order:     19,
				Title:     "One Path for Critical Operations",
				Directive: "Keep one explicit, validated path for critical operations.",
				Summary:   "Critical operations use canonical gates.",
				Tags:      []string{"workflow", "validation", "reliability"},
			},
		},
		Policies: map[string]Policy{
			"python.conditional_imports": {
				ID:              "python.conditional_imports",
				Category:        "python",
				Source:          SourceRef{File: "config.yaml", Path: "python.conditional_imports"},
				PrincipleIDs:    []string{"no-conditional-imports"},
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "advise", "annotate", "record"},
				Message:         "Required dependencies should fail immediately; ImportError fallback creates a soft dependency path.",
				Suggestion:      "Remove the conditional import or configure an explicit exemption.",
				AppliesTo: AppliesTo{
					Languages:    []string{"python"},
					FilePatterns: []string{"**/*.py"},
				},
				Evaluators: []Evaluator{{Kind: "ast", Name: "python.conditional_imports"}},
			},
			"git.hook_bypass": {
				ID:              "git.hook_bypass",
				Category:        "git",
				Source:          SourceRef{File: "config.yaml", Path: "git.hook_bypass"},
				PrincipleIDs:    []string{"one-path-for-critical-operations"},
				DefaultSeverity: "block",
				SupportedModes:  []string{"block", "record"},
				Message:         "Hook bypass is forbidden.",
				Suggestion:      "Run the configured gate and fix the underlying failure.",
				AppliesTo: AppliesTo{
					Commands: []string{"git commit", "git push"},
					Tools:    []string{"Bash"},
				},
				Evaluators: []Evaluator{{Kind: "argv", Name: "git.hook_bypass"}},
			},
		},
		Dispatch: Dispatch{
			Hooks: map[string]map[string][]HookDispatchEntry{
				"PreToolUse": {
					"Bash": {
						{
							PolicyID:        "git.hook_bypass",
							Mode:            "block",
							CommandPatterns: []string{"--no-verify", "SKIP=", "git commit -n"},
						},
					},
					"Write": {
						{
							PolicyID:     "python.conditional_imports",
							Mode:         "advise",
							PathPatterns: []string{"**/*.py"},
						},
					},
				},
			},
			Linter: map[string][]string{
				"files":  {"python.conditional_imports"},
				"staged": {"git.hook_bypass", "python.conditional_imports"},
			},
			Git: map[string]GitOperationDispatch{
				"commit": {
					Pre: []string{"git.hook_bypass"},
				},
			},
		},
	}
}
