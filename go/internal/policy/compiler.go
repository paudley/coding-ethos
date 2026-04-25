// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

type CompileOptions struct {
	GeneratedAt string
	BundleID    string
	Primary     string
	RepoEthos   string
	Config      string
	RepoConfig  string
}

func Compile(options CompileOptions) (Bundle, Metadata, error) {
	if options.Primary == "" {
		options.Primary = "coding_ethos.yml"
	}
	if options.Config == "" {
		options.Config = "config.yaml"
	}
	if options.GeneratedAt == "" {
		options.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	primaryPayload, primaryHash, err := loadYAMLFile(options.Primary)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}
	configPayload, configHash, err := loadYAMLFile(options.Config)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	sourceHashes := map[string]string{
		options.Primary: primaryHash,
		options.Config:  configHash,
	}

	if options.RepoEthos != "" && fileExists(options.RepoEthos) {
		repoEthosPayload, repoEthosHash, err := loadYAMLFile(options.RepoEthos)
		if err != nil {
			return Bundle{}, Metadata{}, err
		}
		sourceHashes[options.RepoEthos] = repoEthosHash
		primaryPayload = mergeMaps(primaryPayload, repoEthosPayload)
	}

	if options.RepoConfig != "" && fileExists(options.RepoConfig) {
		repoConfigPayload, repoConfigHash, err := loadYAMLFile(options.RepoConfig)
		if err != nil {
			return Bundle{}, Metadata{}, err
		}
		sourceHashes[options.RepoConfig] = repoConfigHash
		configPayload = mergeMaps(configPayload, repoConfigPayload)
	}

	principles := compilePrinciples(primaryPayload)
	if len(principles) == 0 {
		return Bundle{}, Metadata{}, fmt.Errorf("compile principles: no principles found in %s", options.Primary)
	}

	policies := compilePolicies(configPayload, principles)
	if len(policies) == 0 {
		return Bundle{}, Metadata{}, fmt.Errorf("compile policies: no enabled policies found in %s", options.Config)
	}

	bundleID := options.BundleID
	if bundleID == "" {
		bundleID = defaultBundleID(options.Primary, options.Config, sourceHashes)
	}

	bundle := Bundle{
		Version:     1,
		BundleID:    bundleID,
		GeneratedAt: options.GeneratedAt,
		Sources: Sources{
			Ethos: SourcePair{
				Primary: options.Primary,
				Repo:    options.RepoEthos,
			},
			Enforcement: SourcePair{
				Primary: options.Config,
				Repo:    options.RepoConfig,
			},
		},
		Principles: principles,
		Policies:   policies,
		Dispatch:   compileDispatch(policies),
	}
	if err := bundle.Validate(); err != nil {
		return Bundle{}, Metadata{}, err
	}

	metadata, err := BuildMetadata(bundle, sourceHashes)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}
	return bundle, metadata, nil
}

func loadYAMLFile(path string) (map[string]any, string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read YAML %s: %w", path, err)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(payload, &decoded); err != nil {
		return nil, "", fmt.Errorf("parse YAML %s: %w", path, err)
	}
	sum := sha256.Sum256(payload)
	return decoded, "sha256:" + hex.EncodeToString(sum[:]), nil
}

func compilePrinciples(payload map[string]any) map[string]Principle {
	principles := map[string]Principle{}
	rawPrinciples, ok := payload["principles"].([]any)
	if !ok {
		return principles
	}
	for _, raw := range rawPrinciples {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		id := stringValue(item["id"])
		if id == "" {
			continue
		}
		principles[id] = Principle{
			ID:         id,
			Order:      intValue(item["order"]),
			Title:      stringValue(item["title"]),
			Summary:    stringValue(item["summary"]),
			Directive:  stringValue(item["directive"]),
			QuickRef:   stringSlice(item["quick_ref"]),
			Tags:       stringSlice(item["tags"]),
			Related:    stringSlice(item["related"]),
			AgentHints: stringMap(item["agent_hints"]),
			DetailPath: filepath.ToSlash(filepath.Join(".agents", "ethos", id+".md")),
		}
	}
	return principles
}

func compilePolicies(config map[string]any, principles map[string]Principle) map[string]Policy {
	policies := map[string]Policy{}
	addPolicyIfEnabled(policies, config, principles, "python.conditional_imports", []string{"python", "conditional_imports"}, Policy{
		ID:              "python.conditional_imports",
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.conditional_imports"},
		PrincipleIDs:    principleRefs(principles, "no-conditional-imports"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         "Required dependencies should fail immediately; ImportError fallback creates a soft dependency path.",
		Suggestion:      "Remove the conditional import or configure an explicit exemption.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{Languages: []string{"python"}, FilePatterns: []string{"**/*.py"}},
		Evaluators:      []Evaluator{{Kind: "ast", Name: "python.conditional_imports"}},
	})
	addPolicyIfEnabled(policies, config, principles, "python.optional_returns", []string{"python", "optional_returns"}, Policy{
		ID:              "python.optional_returns",
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.optional_returns"},
		PrincipleIDs:    principleRefs(principles, "no-optional-types-for-required-dependencies"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         "Required values should not be modeled as optional returns unless explicitly exempted.",
		Suggestion:      "Use a required return type or configure a narrow exemption.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{Languages: []string{"python"}, FilePatterns: []string{"**/*.py"}},
		Evaluators:      []Evaluator{{Kind: "ast", Name: "python.optional_returns"}},
	})
	addPolicyIfEnabled(policies, config, principles, "python.catch_and_silence", []string{"python", "catch_and_silence"}, Policy{
		ID:              "python.catch_and_silence",
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.catch_and_silence"},
		PrincipleIDs:    principleRefs(principles, "fail-fast-fail-hard-overview", "exception-hierarchy-and-error-messages"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         "Silent exception handling hides failures and violates fail-fast behavior.",
		Suggestion:      "Handle the exception explicitly or let it fail with useful context.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{Languages: []string{"python"}, FilePatterns: []string{"**/*.py"}},
		Evaluators:      []Evaluator{{Kind: "ast", Name: "python.catch_and_silence"}},
	})
	addPolicyIfEnabled(policies, config, principles, "python.structured_logging", []string{"python", "structured_logging"}, Policy{
		ID:              "python.structured_logging",
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.structured_logging"},
		PrincipleIDs:    principleRefs(principles, "radical-visibility"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         "Logging should preserve structured context instead of formatting it away.",
		Suggestion:      "Use structured logging fields according to the repo policy.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{Languages: []string{"python"}, FilePatterns: []string{"**/*.py"}},
		Evaluators:      []Evaluator{{Kind: "ast", Name: "python.structured_logging"}},
	})
	addPolicyIfEnabled(policies, config, principles, "python.direct_imports", []string{"python", "direct_imports"}, Policy{
		ID:              "python.direct_imports",
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.direct_imports"},
		PrincipleIDs:    principleRefs(principles, "protocol-first-design"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         "Direct imports from protected packages bypass the intended public interface.",
		Suggestion:      "Import through the package public API or configure an exempt path.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{Languages: []string{"python"}, FilePatterns: []string{"**/*.py"}},
		Evaluators:      []Evaluator{{Kind: "ast", Name: "python.direct_imports"}},
	})
	addPolicyIfEnabled(policies, config, principles, "pytest.gate", []string{"python", "pytest_gate"}, Policy{
		ID:              "pytest.gate",
		Category:        "pytest",
		Source:          SourceRef{File: "config.yaml", Path: "python.pytest_gate"},
		PrincipleIDs:    principleRefs(principles, "testing-as-specification"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "prepare", "annotate", "record"},
		Message:         "The configured pytest gate must pass before claiming readiness.",
		Suggestion:      "Run the configured pytest gate and address failures.",
		DefenseLayers:   PytestDefenseLayers(),
		AppliesTo:       AppliesTo{Commands: []string{"pytest"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "external", Name: "pytest.gate"}},
	})

	policies["git.hook_bypass"] = Policy{
		ID:              "git.hook_bypass",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.hook_bypass"},
		PrincipleIDs:    principleRefs(principles, "one-path-for-critical-operations", "linting-as-code-quality-enforcement"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Hook bypass is forbidden.",
		Suggestion:      "Run the configured gate and fix the underlying failure.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "pre_commit", "git_state"),
		AppliesTo:       AppliesTo{Commands: []string{"git commit", "git push"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.hook_bypass"}},
	}
	policies["git.destructive_command"] = Policy{
		ID:              "git.destructive_command",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.destructive_command"},
		PrincipleIDs:    principleRefs(principles, "no-rationalized-shortcuts"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Destructive git commands are forbidden.",
		Suggestion:      "Preserve work and resolve the current state explicitly.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git reset", "git clean", "git checkout", "git restore"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.destructive_command"}},
	}
	policies["git.merge_strategy_shortcut"] = Policy{
		ID:              "git.merge_strategy_shortcut",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.merge_strategy_shortcut"},
		PrincipleIDs:    principleRefs(principles, "no-rationalized-shortcuts"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "git merge -X theirs/ours destroys conflict evidence.",
		Suggestion:      "Resolve each conflict explicitly instead of using blanket merge strategies.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git merge"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.merge_strategy_shortcut"}},
	}
	policies["git.force_push_protected_branch"] = Policy{
		ID:              "git.force_push_protected_branch",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.force_push_protected_branch"},
		PrincipleIDs:    principleRefs(principles, "no-rationalized-shortcuts", "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Force push to protected branches is forbidden.",
		Suggestion:      "Use the repository's normal review and merge path.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "pre_push", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git push"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.force_push_protected_branch"}},
	}
	policies["git.checkout_protected_branch"] = Policy{
		ID:              "git.checkout_protected_branch",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.checkout_protected_branch"},
		PrincipleIDs:    principleRefs(principles, "forward-motion-only"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "record"},
		Message:         "Switching to main/master to check history is forbidden in managed workflows.",
		Suggestion:      "Inspect history with git fetch, git show, or git diff without switching branches.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git checkout", "git switch"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.checkout_protected_branch"}},
	}
	policies["git.destructive_worktree"] = Policy{
		ID:              "git.destructive_worktree",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.destructive_worktree"},
		PrincipleIDs:    principleRefs(principles, "no-rationalized-shortcuts"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Destructive git worktree operations are forbidden.",
		Suggestion:      "Inspect worktree state and remove or move worktrees only through explicit safe steps.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git worktree"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.destructive_worktree"}},
	}
	policies["git.change_dir_flag"] = Policy{
		ID:              "git.change_dir_flag",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.change_dir_flag"},
		PrincipleIDs:    principleRefs(principles, "evidence-based-engineering-and-decision-quality"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "git -C hides the working directory context.",
		Suggestion:      "Change to the intended directory explicitly, then run git there.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: "git.change_dir_flag"}},
	}
	if _, ok := principles["no-rationalized-shortcuts"]; ok {
		policies["git.stash_blocked"] = Policy{
			ID:              "git.stash_blocked",
			Category:        "git",
			Source:          SourceRef{File: "coding_ethos.yml", Path: "principles.no-rationalized-shortcuts"},
			PrincipleIDs:    principleRefs(principles, "no-rationalized-shortcuts"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "git stash hides working state and is forbidden when the stash ethos is active.",
			Suggestion:      "Keep changes visible in the worktree or commit them through the normal validated path.",
			DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
			AppliesTo:       AppliesTo{Commands: []string{"git stash"}, Tools: []string{"Bash"}},
			Evaluators:      []Evaluator{{Kind: "argv", Name: "git.stash_blocked"}},
		}
	}
	policies["git.staged_admin_files"] = Policy{
		ID:              "git.staged_admin_files",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.staged_admin_files"},
		PrincipleIDs:    principleRefs(principles, "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "record"},
		Message:         "Administrative staged files require explicit handling before commit.",
		Suggestion:      "Confirm the policy/config change is intentional or move it to a separate admin commit.",
		DefenseLayers:   GitDefenseLayers("ask", "wrapper", "block", "pre_commit", "git_state"),
		AppliesTo:       AppliesTo{Commands: []string{"git commit"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "git_state", Name: "git.staged_admin_files"}},
	}
	policies["shell.dangerous_command"] = Policy{
		ID:              "shell.dangerous_command",
		Category:        "shell",
		Source:          SourceRef{File: "config.yaml", Path: "shell.dangerous_command"},
		PrincipleIDs:    principleRefs(principles, "security-by-design", "no-rationalized-shortcuts"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Dangerous shell commands are forbidden.",
		Suggestion:      "Use reviewed, explicit commands instead of broad destructive or pipe-to-shell patterns.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"rm", "curl", "wget", "chmod"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "shell", Name: "shell.dangerous_command"}},
	}
	policies["shell.background_git"] = Policy{
		ID:              "shell.background_git",
		Category:        "shell",
		Source:          SourceRef{File: "config.yaml", Path: "shell.background_git"},
		PrincipleIDs:    principleRefs(principles, "evidence-based-engineering-and-decision-quality", "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "git commit and git push must not run in the background or under timeout.",
		Suggestion:      "Run git commit or git push in the foreground so hooks and results are visible.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", "git_state"),
		AppliesTo:       AppliesTo{Commands: []string{"git commit", "git push"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "shell", Name: "shell.background_git"}},
	}
	policies["git.commit_head_advanced"] = Policy{
		ID:              "git.commit_head_advanced",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.commit_head_advanced"},
		PrincipleIDs:    principleRefs(principles, "evidence-based-engineering-and-decision-quality"),
		DefaultSeverity: "annotate",
		SupportedModes:  []string{"annotate", "record"},
		Message:         "Commit success must be verified by checking that HEAD advanced.",
		Suggestion:      "Compare pre-commit and post-commit HEAD before reporting success.",
		DefenseLayers:   GitDefenseLayers("", "wrapper", "record", "", "git_state"),
		AppliesTo:       AppliesTo{Commands: []string{"git commit"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "git_state", Name: "git.commit_head_advanced"}},
	}
	policies["generated_config.freshness"] = Policy{
		ID:              "generated_config.freshness",
		Category:        "config",
		Source:          SourceRef{File: "config.yaml", Path: "generated_config.freshness"},
		PrincipleIDs:    principleRefs(principles, "static-analysis-is-the-first-line-of-defense"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "advise", "annotate", "record"},
		Message:         "Generated tool configuration must match coding-ethos source policy.",
		Suggestion:      "Run the configured tool-config sync/check command.",
		DefenseLayers:   GeneratedConfigDefenseLayers(),
		AppliesTo:       AppliesTo{Paths: []string{"ruff.toml", "mypy.ini", "pyrightconfig.json", ".yamllint.yml"}},
		Evaluators:      []Evaluator{{Kind: "config", Name: "generated_config.freshness"}},
	}

	return policies
}

func addPolicyIfEnabled(
	policies map[string]Policy,
	config map[string]any,
	_ map[string]Principle,
	id string,
	path []string,
	policy Policy,
) {
	if boolAt(config, append(path, "enabled")...) {
		policies[id] = policy
	}
}

func compileDispatch(policies map[string]Policy) Dispatch {
	hooks := map[string]map[string][]HookDispatchEntry{}
	if _, ok := policies["git.hook_bypass"]; ok {
		ensureHookTool(hooks, "PreToolUse", "Bash")
		hooks["PreToolUse"]["Bash"] = append(hooks["PreToolUse"]["Bash"], HookDispatchEntry{
			PolicyID:        "git.hook_bypass",
			Mode:            "block",
			CommandPatterns: []string{"--no-verify", "SKIP=", "git commit -n"},
		})
	}
	for _, id := range []string{
		"git.destructive_command",
		"git.merge_strategy_shortcut",
		"git.force_push_protected_branch",
		"git.checkout_protected_branch",
		"git.destructive_worktree",
		"git.change_dir_flag",
		"git.stash_blocked",
		"shell.dangerous_command",
		"shell.background_git",
	} {
		if _, ok := policies[id]; ok {
			ensureHookTool(hooks, "PreToolUse", "Bash")
			hooks["PreToolUse"]["Bash"] = append(hooks["PreToolUse"]["Bash"], HookDispatchEntry{
				PolicyID: id,
				Mode:     "block",
			})
		}
	}
	for _, id := range []string{
		"python.conditional_imports",
		"python.optional_returns",
		"python.catch_and_silence",
		"python.structured_logging",
		"python.direct_imports",
	} {
		if _, ok := policies[id]; ok {
			for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
				ensureHookTool(hooks, "PreToolUse", tool)
				hooks["PreToolUse"][tool] = append(hooks["PreToolUse"][tool], HookDispatchEntry{
					PolicyID:     id,
					Mode:         "advise",
					PathPatterns: []string{"**/*.py"},
				})
			}
		}
	}
	if _, ok := policies["pytest.gate"]; ok {
		ensureHookTool(hooks, "PostToolUse", "Bash")
		hooks["PostToolUse"]["Bash"] = append(hooks["PostToolUse"]["Bash"], HookDispatchEntry{
			PolicyID:        "pytest.gate",
			Mode:            "annotate",
			CommandPatterns: []string{"pytest", "make check", "lefthook"},
		})
	}

	linter := map[string][]string{
		"files":  existingPolicyIDs(policies, "python.conditional_imports", "python.optional_returns", "python.catch_and_silence", "python.structured_logging", "python.direct_imports"),
		"staged": existingPolicyIDs(policies, "git.hook_bypass", "git.destructive_command", "git.merge_strategy_shortcut", "git.force_push_protected_branch", "git.checkout_protected_branch", "git.destructive_worktree", "git.change_dir_flag", "git.stash_blocked", "shell.dangerous_command", "shell.background_git", "git.staged_admin_files", "generated_config.freshness", "python.conditional_imports", "python.optional_returns", "python.catch_and_silence", "python.structured_logging", "python.direct_imports"),
		"full":   existingPolicyIDs(policies, "pytest.gate", "generated_config.freshness"),
	}
	if _, ok := policies["pytest.gate"]; ok {
		linter["smoke"] = []string{"pytest.gate"}
	}

	return Dispatch{
		Hooks:  hooks,
		Linter: linter,
		Git: map[string]GitOperationDispatch{
			"commit": {
				Pre:  existingPolicyIDs(policies, "git.hook_bypass", "git.staged_admin_files"),
				Post: existingPolicyIDs(policies, "git.commit_head_advanced"),
			},
			"push": {
				Pre: existingPolicyIDs(policies, "git.hook_bypass", "git.force_push_protected_branch"),
			},
			"-C": {
				Pre: existingPolicyIDs(policies, "git.change_dir_flag"),
			},
			"reset": {
				Pre: existingPolicyIDs(policies, "git.destructive_command"),
			},
			"clean": {
				Pre: existingPolicyIDs(policies, "git.destructive_command"),
			},
			"checkout": {
				Pre: existingPolicyIDs(policies, "git.destructive_command", "git.checkout_protected_branch"),
			},
			"switch": {
				Pre: existingPolicyIDs(policies, "git.checkout_protected_branch"),
			},
			"restore": {
				Pre: existingPolicyIDs(policies, "git.destructive_command"),
			},
			"merge": {
				Pre: existingPolicyIDs(policies, "git.merge_strategy_shortcut"),
			},
			"worktree": {
				Pre: existingPolicyIDs(policies, "git.destructive_worktree"),
			},
			"stash": {
				Pre: existingPolicyIDs(policies, "git.stash_blocked"),
			},
		},
	}
}

func ensureHookTool(hooks map[string]map[string][]HookDispatchEntry, event string, tool string) {
	if _, ok := hooks[event]; !ok {
		hooks[event] = map[string][]HookDispatchEntry{}
	}
	if _, ok := hooks[event][tool]; !ok {
		hooks[event][tool] = []HookDispatchEntry{}
	}
}

func existingPolicyIDs(policies map[string]Policy, ids ...string) []string {
	existing := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := policies[id]; ok {
			existing = append(existing, id)
		}
	}
	return existing
}

func principleRefs(principles map[string]Principle, ids ...string) []string {
	refs := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := principles[id]; ok {
			refs = append(refs, id)
		}
	}
	return refs
}

func mergeMaps(base map[string]any, overlay map[string]any) map[string]any {
	for key, overlayValue := range overlay {
		baseMap, baseOK := base[key].(map[string]any)
		overlayMap, overlayOK := overlayValue.(map[string]any)
		if baseOK && overlayOK {
			base[key] = mergeMaps(baseMap, overlayMap)
			continue
		}
		base[key] = overlayValue
	}
	return base
}

func boolAt(values map[string]any, path ...string) bool {
	value, ok := valueAt(values, path...)
	if !ok {
		return false
	}
	boolValue, ok := value.(bool)
	return ok && boolValue
}

func valueAt(values map[string]any, path ...string) (any, bool) {
	current := any(values)
	for _, part := range path {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = currentMap[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func intValue(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func stringSlice(value any) []string {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	items := make([]string, 0, len(rawItems))
	for _, raw := range rawItems {
		items = append(items, stringValue(raw))
	}
	return items
}

func stringMap(value any) map[string]string {
	raw, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	items := map[string]string{}
	for key, value := range raw {
		items[key] = stringValue(value)
	}
	return items
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func defaultBundleID(primary string, config string, hashes map[string]string) string {
	parts := []string{primary, config}
	for path, hash := range hashes {
		parts = append(parts, path+"="+hash)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "policy-" + hex.EncodeToString(sum[:8])
}
