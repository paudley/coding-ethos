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
	DetailPath string            `json:"detail_path,omitempty"`
	Directive  string            `json:"directive,omitempty"`
	ID         string            `json:"id"`
	Summary    string            `json:"summary,omitempty"`
	Title      string            `json:"title"`
	QuickRef   []string          `json:"quick_ref,omitempty"`
	Related    []string          `json:"related,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Order      int               `json:"order,omitempty"`
}

type Policy struct {
	Source          SourceRef     `json:"source"`
	Category        string        `json:"category"`
	DefaultSeverity string        `json:"default_severity"`
	ID              string        `json:"id"`
	Message         string        `json:"message"`
	Suggestion      string        `json:"suggestion,omitempty"`
	AppliesTo       AppliesTo     `json:"applies_to,omitzero"`
	DefenseLayers   DefenseLayers `json:"defense_layers"`
	Evaluators      []Evaluator   `json:"evaluators"`
	PrincipleIDs    []string      `json:"principle_ids,omitempty"`
	SupportedModes  []string      `json:"supported_modes"`
}

type DefenseLayers struct {
	Enforce   string `json:"enforce,omitempty"`
	Intercept string `json:"intercept,omitempty"`
	Mediate   string `json:"mediate,omitempty"`
	Detect    string `json:"detect,omitempty"`
	Notify    string `json:"notify,omitempty"`
	Verify    string `json:"verify,omitempty"`
	Persuade  bool   `json:"persuade"`
	Record    bool   `json:"record"`
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
	PolicyID        string   `json:"policy_id"`
	Mode            string   `json:"mode"`
	CommandPatterns []string `json:"command_patterns,omitempty"`
	PathPatterns    []string `json:"path_patterns,omitempty"`
}

type GitOperationDispatch struct {
	Pre  []string `json:"pre,omitempty"`
	Post []string `json:"post,omitempty"`
}

const (
	noConditionalImportsOrder      = 3
	onePathCriticalOperationsOrder = 19
)

func ExampleBundle() Bundle {
	return Bundle{
		Version:     1,
		BundleID:    "example-policy-bundle",
		GeneratedAt: "1970-01-01T00:00:00Z",
		Sources:     exampleSources(),
		Principles:  examplePrinciples(),
		Policies:    examplePolicies(),
		Dispatch:    exampleDispatch(),
	}
}

func exampleSources() Sources {
	return Sources{
		Ethos:       SourcePair{Primary: "coding_ethos.yml", Repo: "repo_ethos.yml"},
		Enforcement: SourcePair{Primary: "config.yaml", Repo: "repo_config.yaml"},
	}
}

func examplePrinciples() map[string]Principle {
	return map[string]Principle{
		"no-conditional-imports": {
			ID:    "no-conditional-imports",
			Order: noConditionalImportsOrder,
			Title: "No Conditional Imports",
			Directive: sentence(
				"Treat required imports as hard dependencies and fail",
				"immediately if they are missing.",
			),
			Summary:    "Required imports are hard dependencies.",
			DetailPath: ".agents/ethos/no-conditional-imports.md",
			Tags:       []string{"dependency", "startup", "reliability"},
		},
		"one-path-for-critical-operations": {
			ID:        "one-path-for-critical-operations",
			Order:     onePathCriticalOperationsOrder,
			Title:     "One Path for Critical Operations",
			Directive: "Keep one explicit, validated path for critical operations.",
			Summary:   "Critical operations use canonical gates.",
			Tags:      []string{"workflow", "validation", "reliability"},
		},
	}
}

func examplePolicies() map[string]Policy {
	return map[string]Policy{
		"python.conditional_imports": exampleConditionalImportPolicy(),
		"git.hook_bypass":            exampleHookBypassPolicy(),
		"git.commit_head_advanced":   exampleCommitHeadPolicy(),
		"filesystem.protected_path":  exampleProtectedPathPolicy(),
	}
}

func exampleConditionalImportPolicy() Policy {
	return Policy{
		ID:              "python.conditional_imports",
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.conditional_imports"},
		PrincipleIDs:    []string{"no-conditional-imports"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message: sentence(
			"Required dependencies should fail immediately;",
			"ImportError fallback creates a soft dependency path.",
		),
		Suggestion: sentence(
			"Remove the conditional import or configure an",
			"explicit exemption.",
		),
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			Languages:    []string{"python"},
			FilePatterns: []string{"**/*.py"},
		},
		Evaluators: []Evaluator{{Kind: "ast", Name: "python.conditional_imports"}},
	}
}

func exampleHookBypassPolicy() Policy {
	return Policy{
		ID:              "git.hook_bypass",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.hook_bypass"},
		PrincipleIDs:    []string{"one-path-for-critical-operations"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Hook bypass is forbidden.",
		Suggestion:      "Run the configured gate and fix the underlying failure.",
		DefenseLayers: GitDefenseLayers(
			"block",
			"wrapper",
			"block",
			"pre_commit",
			"git_state",
		),
		AppliesTo: AppliesTo{
			Commands: []string{"git commit", "git push"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{Kind: "argv", Name: "git.hook_bypass"}},
	}
}

func exampleProtectedPathPolicy() Policy {
	return Policy{
		ID:              "filesystem.protected_path",
		Category:        "filesystem",
		Source:          SourceRef{File: "config.yaml", Path: "filesystem.protected_path"},
		PrincipleIDs:    []string{"one-path-for-critical-operations"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Protected paths must not be modified.",
		Suggestion:      "Do not write to protected system paths.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo: AppliesTo{
			Paths: []string{"/usr/bin/got"},
			Tools: []string{"Bash", "Write", "Edit", "MultiEdit"},
		},
		Evaluators: []Evaluator{{Kind: "path", Name: "filesystem.protected_path"}},
	}
}

func exampleCommitHeadPolicy() Policy {
	return Policy{
		ID:              "git.commit_head_advanced",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.commit_head_advanced"},
		PrincipleIDs:    []string{"one-path-for-critical-operations"},
		DefaultSeverity: "annotate",
		SupportedModes:  []string{"annotate", "record", "block"},
		Message:         "Commit success must be verified by checking that HEAD advanced.",
		Suggestion: sentence(
			"Compare pre-commit and post-commit HEAD before",
			"reporting success.",
		),
		DefenseLayers: GitDefenseLayers("", "wrapper", "record", "", "git_state"),
		AppliesTo: AppliesTo{
			Commands: []string{"git commit"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{Kind: "git_state", Name: "git.commit_head_advanced"}},
	}
}

func exampleDispatch() Dispatch {
	return Dispatch{
		Hooks:  exampleHookDispatch(),
		Linter: exampleLinterDispatch(),
		Git: map[string]GitOperationDispatch{
			"commit": {
				Pre:  []string{"git.hook_bypass"},
				Post: []string{"git.commit_head_advanced"},
			},
		},
	}
}

func exampleHookDispatch() map[string]map[string][]HookDispatchEntry {
	return map[string]map[string][]HookDispatchEntry{
		"PreToolUse": {
			"Bash": {
				{
					PolicyID:        "git.hook_bypass",
					Mode:            "block",
					CommandPatterns: []string{"--no-verify", "SKIP=", "git commit -n"},
				},
				{
					PolicyID:        "git.commit_head_advanced",
					Mode:            "record",
					CommandPatterns: []string{"git commit"},
				},
				{
					PolicyID: "filesystem.protected_path",
					Mode:     "block",
				},
			},
			"Write": {
				{
					PolicyID: "filesystem.protected_path",
					Mode:     "block",
				},
				{
					PolicyID:     "python.conditional_imports",
					Mode:         "advise",
					PathPatterns: []string{"**/*.py"},
				},
			},
		},
		"PostToolUse": {
			"Bash": {
				{
					PolicyID:        "git.commit_head_advanced",
					Mode:            "block",
					CommandPatterns: []string{"git commit"},
				},
			},
		},
	}
}

func exampleLinterDispatch() map[string][]string {
	return map[string][]string{
		"files": {"python.conditional_imports"},
		"staged": {
			"git.hook_bypass",
			"git.commit_head_advanced",
			"filesystem.protected_path",
			"python.conditional_imports",
		},
	}
}
