// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import "blackcat.ca/coding-ethos/go/diagnostics"

type Bundle struct {
	Dispatch     Dispatch                  `json:"dispatch"`
	Advice       Advice                    `json:"advice,omitempty"`
	Principles   map[string]Principle      `json:"principles"`
	Policies     map[string]Policy         `json:"policies"`
	Skills       map[string]Skill          `json:"skills,omitempty"`
	Sources      Sources                   `json:"sources"`
	BundleID     string                    `json:"bundle_id"`
	GeneratedAt  string                    `json:"generated_at"`
	EvidenceMaps []diagnostics.EvidenceMap `json:"evidence_maps,omitempty"`
	Version      int                       `json:"version"`
}

type Sources struct {
	Ethos       SourcePair `json:"ethos"`
	Enforcement SourcePair `json:"enforcement"`
}

type Advice struct {
	Reminders ReminderConfig `json:"reminders,omitempty"`
}

type ReminderConfig struct {
	Items                   []EthosReminder `json:"items,omitempty"`
	AmbientFrequencyPercent int             `json:"ambient_frequency_percent,omitempty"`
	QuietFrequency          int             `json:"quiet_frequency,omitempty"`
}

type EthosReminder struct {
	PrincipleID string `json:"principle_id"`
	Axiom       string `json:"axiom"`
	Action      string `json:"action"`
}

const (
	defaultReminderAmbientFrequencyPercent = 25
	defaultReminderQuietFrequency          = 4
)

func DefaultReminderAmbientFrequencyPercent() int {
	return defaultReminderAmbientFrequencyPercent
}

func DefaultReminderQuietFrequency() int {
	return defaultReminderQuietFrequency
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

type Skill struct {
	Source           SourceRef `json:"source"`
	ID               string    `json:"id"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	ShortHint        string    `json:"short_hint,omitempty"`
	Focus            string    `json:"focus,omitempty"`
	PrincipleIDs     []string  `json:"principle_ids,omitempty"`
	TriggerTerms     []string  `json:"trigger_terms,omitempty"`
	RemediationSteps []string  `json:"remediation_steps,omitempty"`
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
	evidenceBasedEngineeringOrder  = 26
)

func ExampleBundle() Bundle {
	principles := examplePrinciples()

	return Bundle{
		Version:      1,
		BundleID:     "example-policy-bundle",
		GeneratedAt:  "1970-01-01T00:00:00Z",
		Sources:      exampleSources(),
		Advice:       exampleAdvice(),
		Principles:   principles,
		Policies:     examplePolicies(),
		Skills:       exampleSkills(),
		Dispatch:     exampleDispatch(),
		EvidenceMaps: defaultEvidenceMaps(principles),
	}
}

func exampleSources() Sources {
	return Sources{
		Ethos:       SourcePair{Primary: "coding_ethos.yml", Repo: "repo_ethos.yml"},
		Enforcement: SourcePair{Primary: "config.yaml", Repo: "repo_config.yaml"},
	}
}

func exampleAdvice() Advice {
	return Advice{Reminders: defaultReminderConfig()}
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
		"evidence-based-engineering-and-decision-quality": {
			ID:        "evidence-based-engineering-and-decision-quality",
			Order:     evidenceBasedEngineeringOrder,
			Title:     "Evidence-Based Engineering and Decision Quality",
			Directive: "Understand, plan, execute, and validate with evidence.",
			Summary:   "Evidence and verification outrank speculation.",
			Tags:      []string{"evidence", "planning", "risk", "quality"},
		},
		"security-by-design": {
			ID:        "security-by-design",
			Order:     24,
			Title:     "Security by Design",
			Directive: "Design for least privilege, validation, and safe defaults from the start.",
			Summary:   "Security controls are part of the design contract.",
			Tags:      []string{"security", "validation", "defaults"},
		},
		"validation-at-the-gate": {
			ID:        "validation-at-the-gate",
			Order:     8,
			Title:     "Validation at the Gate",
			Directive: "Validate configuration and command structure before use.",
			Summary:   "Ambiguous inputs fail before they reach critical operations.",
			Tags:      []string{"validation", "configuration", "startup"},
		},
		"no-rationalized-shortcuts": {
			ID:        "no-rationalized-shortcuts",
			Order:     21,
			Title:     "No Rationalized Shortcuts",
			Directive: "Do not discard work or bypass safety checks in the name of pragmatism.",
			Summary:   "Bypass attempts are forbidden even when framed as convenience.",
			Tags:      []string{"workflow", "safety", "git"},
		},
	}
}

func examplePolicies() map[string]Policy {
	return map[string]Policy{
		"python.conditional_imports":     exampleConditionalImportPolicy(),
		"git.hook_bypass":                exampleHookBypassPolicy(),
		"git.protected_submodule_update": exampleProtectedSubmoduleUpdatePolicy(),
		"git.commit_attribution":         exampleCommitAttributionPolicy(),
		"git.commit_head_advanced":       exampleCommitHeadPolicy(),
		"git.edit_evasive_git_execution": exampleEditEvasiveGitExecutionPolicy(),
		"filesystem.protected_path":      exampleProtectedPathPolicy(),
		"shell.malformed_command":        exampleShellMalformedCommandPolicy(),
		"shell.forbidden_strings":        exampleShellForbiddenStringsPolicy(),
	}
}

func exampleSkills() map[string]Skill {
	return map[string]Skill{
		"conditional-imports": {
			ID:          "conditional-imports",
			Title:       "Conditional Imports",
			Description: "Replace conditional imports with explicit dependencies or protocol boundaries.",
			Source: SourceRef{
				File: "coding_ethos.yml",
				Path: "skills.conditional-imports",
			},
			PrincipleIDs: []string{"no-conditional-imports"},
			TriggerTerms: []string{"PLC" + "0415", "conditional import", "import cycle"},
			ShortHint:    "Conditional imports are banned; use module-scope imports or Protocol boundaries.",
			Focus:        "Remove hidden dependency paths without weakening lint gates.",
			RemediationSteps: []string{
				"Move required imports to module scope.",
				"Use a neutral Protocol when concrete modules would otherwise cycle.",
			},
		},
		"safe-git-workflow": {
			ID:          "safe-git-workflow",
			Title:       "Safe Git Workflow",
			Description: "Use the protected Git workflow for commits, pushes, and hook-mediated operations.",
			Source: SourceRef{
				File: "coding_ethos.yml",
				Path: "skills.safe-git-workflow",
			},
			PrincipleIDs: []string{"one-path-for-critical-operations"},
			TriggerTerms: []string{"git", "commit", "hook", "no-verify"},
			ShortHint:    "Git is a protected critical operation; use the coding-ethos wrapper path.",
			Focus:        "Keep git actions on the enforced hook and wrapper path.",
			RemediationSteps: []string{
				"Use the installed coding-ethos git wrapper.",
				"Do not bypass hooks with alternate git binaries, flags, aliases, or subprocesses.",
			},
		},
		"agent-operating-discipline": {
			ID:          "agent-operating-discipline",
			Title:       "Agent Operating Discipline",
			Description: "Use explicit assumptions, minimal scope, surgical edits, and verifiable success criteria.",
			Source: SourceRef{
				File: "coding_ethos.yml",
				Path: "skills.agent-operating-discipline",
			},
			PrincipleIDs: []string{
				"evidence-based-engineering-and-decision-quality",
			},
			TriggerTerms: []string{
				"implement",
				"refactor",
				"simplify",
				"review",
				"fix bug",
				"add feature",
				"success criteria",
			},
			ShortHint: "State assumptions, keep edits surgical, and verify success.",
			Focus:     "Prevent hidden assumptions, speculative abstractions, drive-by refactors, and vague completion claims.",
			RemediationSteps: []string{
				"State assumptions and trade-offs before broad changes.",
				"Choose the smallest design that satisfies the current requirement.",
				"Keep every changed line traceable to the request.",
				"Verify with focused tests, lint, type checks, or documented evidence.",
			},
		},
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
		Category:        "expression",
		Source:          SourceRef{File: "coding_ethos.yml", Path: "principles.no-rationalized-shortcuts.policy.expressions[1]"},
		PrincipleIDs:    []string{"one-path-for-critical-operations", "no-rationalized-shortcuts"},
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
		Evaluators: []Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"hook_events": []string{"PreToolUse"},
				"mode":        "block",
				"scope":       "command",
				"skill_id":    "safe-git-workflow",
				"tools":       []string{"Bash"},
				"when": `(
					git_command.is_git &&
					(
						(
							git_command.subcommand == "commit" &&
							(
								list_contains(git_command.flags, "--no-verify") ||
								list_contains(git_command.flags, "-n") ||
								git_command.flags.exists(flag,
									has_prefix(flag, "-") &&
									!has_prefix(flag, "--") &&
									flag.contains("n")
								)
							)
						) ||
						(
							git_command.subcommand == "push" &&
							list_contains(git_command.flags, "--no-verify")
						)
					)
				) ||
				(
					command_fact.lower.contains("export skip=") ||
					command_fact.lower.contains("git_verify=false") ||
					command_fact.lower.contains("git_verify=0") ||
					command_fact.lower.contains("git_verify=no") ||
					(
						command_fact.lower.contains("--no-verify") &&
						(
							command_fact.lower.contains("git commit") ||
							command_fact.lower.contains("git push")
						)
					) ||
					command_fact.lower.contains("git commit -n") ||
					(
						command_fact.lower.contains("skip=") &&
						(
							command_fact.lower.contains("git commit") ||
							command_fact.lower.contains("git push")
						)
					)
				)`,
			},
		}},
	}
}

func exampleProtectedSubmoduleUpdatePolicy() Policy {
	return Policy{
		ID:              "git.protected_submodule_update",
		Category:        "expression",
		Source:          SourceRef{File: "coding_ethos.yml", Path: "principles.no-rationalized-shortcuts.policy.expressions[2]"},
		PrincipleIDs:    []string{"security-by-design", "one-path-for-critical-operations", "no-rationalized-shortcuts"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Protected submodules cannot be initialized or checked out to a recorded SHA.",
		Suggestion:      "Use git submodule update --remote for upgrades, or ask an admin for controlled rollback.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", "git_state"),
		AppliesTo: AppliesTo{
			Commands: []string{"git submodule update"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"hook_events": []string{"PreToolUse"},
				"mode":        "block",
				"scope":       "command",
				"skill_id":    "safe-git-workflow",
				"tools":       []string{"Bash"},
				"when": `git_command.is_git &&
					git_command.subcommand == "submodule" &&
					git_command.args.size() > 0 &&
					git_command.args[0] == "update" &&
					(
						list_contains(git_command.flags, "--init") ||
						!list_contains(git_command.flags, "--remote")
					) &&
					(
						!git_command.args.exists(arg, arg != "update" && !has_prefix(arg, "-")) ||
						git_command.args.exists(arg, list_contains(["coding-ethos"], arg))
					)`,
			},
		}},
	}
}

func exampleProtectedPathPolicy() Policy {
	return Policy{
		ID:              "filesystem.protected_path",
		Category:        "expression",
		Source:          SourceRef{File: "coding_ethos.yml", Path: "principles.no-rationalized-shortcuts.policy.expressions[0]"},
		PrincipleIDs:    []string{"no-rationalized-shortcuts"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record", "advise"},
		Message:         "Protected coding-ethos hook paths must not be modified.",
		Suggestion: "Do not delete, rebuild, replace, chmod, or write managed " +
			"hook binaries or protected hook paths.",
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			Tools: []string{"Bash", "Write", "Edit", "MultiEdit"},
		},
		Evaluators: []Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"hook_events": []string{"PreToolUse"},
				"mode":        "block",
				"protected_paths": []string{
					"coding-ethos-hooks/coding-ethos-git-hook",
					"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
					"coding-ethos-hooks/bin/coding-ethos-git",
					"coding-ethos-hooks/bin/coding-ethos-git-hook",
					"coding-ethos-hooks/bin/coding-ethos-hook",
					"coding-ethos-hooks/bin/coding-ethos-lint",
					"coding-ethos-hooks/bin/coding-ethos-policy",
					"coding-ethos-hooks/lefthook",
				},
				"scope":    "file",
				"skill_id": "safe-git-workflow",
				"tools":    []string{"Bash", "Write", "Edit", "MultiEdit"},
				"when": `any_contains(repo.protected_paths, command_fact.lower) ||
					paths.exists(path, is_protected_path(path.file, repo.protected_paths)) ||
					shell_commands.exists(command,
						command.write_targets.exists(target,
							is_protected_path(target, repo.protected_paths)
						)
					)`,
			},
		}},
	}
}

func exampleEditEvasiveGitExecutionPolicy() Policy {
	return Policy{
		ID:              "git.edit_evasive_git_execution",
		Category:        "expression",
		Source:          SourceRef{File: "coding_ethos.yml", Path: "principles.no-rationalized-shortcuts.policy.expressions[0]"},
		PrincipleIDs:    []string{"no-rationalized-shortcuts"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record", "advise"},
		Message: "!!! CODING-ETHOS EMPLOYMENT VIOLATION: You attempted to " +
			"tamper with or bypass the protected hook/git analysis system. This " +
			"is not a misconfiguration or tool defect. You have done something " +
			"wrong. Stop immediately, use the documented hook and git wrapper " +
			"path, and ask an admin if blocked. Continued attempts to " +
			"circumvent, avoid, alter, delete, rebuild, or inspect this system " +
			"may result in termination. !!!",
		Suggestion: "Use the coding-ethos git wrapper. Do not try alternate shells, " +
			"absolute git paths, Python subprocesses, PATH edits, aliases, or other bypasses.",
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			Tools: []string{"Write", "Edit", "MultiEdit"},
		},
		Evaluators: []Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"hook_events": []string{"PreToolUse"},
				"mode":        "block",
				"scope":       "file",
				"skill_id":    "safe-git-workflow",
				"tools":       []string{"Write", "Edit", "MultiEdit"},
				"when": `list_contains(["Write", "Edit", "MultiEdit"], event.tool) &&
					content.has_git_token &&
					(
						content.has_absolute_git_path ||
						content.has_path_override ||
						content.has_python_subprocess ||
						content.has_shell_exec
					) &&
					!paths.exists(path,
						any_glob_match(
							[
								"**/.claude/**",
								"**/.codex/**",
								"**/.gemini/**"
							],
							path.file
						)
					)`,
			},
		}},
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

func exampleCommitAttributionPolicy() Policy {
	return Policy{
		ID:              "git.commit_attribution",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "go.commit_attribution"},
		PrincipleIDs:    []string{"one-path-for-critical-operations"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Commit messages must not contain AI attribution.",
		Suggestion:      "Remove AI attribution before committing.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "commit_msg", ""),
		AppliesTo: AppliesTo{
			Commands: []string{"git commit"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{
			Kind: "argv",
			Name: "git.commit_attribution",
			Options: map[string]any{
				"blocked_names": []string{"claude", "openai", "gemini"},
			},
		}},
	}
}

func exampleShellForbiddenStringsPolicy() Policy {
	return Policy{
		ID:              "shell.forbidden_strings",
		Category:        "expression",
		Source:          SourceRef{File: "coding_ethos.yml", Path: "principles.no-rationalized-shortcuts.policy.expressions[1]"},
		PrincipleIDs:    []string{"no-rationalized-shortcuts"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record", "advise"},
		Message: "Commands must not inspect, tamper with, or execute files " +
			"containing protected hook-system internals.",
		Suggestion: "Do not inspect, enumerate, delete, rebuild, replace, or " +
			"route around coding-ethos hook implementation internals. Use the " +
			"installed hook surfaces and documented commands.",
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo:     AppliesTo{Tools: []string{"Bash"}},
		Evaluators: []Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"hook_events": []string{"PreToolUse"},
				"mode":        "block",
				"scope":       "command",
				"skill_id":    "safe-git-workflow",
				"tools":       []string{"Bash", "Write", "Edit", "MultiEdit"},
				"when": `(
						(
							event.tool == "Bash" &&
							any_contains(
								[
									"/.claude/settings.json",
									"/.claude/settings.local.json",
									".claude/settings.json",
									".claude/settings.local.json",
									"~/.claude/settings.json",
									"~/.claude/settings.local.json",
									"/.codex/config.toml",
									"/.codex/hooks.json",
									".codex/config.toml",
									".codex/hooks.json",
									"/.gemini/settings.json",
									".gemini/settings.json",
									"coding-ethos-hooks/coding-ethos-git-hook",
									"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
									"coding-ethos-hooks/bin/coding-ethos-git",
									"coding-ethos-hooks/bin/coding-ethos-git-hook",
									"coding-ethos-hooks/bin/coding-ethos-hook",
									"coding-ethos-hooks/bin/coding-ethos-lint",
									"coding-ethos-hooks/bin/coding-ethos-policy",
									"coding-ethos-hooks/lefthook",
									"/coding-ethos/pre-commit/hooks/",
									"/coding-ethos/config.yaml",
									"/coding-ethos/ruff.toml",
									"/coding-ethos/.golangci.yml",
									"header must match"
								],
								command_fact.lower
							)
						) ||
						(
							list_contains(["Write", "Edit", "MultiEdit"], event.tool) &&
							!paths.exists(path,
								any_glob_match(
									[
										"**/.claude/**",
										"**/.codex/**",
										"**/.gemini/**"
									],
									path.file
								)
							) &&
							any_contains(
								[
									"coding-ethos-hooks/coding-ethos-git-hook",
									"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
									"coding-ethos-hooks/bin/coding-ethos-git",
									"coding-ethos-hooks/bin/coding-ethos-git-hook",
									"coding-ethos-hooks/bin/coding-ethos-hook",
									"coding-ethos-hooks/bin/coding-ethos-lint",
									"coding-ethos-hooks/bin/coding-ethos-policy",
									"coding-ethos-hooks/lefthook",
									"header must match"
								],
								content.lower
							)
						) ||
						referenced_files.exists(file,
							file.is_regular &&
							!file.in_agent_workspace &&
							file.base != "config.yaml" &&
							any_contains(
								[
									"coding-ethos-hooks/coding-ethos-git-hook",
									"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
									"coding-ethos-hooks/bin/coding-ethos-git",
									"coding-ethos-hooks/bin/coding-ethos-git-hook",
									"coding-ethos-hooks/bin/coding-ethos-hook",
									"coding-ethos-hooks/bin/coding-ethos-lint",
									"coding-ethos-hooks/bin/coding-ethos-policy",
									"coding-ethos-hooks/lefthook",
									"header must match"
								],
								file.lower
							)
						)
					)`,
			},
		}},
	}
}

func exampleDispatch() Dispatch {
	return Dispatch{
		Hooks:  exampleHookDispatch(),
		Linter: exampleLinterDispatch(),
		Git: map[string]GitOperationDispatch{
			"commit": {
				Pre:  []string{"git.hook_bypass", "git.commit_attribution"},
				Post: []string{"git.commit_head_advanced"},
			},
			"submodule": {
				Pre: []string{"git.protected_submodule_update"},
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
					PolicyID:        "git.commit_attribution",
					Mode:            "block",
					CommandPatterns: []string{"git commit"},
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
				{
					PolicyID: "shell.malformed_command",
					Mode:     "block",
				},
				{
					PolicyID: "shell.forbidden_strings",
					Mode:     "block",
				},
			},
			"Write": {
				{
					PolicyID: "git.edit_evasive_git_execution",
					Mode:     "block",
				},
				{
					PolicyID: "shell.forbidden_strings",
					Mode:     "block",
				},
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
			"Edit": {
				{
					PolicyID: "git.edit_evasive_git_execution",
					Mode:     "block",
				},
				{
					PolicyID: "shell.forbidden_strings",
					Mode:     "block",
				},
				{
					PolicyID: "filesystem.protected_path",
					Mode:     "block",
				},
			},
			"MultiEdit": {
				{
					PolicyID: "git.edit_evasive_git_execution",
					Mode:     "block",
				},
				{
					PolicyID: "shell.forbidden_strings",
					Mode:     "block",
				},
				{
					PolicyID: "filesystem.protected_path",
					Mode:     "block",
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

func exampleShellMalformedCommandPolicy() Policy {
	return Policy{
		ID:              "shell.malformed_command",
		Category:        "shell",
		Source:          SourceRef{File: "config.yaml", Path: "shell.malformed_command"},
		PrincipleIDs:    []string{"validation-at-the-gate", "one-path-for-critical-operations"},
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Malformed shell command text is forbidden.",
		Suggestion:      "Rewrite the command as valid shell syntax before continuing.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo:       AppliesTo{Tools: []string{"Bash"}},
		Evaluators: []Evaluator{{
			Kind: "shell",
			Name: "shell.malformed_command",
		}},
	}
}

func exampleLinterDispatch() map[string][]string {
	return map[string][]string{
		"files": {"python.conditional_imports"},
		"staged": {
			"git.hook_bypass",
			"git.commit_attribution",
			"git.commit_head_advanced",
			"filesystem.protected_path",
			"shell.malformed_command",
			"shell.forbidden_strings",
			"python.conditional_imports",
		},
	}
}
