// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

var (
	errNoCompiledPrinciples = errors.New("no principles found")
	errNoCompiledPolicies   = errors.New("no enabled policies found")
)

const defaultBundleBaseParts = 2

type CompileOptions struct {
	GeneratedAt string
	BundleID    string
	Primary     string
	RepoEthos   string
	Config      string
	RepoConfig  string
}

func Compile(options CompileOptions) (Bundle, Metadata, error) {
	options = normalizedCompileOptions(options)

	primaryPayload, configPayload, sourceHashes, err := compileInputs(options)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	principles := compilePrinciples(primaryPayload)
	if len(principles) == 0 {
		return Bundle{}, Metadata{}, fmt.Errorf(
			"compile principles: %w in %s",
			errNoCompiledPrinciples,
			options.Primary,
		)
	}

	policies := compilePolicies(configPayload, principles)
	if len(policies) == 0 {
		return Bundle{}, Metadata{}, fmt.Errorf(
			"compile policies: %w in %s",
			errNoCompiledPolicies,
			options.Config,
		)
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

	err = bundle.Validate()
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	metadata, err := BuildMetadata(bundle, sourceHashes)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}

	return bundle, metadata, nil
}

func normalizedCompileOptions(options CompileOptions) CompileOptions {
	if options.Primary == "" {
		options.Primary = "coding_ethos.yml"
	}

	if options.Config == "" {
		options.Config = "config.yaml"
	}

	if options.GeneratedAt == "" {
		options.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}

	return options
}

func compileInputs(
	options CompileOptions,
) (map[string]any, map[string]any, map[string]string, error) {
	primaryPayload, primaryHash, err := loadYAMLFile(options.Primary)
	if err != nil {
		return nil, nil, nil, err
	}

	configPayload, configHash, err := loadYAMLFile(options.Config)
	if err != nil {
		return nil, nil, nil, err
	}

	sourceHashes := map[string]string{
		options.Primary: primaryHash,
		options.Config:  configHash,
	}

	primaryPayload, err = mergeOptionalYAML(
		primaryPayload,
		options.RepoEthos,
		sourceHashes,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	configPayload, err = mergeOptionalYAML(
		configPayload,
		options.RepoConfig,
		sourceHashes,
	)
	if err != nil {
		return nil, nil, nil, err
	}

	return primaryPayload, configPayload, sourceHashes, nil
}

func mergeOptionalYAML(
	base map[string]any,
	path string,
	sourceHashes map[string]string,
) (map[string]any, error) {
	if path == "" || !fileExists(path) {
		return base, nil
	}

	overlay, hash, err := loadYAMLFile(path)
	if err != nil {
		return nil, err
	}

	sourceHashes[path] = hash

	return mergeMaps(base, overlay), nil
}

func loadYAMLFile(path string) (map[string]any, string, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, "", fmt.Errorf("read YAML %s: %w", path, err)
	}

	var decoded map[string]any

	err = yaml.Unmarshal(payload, &decoded)
	if err != nil {
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

		principleID := stringValue(item["id"])
		if principleID == "" {
			continue
		}

		principles[principleID] = Principle{
			ID:         principleID,
			Order:      intValue(item["order"]),
			Title:      stringValue(item["title"]),
			Summary:    stringValue(item["summary"]),
			Directive:  stringValue(item["directive"]),
			QuickRef:   stringSlice(item["quick_ref"]),
			Tags:       stringSlice(item["tags"]),
			Related:    stringSlice(item["related"]),
			AgentHints: stringMap(item["agent_hints"]),
			DetailPath: filepath.ToSlash(
				filepath.Join(".agents", "ethos", principleID+".md"),
			),
		}
	}

	return principles
}

func compilePolicies(
	config map[string]any,
	principles map[string]Principle,
) map[string]Policy {
	policies := map[string]Policy{}
	addConfiguredPythonPolicies(policies, config, principles)
	addGitPolicies(policies, principles)
	addShellPolicies(policies, principles)
	addGeneratedConfigPolicy(policies, principles)

	return policies
}

func addConfiguredPythonPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
	for _, spec := range pythonPolicySpecs(principles) {
		addPolicyIfEnabled(
			policies,
			config,
			principles,
			spec.ID,
			spec.EnabledPath,
			spec.Policy,
		)
	}
}

type compiledPolicySpec struct {
	Policy      Policy
	ID          string
	EnabledPath []string
}

func pythonPolicySpecs(principles map[string]Principle) []compiledPolicySpec {
	return []compiledPolicySpec{
		pythonPolicySpec(
			"python.conditional_imports",
			[]string{"python", "conditional_imports"},
			principleRefs(principles, "no-conditional-imports"),
		),
		pythonPolicySpec(
			"python.optional_returns",
			[]string{"python", "optional_returns"},
			principleRefs(principles, "no-optional-types-for-required-dependencies"),
		),
		pythonPolicySpec(
			"python.catch_and_silence",
			[]string{"python", "catch_and_silence"},
			principleRefs(
				principles,
				"fail-fast-fail-hard-overview",
				"exception-hierarchy-and-error-messages",
			),
		),
		pythonPolicySpec(
			"python.structured_logging",
			[]string{"python", "structured_logging"},
			principleRefs(principles, "radical-visibility"),
		),
		pythonPolicySpec(
			"python.direct_imports",
			[]string{"python", "direct_imports"},
			principleRefs(principles, "protocol-first-design"),
		),
		pytestGatePolicySpec(principles),
	}
}

func pythonPolicySpec(
	policyID string,
	enabledPath []string,
	principleIDs []string,
) compiledPolicySpec {
	policy := Policy{
		ID:              policyID,
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: policyID},
		PrincipleIDs:    principleIDs,
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "advise", "annotate", "record"},
		Message:         pythonPolicyMessage(policyID),
		Suggestion:      pythonPolicySuggestion(policyID),
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			Languages:    []string{"python"},
			FilePatterns: []string{"**/*.py"},
		},
		Evaluators: []Evaluator{{Kind: "ast", Name: policyID}},
	}

	return compiledPolicySpec{ID: policyID, EnabledPath: enabledPath, Policy: policy}
}

func pytestGatePolicySpec(principles map[string]Principle) compiledPolicySpec {
	policy := Policy{
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
	}

	return compiledPolicySpec{
		ID:          "pytest.gate",
		EnabledPath: []string{"python", "pytest_gate"},
		Policy:      policy,
	}
}

func pythonPolicyMessage(policyID string) string {
	switch policyID {
	case "python.conditional_imports":
		return sentence(
			"Required dependencies should fail immediately;",
			"ImportError fallback creates a soft dependency path.",
		)
	case "python.optional_returns":
		return sentence(
			"Required values should not be modeled as optional",
			"returns unless explicitly exempted.",
		)
	case "python.catch_and_silence":
		return sentence(
			"Silent exception handling hides failures and violates",
			"fail-fast behavior.",
		)
	case "python.structured_logging":
		return sentence(
			"Logging should preserve structured context instead of",
			"formatting it away.",
		)
	default:
		return sentence(
			"Direct imports from protected packages bypass the",
			"intended public interface.",
		)
	}
}

func pythonPolicySuggestion(policyID string) string {
	switch policyID {
	case "python.conditional_imports":
		return "Remove the conditional import or configure an explicit exemption."
	case "python.optional_returns":
		return "Use a required return type or configure a narrow exemption."
	case "python.catch_and_silence":
		return "Handle the exception explicitly or let it fail with useful context."
	case "python.structured_logging":
		return "Use structured logging fields according to the repo policy."
	default:
		return "Import through the package public API or configure an exempt path."
	}
}

func addGitPolicies(policies map[string]Policy, principles map[string]Principle) {
	for _, policy := range gitPolicies(principles) {
		policies[policy.ID] = policy
	}

	if _, ok := principles["no-rationalized-shortcuts"]; ok {
		policies["git.stash_blocked"] = gitStashPolicy(principles)
	}
}

func gitPolicies(principles map[string]Principle) []Policy {
	return []Policy{
		gitPolicy(
			"git.hook_bypass",
			"git.hook_bypass",
			principleRefs(
				principles,
				"one-path-for-critical-operations",
				"linting-as-code-quality-enforcement",
			),
			"Hook bypass is forbidden.",
			"Run the configured gate and fix the underlying failure.",
		),
		gitPolicy(
			"git.destructive_command",
			"git.destructive_command",
			principleRefs(principles, "no-rationalized-shortcuts"),
			"Destructive git commands are forbidden.",
			"Preserve work and resolve the current state explicitly.",
		),
		gitPolicy(
			"git.merge_strategy_shortcut",
			"git.merge_strategy_shortcut",
			principleRefs(principles, "no-rationalized-shortcuts"),
			"git merge -X theirs/ours destroys conflict evidence.",
			"Resolve each conflict explicitly instead of using blanket merge strategies.",
		),
		gitPolicy(
			"git.force_push_protected_branch",
			"git.force_push_protected_branch",
			principleRefs(
				principles,
				"no-rationalized-shortcuts",
				"one-path-for-critical-operations",
			),
			"Force push to protected branches is forbidden.",
			"Use the repository's normal review and merge path.",
		),
		gitPolicy(
			"git.checkout_protected_branch",
			"git.checkout_protected_branch",
			principleRefs(principles, "forward-motion-only"),
			"Switching to main/master to check history is forbidden in managed workflows.",
			"Inspect history with git fetch, git show, or git diff without switching.",
		),
		gitPolicy(
			"git.destructive_worktree",
			"git.destructive_worktree",
			principleRefs(principles, "no-rationalized-shortcuts"),
			"Destructive git worktree operations are forbidden.",
			"Inspect worktree state before changing worktrees.",
		),
		gitChangeDirPolicy(principles),
		gitStagedAdminPolicy(principles),
		gitCommitHeadPolicy(principles),
	}
}

func gitPolicy(
	policyID string,
	sourcePath string,
	principleIDs []string,
	message string,
	suggestion string,
) Policy {
	return Policy{
		ID:              policyID,
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: sourcePath},
		PrincipleIDs:    principleIDs,
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         message,
		Suggestion:      suggestion,
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "argv", Name: policyID}},
	}
}

func gitChangeDirPolicy(principles map[string]Principle) Policy {
	return gitPolicy(
		"git.change_dir_flag",
		"git.change_dir_flag",
		principleRefs(principles, "evidence-based-engineering-and-decision-quality"),
		"git -C hides the working directory context.",
		"Change to the intended directory explicitly, then run git there.",
	)
}

func gitStagedAdminPolicy(principles map[string]Principle) Policy {
	return Policy{
		ID:              "git.staged_admin_files",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.staged_admin_files"},
		PrincipleIDs:    principleRefs(principles, "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "record"},
		Message:         "Administrative staged files require explicit handling.",
		Suggestion:      "Confirm the policy/config change is intentional.",
		DefenseLayers: GitDefenseLayers(
			"ask",
			"wrapper",
			"block",
			"pre_commit",
			"git_state",
		),
		AppliesTo: AppliesTo{
			Commands: []string{"git commit"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{Kind: "git_state", Name: "git.staged_admin_files"}},
	}
}

func gitCommitHeadPolicy(principles map[string]Principle) Policy {
	return Policy{
		ID:       "git.commit_head_advanced",
		Category: "git",
		Source:   SourceRef{File: "config.yaml", Path: "git.commit_head_advanced"},
		PrincipleIDs: principleRefs(
			principles,
			"evidence-based-engineering-and-decision-quality",
		),
		DefaultSeverity: "annotate",
		SupportedModes:  []string{"annotate", "record", "block"},
		Message:         "Commit success must be verified by checking that HEAD advanced.",
		Suggestion:      "Compare pre-commit and post-commit HEAD before reporting success.",
		DefenseLayers:   GitDefenseLayers("", "wrapper", "record", "", "git_state"),
		AppliesTo:       AppliesTo{Commands: []string{"git commit"}, Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "git_state", Name: "git.commit_head_advanced"}},
	}
}

func gitStashPolicy(principles map[string]Principle) Policy {
	return gitPolicy(
		"git.stash_blocked",
		"principles.no-rationalized-shortcuts",
		principleRefs(principles, "no-rationalized-shortcuts"),
		"git stash hides working state and is forbidden when the stash ethos is active.",
		"Keep changes visible in the worktree or commit them normally.",
	)
}

func addShellPolicies(policies map[string]Policy, principles map[string]Principle) {
	for _, policy := range []Policy{
		shellPolicy(
			"shell.dangerous_command",
			principleRefs(principles, "security-by-design", "no-rationalized-shortcuts"),
			"Dangerous shell commands are forbidden.",
			"Use reviewed, explicit commands.",
		),
		shellPolicy(
			"shell.background_git",
			principleRefs(
				principles,
				"evidence-based-engineering-and-decision-quality",
				"one-path-for-critical-operations",
			),
			"git commit and git push must not run in the background or under timeout.",
			"Run git commit or git push in the foreground.",
		),
	} {
		policies[policy.ID] = policy
	}
}

func shellPolicy(
	policyID string,
	principleIDs []string,
	message string,
	suggestion string,
) Policy {
	return Policy{
		ID:              policyID,
		Category:        "shell",
		Source:          SourceRef{File: "config.yaml", Path: policyID},
		PrincipleIDs:    principleIDs,
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         message,
		Suggestion:      suggestion,
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo:       AppliesTo{Tools: []string{"Bash"}},
		Evaluators:      []Evaluator{{Kind: "shell", Name: policyID}},
	}
}

func addGeneratedConfigPolicy(
	policies map[string]Policy,
	principles map[string]Principle,
) {
	policies["generated_config.freshness"] = Policy{
		ID:       "generated_config.freshness",
		Category: "config",
		Source:   SourceRef{File: "config.yaml", Path: "generated_config.freshness"},
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "advise", "annotate", "record"},
		Message:         "Generated tool configuration must match source policy.",
		Suggestion:      "Run the configured tool-config sync/check command.",
		DefenseLayers:   GeneratedConfigDefenseLayers(),
		AppliesTo: AppliesTo{
			Paths: []string{
				"ruff.toml",
				"mypy.ini",
				"pyrightconfig.json",
				".yamllint.yml",
			},
		},
		Evaluators: []Evaluator{
			{Kind: "config", Name: "generated_config.freshness"},
		},
	}
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
	hooks := compileHookDispatch(policies)

	return Dispatch{
		Hooks:  hooks,
		Linter: compileLinterDispatch(policies),
		Git:    compileGitDispatch(policies),
	}
}

func compileHookDispatch(
	policies map[string]Policy,
) map[string]map[string][]HookDispatchEntry {
	hooks := map[string]map[string][]HookDispatchEntry{}
	addGitHookBypassDispatch(hooks, policies)
	addBlockingBashDispatch(hooks, policies)
	addPythonWriteDispatch(hooks, policies)
	addPytestGateDispatch(hooks, policies)
	addCommitHeadDispatch(hooks, policies)

	return hooks
}

func addGitHookBypassDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["git.hook_bypass"]; ok {
		ensureHookTool(hooks, "PreToolUse", "Bash")
		hooks["PreToolUse"]["Bash"] = append(
			hooks["PreToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID:        "git.hook_bypass",
				Mode:            "block",
				CommandPatterns: []string{"--no-verify", "SKIP=", "git commit -n"},
			},
		)
	}
}

func addBlockingBashDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	for _, policyID := range []string{
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
		if _, ok := policies[policyID]; ok {
			ensureHookTool(hooks, "PreToolUse", "Bash")
			hooks["PreToolUse"]["Bash"] = append(
				hooks["PreToolUse"]["Bash"],
				HookDispatchEntry{
					PolicyID: policyID,
					Mode:     "block",
				},
			)
		}
	}
}

func addPythonWriteDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	for _, policyID := range []string{
		"python.conditional_imports",
		"python.optional_returns",
		"python.catch_and_silence",
		"python.structured_logging",
		"python.direct_imports",
	} {
		if _, exists := policies[policyID]; exists {
			for _, tool := range []string{"Write", "Edit", "MultiEdit"} {
				ensureHookTool(hooks, "PreToolUse", tool)
				hooks["PreToolUse"][tool] = append(
					hooks["PreToolUse"][tool],
					HookDispatchEntry{
						PolicyID:     policyID,
						Mode:         "advise",
						PathPatterns: []string{"**/*.py"},
					},
				)
			}
		}
	}
}

func addPytestGateDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["pytest.gate"]; ok {
		ensureHookTool(hooks, "PostToolUse", "Bash")
		hooks["PostToolUse"]["Bash"] = append(
			hooks["PostToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID:        "pytest.gate",
				Mode:            "annotate",
				CommandPatterns: []string{"pytest", "make check", "make pre-commit"},
			},
		)
	}
}

func addCommitHeadDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["git.commit_head_advanced"]; ok {
		ensureHookTool(hooks, "PreToolUse", "Bash")
		hooks["PreToolUse"]["Bash"] = append(
			hooks["PreToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID:        "git.commit_head_advanced",
				Mode:            "record",
				CommandPatterns: []string{"git commit"},
			},
		)
		ensureHookTool(hooks, "PostToolUse", "Bash")
		hooks["PostToolUse"]["Bash"] = append(
			hooks["PostToolUse"]["Bash"],
			HookDispatchEntry{
				PolicyID:        "git.commit_head_advanced",
				Mode:            "block",
				CommandPatterns: []string{"git commit"},
			},
		)
	}
}

func compileLinterDispatch(policies map[string]Policy) map[string][]string {
	linter := map[string][]string{
		"files": existingPolicyIDs(
			policies,
			"python.conditional_imports",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
		),
		"staged": existingPolicyIDs(
			policies,
			"git.hook_bypass",
			"git.destructive_command",
			"git.merge_strategy_shortcut",
			"git.force_push_protected_branch",
			"git.checkout_protected_branch",
			"git.destructive_worktree",
			"git.change_dir_flag",
			"git.stash_blocked",
			"shell.dangerous_command",
			"shell.background_git",
			"git.staged_admin_files",
			"generated_config.freshness",
			"python.conditional_imports",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
		),
		"full": existingPolicyIDs(
			policies,
			"pytest.gate",
			"generated_config.freshness",
		),
	}
	if _, ok := policies["pytest.gate"]; ok {
		linter["smoke"] = []string{"pytest.gate"}
	}

	return linter
}

func compileGitDispatch(policies map[string]Policy) map[string]GitOperationDispatch {
	return map[string]GitOperationDispatch{
		"*": {
			Pre: existingPolicyIDs(policies, "git.change_dir_flag"),
		},
		"commit": {
			Pre: existingPolicyIDs(
				policies,
				"git.hook_bypass",
				"git.staged_admin_files",
			),
			Post: existingPolicyIDs(policies, "git.commit_head_advanced"),
		},
		"push": {
			Pre: existingPolicyIDs(
				policies,
				"git.hook_bypass",
				"git.force_push_protected_branch",
			),
		},
		"reset":    {Pre: existingPolicyIDs(policies, "git.destructive_command")},
		"clean":    {Pre: existingPolicyIDs(policies, "git.destructive_command")},
		"restore":  {Pre: existingPolicyIDs(policies, "git.destructive_command")},
		"switch":   {Pre: existingPolicyIDs(policies, "git.checkout_protected_branch")},
		"merge":    {Pre: existingPolicyIDs(policies, "git.merge_strategy_shortcut")},
		"worktree": {Pre: existingPolicyIDs(policies, "git.destructive_worktree")},
		"stash":    {Pre: existingPolicyIDs(policies, "git.stash_blocked")},
		"checkout": {
			Pre: existingPolicyIDs(
				policies,
				"git.destructive_command",
				"git.checkout_protected_branch",
			),
		},
	}
}

func ensureHookTool(
	hooks map[string]map[string][]HookDispatchEntry,
	event string,
	tool string,
) {
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
	value, exists := valueAt(values, path...)
	if !exists {
		return false
	}

	boolValue, isBool := value.(bool)

	return isBool && boolValue
}

func valueAt(values map[string]any, path ...string) (any, bool) {
	current := any(values)
	for _, part := range path {
		currentMap, isMap := current.(map[string]any)
		if !isMap {
			return nil, false
		}

		var exists bool

		current, exists = currentMap[part]
		if !exists {
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
	parts := make([]string, 0, defaultBundleBaseParts+len(hashes))
	parts = append(parts, primary, config)

	for path, hash := range hashes {
		parts = append(parts, path+"="+hash)
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))

	return "policy-" + hex.EncodeToString(sum[:8])
}
