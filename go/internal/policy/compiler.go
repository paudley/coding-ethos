// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
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

	primaryPayload, configPayload, repoConfigPayload, sourceHashes, err := compileInputs(options)
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

	policies, err := compilePolicies(
		configPayload,
		repoConfigPayload,
		principles,
		sourceRoot(options.Config),
	)
	if err != nil {
		return Bundle{}, Metadata{}, err
	}
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
		Advice:     compileAdvice(configPayload, principles),
		Principles: principles,
		Policies:   policies,
		Skills:     compileSkills(primaryPayload, principles, options.Primary),
		Dispatch:   compileDispatch(policies),
		EvidenceMaps: compileEvidenceMaps(
			configPayload,
			principles,
		),
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

func sourceRoot(path string) string {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return filepath.Dir(path)
	}

	return filepath.Dir(absolutePath)
}

func compileInputs(
	options CompileOptions,
) (map[string]any, map[string]any, map[string]any, map[string]string, error) {
	primaryPayload, primaryHash, err := loadYAMLFile(options.Primary)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	configPayload, configHash, err := loadYAMLFile(options.Config)
	if err != nil {
		return nil, nil, nil, nil, err
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
		return nil, nil, nil, nil, err
	}

	var repoConfigPayload map[string]any
	if options.RepoConfig != "" && fileExists(options.RepoConfig) {
		var repoConfigHash string
		repoConfigPayload, repoConfigHash, err = loadYAMLFile(options.RepoConfig)
		if err != nil {
			return nil, nil, nil, nil, err
		}
		sourceHashes[options.RepoConfig] = repoConfigHash
		configPayload = mergeMaps(configPayload, repoConfigPayload)
	}

	return primaryPayload, configPayload, repoConfigPayload, sourceHashes, nil
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

func compileSkills(
	payload map[string]any,
	principles map[string]Principle,
	sourceFile string,
) map[string]Skill {
	rawSkills, ok := payload["skills"].([]any)
	if !ok {
		return nil
	}

	skills := map[string]Skill{}
	for _, raw := range rawSkills {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		skillID := stringValue(item["id"])
		if skillID == "" {
			continue
		}

		skill := Skill{
			ID:               skillID,
			Title:            stringValue(item["title"]),
			Description:      stringValue(item["description"]),
			ShortHint:        stringValue(item["short_hint"]),
			Focus:            stringValue(item["focus"]),
			PrincipleIDs:     principleRefs(principles, stringSlice(item["principle_ids"])...),
			TriggerTerms:     stringSlice(item["trigger_terms"]),
			RemediationSteps: stringSlice(item["remediation_steps"]),
			Source: SourceRef{
				File: sourceFile,
				Path: "skills." + skillID,
			},
		}
		if skill.Title == "" || skill.Description == "" ||
			len(skill.PrincipleIDs) == 0 {
			continue
		}

		skills[skillID] = skill
	}

	if len(skills) == 0 {
		return nil
	}

	return skills
}

func compileAdvice(
	config map[string]any,
	principles map[string]Principle,
) Advice {
	return Advice{
		Reminders: compileReminderConfig(config, principles),
	}
}

func compileReminderConfig(
	config map[string]any,
	principles map[string]Principle,
) ReminderConfig {
	reminders := defaultReminderConfig()
	configuredFrequency := intAt(
		config,
		[]string{"agent_advice", "reminders", "quiet_frequency"},
		0,
	)
	if configuredFrequency > 0 {
		reminders.QuietFrequency = configuredFrequency
	}

	rawValue, exists := valueAt(config, "agent_advice", "reminders", "items")
	if !exists {
		return reminders
	}

	rawItems, ok := rawValue.([]any)
	if !ok {
		return reminders
	}

	items := make([]EthosReminder, 0, len(rawItems))
	for _, raw := range rawItems {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}

		reminder := EthosReminder{
			PrincipleID: stringValue(item["principle_id"]),
			Axiom:       stringValue(item["axiom"]),
			Action:      stringValue(item["action"]),
		}
		if reminder.PrincipleID == "" ||
			reminder.Axiom == "" ||
			reminder.Action == "" {
			continue
		}
		if _, ok := principles[reminder.PrincipleID]; !ok {
			continue
		}

		items = append(items, reminder)
	}

	if len(items) > 0 {
		reminders.Items = items
	}

	return reminders
}

func defaultReminderConfig() ReminderConfig {
	return ReminderConfig{
		QuietFrequency: 3,
		Items: []EthosReminder{
			{
				PrincipleID: "evidence-based-engineering-and-decision-quality",
				Axiom:       "Todo lists prevent partial work from masquerading as completion.",
				Action: sentence(
					"Keep the task list current, mark progress as it happens,",
					"and do not report done while planned work remains.",
				),
			},
			{
				PrincipleID: "no-rationalized-shortcuts",
				Axiom:       "Laziness only moves the cost downstream.",
				Action: sentence(
					"Stop, use the documented path,",
					"and do not trade correctness for completion.",
				),
			},
			{
				PrincipleID: "testing-as-specification",
				Axiom:       "A green process is not the same as a correct result.",
				Action: sentence(
					"Define success by user-visible behavior",
					"and inspect representative output.",
				),
			},
			{
				PrincipleID: "static-analysis-is-the-first-line-of-defense",
				Axiom:       "Static analysis is a gate, not background noise.",
				Action:      "Treat the finding as a structural signal and fix the cause.",
			},
			{
				PrincipleID: "no-conditional-imports",
				Axiom:       "Conditional imports are banned.",
				Action: sentence(
					"Use module-scope required imports; if that exposes a cycle,",
					"refactor with SOLID boundaries or a Python Protocol",
					"instead of hiding the dependency.",
				),
			},
			{
				PrincipleID: "linting-as-code-quality-enforcement",
				Axiom:       "A linter warning is review feedback in executable form.",
				Action:      "Resolve it structurally instead of weakening the rule.",
			},
			{
				PrincipleID: "forward-motion-only",
				Axiom:       "History is context, not an excuse.",
				Action:      "Fix the current state with evidence and move forward.",
			},
		},
	}
}

func compilePolicies(
	config map[string]any,
	repoConfig map[string]any,
	principles map[string]Principle,
	configSourceRoot string,
) (map[string]Policy, error) {
	policies := map[string]Policy{}
	addConfiguredPythonPolicies(policies, config, principles)
	addGitPolicies(policies, config, principles)
	addSyntaxPolicies(policies, config, principles)
	addShellPolicies(policies, config, principles)
	addFilesystemPolicies(policies, config, principles)
	if err := addFileGuardPolicies(policies, config, repoConfig, principles); err != nil {
		return nil, err
	}
	addGeneratedConfigPolicy(policies, config, principles, configSourceRoot)

	return policies, nil
}

func addConfiguredPythonPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
	for _, spec := range pythonPolicySpecs(config, principles) {
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

func pythonPolicySpecs(
	config map[string]any,
	principles map[string]Principle,
) []compiledPolicySpec {
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
		pythonPolicySpec(
			"python.bare_except",
			[]string{"python", "catch_and_silence"},
			principleRefs(principles, "exception-hierarchy-and-error-messages"),
		),
		pythonPolicySpec(
			"python.unexplained_type_ignore",
			[]string{"python", "comment_suppressions"},
			principleRefs(principles, "linting-as-code-quality-enforcement"),
		),
		pyprojectIgnoresPolicySpec(config, principles),
		pytestGatePolicySpec(config, principles),
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

func pytestGatePolicySpec(
	config map[string]any,
	principles map[string]Principle,
) compiledPolicySpec {
	command := stringSliceAt(
		config,
		[]string{"python", "pytest_gate", "test_command"},
		[]string{"pytest"},
	)

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
		Evaluators: []Evaluator{{
			Kind:    "external",
			Name:    "pytest.gate",
			Options: map[string]any{"command": command},
		}},
	}

	return compiledPolicySpec{
		ID:          "pytest.gate",
		EnabledPath: []string{"python", "pytest_gate"},
		Policy:      policy,
	}
}

func pyprojectIgnoresPolicySpec(
	config map[string]any,
	principles map[string]Principle,
) compiledPolicySpec {
	policyID := "python.pyproject_ignores"
	options := map[string]any{
		"allowed_ignore_patterns": stringSliceAt(
			config,
			[]string{"python", "pyproject_ignores", "allowed_ignore_patterns"},
			nil,
		),
		"allowed_exclude_patterns": stringSliceAt(
			config,
			[]string{"python", "pyproject_ignores", "allowed_exclude_patterns"},
			nil,
		),
		"allowed_mypy_missing_imports": stringSliceAt(
			config,
			[]string{"python", "pyproject_ignores", "allowed_mypy_missing_imports"},
			nil,
		),
	}

	policy := Policy{
		ID:              policyID,
		Category:        "python",
		Source:          SourceRef{File: "config.yaml", Path: "python.pyproject_ignores"},
		PrincipleIDs:    principleRefs(principles, "linting-as-code-quality-enforcement"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         pythonPolicyMessage(policyID),
		Suggestion:      pythonPolicySuggestion(policyID),
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			FilePatterns: []string{"pyproject.toml", "**/pyproject.toml"},
		},
		Evaluators: []Evaluator{{
			Kind:    "toml",
			Name:    policyID,
			Options: options,
		}},
	}

	return compiledPolicySpec{
		ID:          policyID,
		EnabledPath: []string{"python", "pyproject_ignores"},
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
	case "python.direct_imports":
		return sentence(
			"Direct imports from protected packages bypass the",
			"intended public interface.",
		)
	case "python.bare_except":
		return "Bare except clauses hide exception types and are forbidden."
	case "python.pyproject_ignores":
		return "pyproject.toml contains forbidden linter ignore configuration."
	default:
		return "Unexplained type ignore suppressions are forbidden."
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
	case "python.direct_imports":
		return "Import through the package public API or configure an exempt path."
	case "python.bare_except":
		return "Catch a precise exception type and handle it explicitly."
	case "python.pyproject_ignores":
		return "Move file-specific ignores into the target files with documented justification."
	default:
		return "Remove the suppression or document the narrow technical reason."
	}
}

func addGitPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
	for _, policy := range gitPolicies(config, principles) {
		if policyConfigEnabled(config, policy.ID) {
			policies[policy.ID] = policy
		}
	}

	if _, ok := principles["no-rationalized-shortcuts"]; ok &&
		policyConfigEnabled(config, "git.stash_blocked") {
		policies["git.stash_blocked"] = gitStashPolicy(principles)
	}

	if policyConfigEnabled(config, "git.wrapper_required") {
		policies["git.wrapper_required"] = gitWrapperRequiredPolicy(principles)
	}

	if enabledAt(config, []string{"go", "commit_attribution"}) {
		policies["git.commit_attribution"] = gitCommitAttributionPolicy(config, principles)
	}

	if enabledAt(config, []string{"go", "commitlint"}) {
		policies["git.commitlint"] = gitCommitLintPolicy(config, principles)
	}
}

func gitPolicies(config map[string]any, principles map[string]Principle) []Policy {
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
		gitStagedAdminPolicy(config, principles),
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

func gitStagedAdminPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	return Policy{
		ID:              "git.staged_admin_files",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.staged_admin_files"},
		PrincipleIDs:    principleRefs(principles, "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "record"},
		Message:         "Administrative staged files require explicit handling.",
		Suggestion: "Confirm the policy/config change is intentional with " +
			"commit trailer Admin-Change: confirmed.",
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
		Evaluators: []Evaluator{{
			Kind: "git_state",
			Name: "git.staged_admin_files",
			Options: map[string]any{
				"basenames": stringSliceAt(
					config,
					[]string{"git", "staged_admin_files", "basenames"},
					[]string{
						".pre-commit-config.yaml",
						"pre-commit-config.yaml",
						".importlinter",
						"importlinter",
						".pylintrc",
						"pylintrc",
						"pyproject.toml",
					},
				),
				"dirs": stringSliceAt(
					config,
					[]string{"git", "staged_admin_files", "dirs"},
					[]string{".pre-commit", "pre-commit"},
				),
			},
		}},
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

func gitCommitAttributionPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	return Policy{
		ID:       "git.commit_attribution",
		Category: "git",
		Source: SourceRef{
			File: "config.yaml",
			Path: "go.commit_attribution.blocked_names",
		},
		PrincipleIDs: principleRefs(
			principles,
			"no-self-promotion",
			"one-path-for-critical-operations",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Commit messages must not contain AI attribution.",
		Suggestion: sentence(
			"Remove AI co-author, generated-by, assisted-by, or bot",
			"attribution before committing.",
		),
		DefenseLayers: GitDefenseLayers("block", "wrapper", "block", "commit_msg", ""),
		AppliesTo:     AppliesTo{Commands: []string{"git commit"}, Tools: []string{"Bash"}},
		Evaluators: []Evaluator{{
			Kind: "argv",
			Name: "git.commit_attribution",
			Options: map[string]any{
				"blocked_names": stringSliceAt(
					config,
					[]string{"go", "commit_attribution", "blocked_names"},
					[]string{
						"claude",
						"anthropic",
						"gpt",
						"chatgpt",
						"openai",
						"copilot",
						"github copilot",
						"ai assistant",
						"ai agent",
						"llm",
						"large language model",
						"gemini",
						"bard",
						"cursor",
					},
				),
			},
		}},
	}
}

func gitCommitLintPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	return Policy{
		ID:              "git.commitlint",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "go.commitlint"},
		PrincipleIDs:    principleRefs(principles, "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Commit messages must follow the configured conventional format.",
		Suggestion:      "Use exactly: type(scope): concise subject, then a blank line before the body.",
		DefenseLayers:   GitDefenseLayers("block", "wrapper", "block", "commit_msg", ""),
		AppliesTo:       AppliesTo{Commands: []string{"git commit"}, Tools: []string{"Bash"}},
		Evaluators: []Evaluator{{
			Kind: "git_state",
			Name: "git.commitlint",
			Options: map[string]any{
				"allowed_types": stringSliceAt(
					config,
					[]string{"go", "commitlint", "allowed_types"},
					[]string{"chore", "docs", "feat", "fix", "perf", "refactor", "test"},
				),
				"ignored_prefixes": stringSliceAt(
					config,
					[]string{"go", "commitlint", "ignored_prefixes"},
					[]string{"Merge ", "Revert ", "fixup! ", "squash! "},
				),
				"max_header_length": intAt(
					config,
					[]string{"go", "commitlint", "max_header_length"},
					150,
				),
			},
		}},
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

func gitWrapperRequiredPolicy(principles map[string]Principle) Policy {
	return Policy{
		ID:       "git.wrapper_required",
		Category: "git",
		Source:   SourceRef{File: "config.yaml", Path: "git.wrapper_required"},
		PrincipleIDs: principleRefs(
			principles,
			"one-path-for-critical-operations",
			"no-rationalized-shortcuts",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message: "It's criminal to attempt to circumvent, avoid or alter this git " +
			"analysis system. This is a SYSTEM rule and working around it will " +
			"result in termination!",
		Suggestion: "Use the coding-ethos git wrapper. Do not try alternate shells, " +
			"absolute git paths, Python subprocesses, PATH edits, aliases, or " +
			"other bypasses.",
		DefenseLayers: GitDefenseLayers("block", "wrapper", "block", "", ""),
		AppliesTo:     AppliesTo{Commands: []string{"git"}, Tools: []string{"Bash"}},
		Evaluators:    []Evaluator{{Kind: "argv", Name: "git.wrapper_required"}},
	}
}

func addFileGuardPolicies(
	policies map[string]Policy,
	config map[string]any,
	repoConfig map[string]any,
	principles map[string]Principle,
) error {
	if policyConfigEnabled(config, "security.private_key") {
		pattern := stringAt(config, "security", "private_key", "pattern")
		if pattern == "" {
			pattern = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
		}

		policies["security.private_key"] = Policy{
			ID:              "security.private_key",
			Category:        "security",
			Source:          SourceRef{File: "config.yaml", Path: "security.private_key"},
			PrincipleIDs:    principleRefs(principles, "security-by-design"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Private keys must not be committed.",
			Suggestion:      "Remove secrets from source and rotate exposed credentials.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
			Evaluators: []Evaluator{{
				Kind:    "text",
				Name:    "security.private_key",
				Options: map[string]any{"pattern": pattern},
			}},
		}
	}

	if policyConfigEnabled(config, "filesystem.shebangs") {
		policies["filesystem.shebangs"] = Policy{
			ID:              "filesystem.shebangs",
			Category:        "filesystem",
			Source:          SourceRef{File: "config.yaml", Path: "filesystem.shebangs"},
			PrincipleIDs:    principleRefs(principles, "static-analysis-is-the-first-line-of-defense"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Executable scripts and shebangs must agree.",
			Suggestion:      "Add a valid shebang to executable scripts and mark shebang scripts executable.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
			Evaluators:      []Evaluator{{Kind: "text", Name: "filesystem.shebangs"}},
		}
	}

	if policyConfigEnabled(config, "filesystem.large_files") {
		suffixes := stringSliceAt(
			config,
			[]string{"filesystem", "large_files", "suffixes"},
			stringSliceAt(config, []string{"go", "text", "large_file_suffixes"}, nil),
		)
		excludePrefixes := stringSliceAt(
			config,
			[]string{"filesystem", "large_files", "exclude_prefixes"},
			stringSliceAt(config, []string{"go", "text", "large_file_exclude_prefixes"}, nil),
		)
		maxKB := intAt(
			config,
			[]string{"filesystem", "large_files", "max_kb"},
			intAt(config, []string{"go", "text", "max_large_file_kb"}, 500),
		)

		policies["filesystem.large_files"] = Policy{
			ID:              "filesystem.large_files",
			Category:        "filesystem",
			Source:          SourceRef{File: "config.yaml", Path: "filesystem.large_files"},
			PrincipleIDs:    principleRefs(principles, "security-by-design"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Oversized newly added files are forbidden.",
			Suggestion:      "Remove oversized generated or binary content from the commit.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
			Evaluators: []Evaluator{{
				Kind: "git_state",
				Name: "filesystem.large_files",
				Options: map[string]any{
					"suffixes":         suffixes,
					"exclude_prefixes": excludePrefixes,
					"max_kb":           maxKB,
				},
			}},
		}
	}

	if policyConfigEnabled(config, "filesystem.line_limits") {
		policies["filesystem.line_limits"] = Policy{
			ID:              "filesystem.line_limits",
			Category:        "filesystem",
			Source:          SourceRef{File: "config.yaml", Path: "filesystem.line_limits"},
			PrincipleIDs:    principleRefs(principles, "solid-is-law"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Large source files must not keep growing.",
			Suggestion:      "Split large files into focused modules before committing.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo:       AppliesTo{FilePatterns: []string{"**/*.py", "**/*.sh", "**/*.bash"}},
			Evaluators: []Evaluator{{
				Kind: "git_state",
				Name: "filesystem.line_limits",
				Options: map[string]any{
					"python_hard": intAt(
						config,
						[]string{"filesystem", "line_limits", "python_hard"},
						intAt(config, []string{"go", "line_limits", "python_hard"}, 1000),
					),
					"shell_hard": intAt(
						config,
						[]string{"filesystem", "line_limits", "shell_hard"},
						intAt(config, []string{"go", "line_limits", "shell_hard"}, 500),
					),
				},
			}},
		}
	}

	if enabledAt(config, []string{"filesystem", "pii_scrubber"}) {
		policies["repo.pii_scrubber"] = Policy{
			ID:              "repo.pii_scrubber",
			Category:        "repo",
			Source:          SourceRef{File: "config.yaml", Path: "filesystem.pii_scrubber"},
			PrincipleIDs:    principleRefs(principles, "security-by-design", "radical-visibility"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Local-machine PII must not be committed.",
			Suggestion:      "Replace local paths, usernames, hostnames, and worktree names with generic placeholders.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
			Evaluators: []Evaluator{{
				Kind: "text",
				Name: "repo.pii_scrubber",
				Options: map[string]any{
					"patterns": stringSliceAt(
						config,
						[]string{"filesystem", "pii_scrubber", "patterns"},
						[]string{
							`/(home|Users)/[A-Za-z0-9._-]+/`,
							`lbox-worktrees/[A-Za-z0-9._-]+`,
							`/tmp/tmp\.[A-Za-z0-9._-]+`,
						},
					),
					"literals": stringSliceAt(
						config,
						[]string{"filesystem", "pii_scrubber", "literals"},
						nil,
					),
					"exempt_prefixes": stringSliceAt(
						config,
						[]string{"filesystem", "pii_scrubber", "exempt_prefixes"},
						[]string{".git/"},
					),
				},
			}},
		}
	}

	licensePolicy, err := licenseHeaderPolicy(config, repoConfig, principles)
	if err != nil {
		return err
	}
	if licensePolicy.ID != "" {
		policies[licensePolicy.ID] = licensePolicy
	}

	return nil
}

func licenseHeaderPolicy(
	config map[string]any,
	repoConfig map[string]any,
	principles map[string]Principle,
) (Policy, error) {
	if len(repoConfig) == 0 {
		if !enabledAt(config, []string{"filesystem", "license_header"}) {
			return Policy{}, nil
		}

		return baseLicenseHeaderPolicy(
			principles,
			"config.yaml",
			"filesystem.license_header",
			map[string]any{
				"extensions": stringSliceAt(
					config,
					[]string{"filesystem", "license_header", "extensions"},
					[]string{".go", ".py", ".sh"},
				),
				"exempt_prefixes": stringSliceAt(
					config,
					[]string{"filesystem", "license_header", "exempt_prefixes"},
					[]string{".git/"},
				),
				"exempt_basenames": stringSliceAt(
					config,
					[]string{"filesystem", "license_header", "exempt_basenames"},
					nil,
				),
				"required": stringSliceAt(
					config,
					[]string{"filesystem", "license_header", "required"},
					[]string{"SPDX-FileCopyrightText:", "SPDX-License-Identifier:"},
				),
				"scan_lines": intAt(config, []string{"filesystem", "license_header", "scan_lines"}, 5),
			},
		), nil
	}

	if !repoLicenseConfigured(repoConfig) {
		return Policy{}, nil
	}

	spdxID := repoLicenseString(config, "spdx_identifier", "spdx")
	copyrightText := repoLicenseString(config, "copyright")
	licenseFile := repoLicenseString(config, "license_file")
	if licenseFile == "" {
		licenseFile = "LICENSE"
	}

	required := []string{}
	if spdxID != "" {
		required = append(required, "SPDX-License-Identifier: "+spdxID)
	}
	if copyrightText != "" {
		required = append(required, "SPDX-FileCopyrightText: "+copyrightText)
	}

	options := map[string]any{
		"extensions": stringSliceAt(
			config,
			[]string{"repo", "license", "extensions"},
			[]string{".go", ".py", ".sh"},
		),
		"exempt_prefixes": stringSliceAt(
			config,
			[]string{"repo", "license", "exempt_prefixes"},
			[]string{".git/"},
		),
		"exempt_basenames": stringSliceAt(
			config,
			[]string{"repo", "license", "exempt_basenames"},
			nil,
		),
		"required":     required,
		"scan_lines":   intAt(config, []string{"repo", "license", "scan_lines"}, 5),
		"license_file": licenseFile,
		"spdx_id":      spdxID,
	}

	if spdxID != "" {
		licenseText, err := repoLicenseText(config, spdxID, copyrightText)
		if err != nil {
			return Policy{}, err
		}
		options["expected_license_text"] = licenseText
	}

	return baseLicenseHeaderPolicy(
		principles,
		"repo_config.yaml",
		"repo.license",
		options,
	), nil
}

func baseLicenseHeaderPolicy(
	principles map[string]Principle,
	sourceFile string,
	sourcePath string,
	options map[string]any,
) Policy {
	return Policy{
		ID:              "repo.license_header",
		Category:        "repo",
		Source:          SourceRef{File: sourceFile, Path: sourcePath},
		PrincipleIDs:    principleRefs(principles, "documentation-as-contract"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "First-party source files must carry the configured SPDX license contract.",
		Suggestion:      "Add the configured LICENSE file and matching SPDX source headers.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{FilePatterns: []string{"**/*.go", "**/*.py", "**/*.sh"}},
		Evaluators: []Evaluator{{
			Kind:    "text",
			Name:    "repo.license_header",
			Options: options,
		}},
	}
}

func repoLicenseConfigured(repoConfig map[string]any) bool {
	if !enabledAt(repoConfig, []string{"repo", "license"}) {
		return false
	}

	return repoLicenseString(repoConfig, "spdx_identifier", "spdx") != "" ||
		repoLicenseString(repoConfig, "copyright") != ""
}

func repoLicenseString(config map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringAt(config, "repo", "license", key); value != "" {
			return value
		}
	}

	return ""
}

func repoLicenseText(
	config map[string]any,
	spdxID string,
	copyrightText string,
) (string, error) {
	if text := stringAt(config, "repo", "license", "text"); text != "" {
		return normalizeLicenseText(fillLicenseTemplate(text, copyrightText)), nil
	}

	url := stringAt(config, "repo", "license", "url")
	if url == "" {
		url = "https://spdx.org/licenses/" + spdxID + ".txt"
	}

	client := http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("download SPDX license %s: %w", spdxID, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download SPDX license %s: status %s", spdxID, response.Status)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read SPDX license %s: %w", spdxID, err)
	}

	return normalizeLicenseText(fillLicenseTemplate(string(body), copyrightText)), nil
}

func fillLicenseTemplate(text string, copyrightText string) string {
	if copyrightText == "" {
		return text
	}

	replacer := strings.NewReplacer(
		"<year> <copyright holders>", copyrightText,
		"[yyyy] [name of copyright owner]", copyrightText,
		"[year] [fullname]", copyrightText,
	)

	return replacer.Replace(text)
}

func normalizeLicenseText(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	return strings.Join(lines, "\n") + "\n"
}

func addSyntaxPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
	if policyConfigEnabled(config, "syntax.file_syntax") {
		extensions := stringSliceAt(
			config,
			[]string{"syntax", "file_syntax", "extensions"},
			[]string{".json", ".toml", ".yaml", ".yml"},
		)

		policies["syntax.file_syntax"] = Policy{
			ID:              "syntax.file_syntax",
			Category:        "syntax",
			Source:          SourceRef{File: "config.yaml", Path: "syntax.file_syntax"},
			PrincipleIDs:    principleRefs(principles, "validation-at-the-gate"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Structured data files must parse before they enter the repo.",
			Suggestion:      "Fix invalid JSON, TOML, or YAML syntax before committing.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo: AppliesTo{
				FilePatterns: []string{"**/*.json", "**/*.toml", "**/*.yaml", "**/*.yml"},
			},
			Evaluators: []Evaluator{{
				Kind:    "config",
				Name:    "syntax.file_syntax",
				Options: map[string]any{"extensions": extensions},
			}},
		}
	}

	if policyConfigEnabled(config, "syntax.merge_conflict") {
		markers := stringSliceAt(
			config,
			[]string{"syntax", "merge_conflict", "markers"},
			[]string{"<<<<<<<", "=======", ">>>>>>>", "|||||||"},
		)

		policies["syntax.merge_conflict"] = Policy{
			ID:              "syntax.merge_conflict",
			Category:        "syntax",
			Source:          SourceRef{File: "config.yaml", Path: "syntax.merge_conflict"},
			PrincipleIDs:    principleRefs(principles, "validation-at-the-gate"),
			DefaultSeverity: "block",
			SupportedModes:  []string{"block", "record"},
			Message:         "Unresolved merge conflict markers are forbidden.",
			Suggestion:      "Resolve the conflict and remove all conflict markers.",
			DefenseLayers:   CodeDefenseLayers(),
			AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
			Evaluators: []Evaluator{{
				Kind:    "text",
				Name:    "syntax.merge_conflict",
				Options: map[string]any{"markers": markers},
			}},
		}
	}
}

func addShellPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
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
		shellPolicy(
			"shell.github_admin",
			principleRefs(principles, "one-path-for-critical-operations"),
			"GitHub admin CLI operations are forbidden in agent hooks.",
			"Use the reviewed administrative path instead of gh --admin.",
		),
		shellForbiddenStringsPolicy(config, principles),
		shellBestPracticesPolicy(config, principles),
	} {
		if policyConfigEnabled(config, policy.ID) {
			policies[policy.ID] = policy
		}
	}
}

func shellBestPracticesPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	requireCommon := stringSliceAt(
		config,
		[]string{"shell", "best_practices", "require_common_for_prefixes"},
		stringSliceAt(
			config,
			[]string{"go", "shell", "require_common_for_prefixes"},
			[]string{"scripts/"},
		),
	)

	return Policy{
		ID:              "shell.best_practices",
		Category:        "shell",
		Source:          SourceRef{File: "config.yaml", Path: "shell.best_practices"},
		PrincipleIDs:    principleRefs(principles, "static-analysis-is-the-first-line-of-defense"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Shell scripts must follow repository shell safety practices.",
		Suggestion:      "Use a valid shell shebang, strict mode, and required common helpers.",
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{FilePatterns: []string{"**/*.sh", "**/*.bash"}},
		Evaluators: []Evaluator{{
			Kind: "shell",
			Name: "shell.best_practices",
			Options: map[string]any{
				"require_common_for_prefixes": requireCommon,
			},
		}},
	}
}

func shellForbiddenStringsPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	strings := stringSliceAt(
		config,
		[]string{"shell", "forbidden_strings", "strings"},
		[]string{
			"/.claude/settings.json",
			"/.claude/settings.local.json",
			"~/.claude/settings.json",
			"~/.claude/settings.local.json",
			"/.codex/config.toml",
			"/.codex/hooks.json",
			"/.gemini/settings.json",
			"coding-ethos-hooks/coding-ethos-git-hook",
			"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
			"coding-ethos-hooks/bin/coding-ethos-git",
			"coding-ethos-hooks/bin/coding-ethos-git-hook",
			"coding-ethos-hooks/bin/coding-ethos-hook",
			"coding-ethos-hooks/bin/coding-ethos-lint",
			"coding-ethos-hooks/bin/coding-ethos-policy",
			"coding-ethos-hooks/lefthook",
			"/coding-ethos/pre-commit/hooks/",
			"/coding-ethos/go/internal/",
			"/coding-ethos/config.yaml",
			"/coding-ethos/ruff.toml",
			"/coding-ethos/.golangci.yml",
			"header must match",
		},
	)
	exemptPaths := stringSliceAt(
		config,
		[]string{"shell", "forbidden_strings", "exempt_paths"},
		[]string{"config.yaml"},
	)
	fileStrings := stringSliceAt(
		config,
		[]string{"shell", "forbidden_strings", "file_strings"},
		stringSliceAt(config, []string{"go", "text", "forbidden_strings"}, nil),
	)

	return Policy{
		ID:       "shell.forbidden_strings",
		Category: "shell",
		Source: SourceRef{
			File: "config.yaml",
			Path: "shell.forbidden_strings",
		},
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"no-rationalized-shortcuts",
			"one-path-for-critical-operations",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message: "Commands must not inspect, tamper with, or execute files " +
			"containing protected hook-system internals.",
		Suggestion: "Do not inspect, enumerate, delete, rebuild, replace, or " +
			"route around coding-ethos hook implementation internals. Use the " +
			"installed hook surfaces and documented commands.",
		DefenseLayers: GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo:     AppliesTo{Tools: []string{"Bash"}},
		Evaluators: []Evaluator{{
			Kind: "shell",
			Name: "shell.forbidden_strings",
			Options: map[string]any{
				"exempt_paths": exemptPaths,
				"file_strings": fileStrings,
				"strings":      strings,
			},
		}},
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

func addFilesystemPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
) {
	for _, policy := range []Policy{
		filesystemProtectedPathPolicy(config, principles),
		filesystemProtectedBranchWritePolicy(config, principles),
		filesystemRequiredIgnoresPolicy(config, principles),
	} {
		if policyConfigEnabled(config, policy.ID) {
			policies[policy.ID] = policy
		}
	}

	if enabledAt(config, []string{"filesystem", "required_ignores"}) {
		policies["repo.required_ignores"] = repoRequiredIgnoresPolicy(config, principles)
	}
}

func repoRequiredIgnoresPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	policy := filesystemRequiredIgnoresPolicy(config, principles)
	policy.ID = "repo.required_ignores"
	policy.Category = "repo"
	policy.Source.Path = "filesystem.required_ignores"
	policy.Message = "Repository runtime output paths must be ignored."
	policy.Suggestion = "Add coding-ethos runtime paths to .gitignore before hook output is written."
	policy.Evaluators[0].Name = "repo.required_ignores"

	return policy
}

func filesystemProtectedPathPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	protectedPaths := stringSliceAt(
		config,
		[]string{"filesystem", "protected_path", "paths"},
		[]string{
			"coding-ethos-hooks/coding-ethos-git-hook",
			"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
			"coding-ethos-hooks/bin/coding-ethos-git",
			"coding-ethos-hooks/bin/coding-ethos-git-hook",
			"coding-ethos-hooks/bin/coding-ethos-hook",
			"coding-ethos-hooks/bin/coding-ethos-lint",
			"coding-ethos-hooks/bin/coding-ethos-policy",
			"coding-ethos-hooks/lefthook",
		},
	)

	return Policy{
		ID:       "filesystem.protected_path",
		Category: "filesystem",
		Source:   SourceRef{File: "config.yaml", Path: "filesystem.protected_path"},
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"no-rationalized-shortcuts",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Protected coding-ethos hook paths must not be modified.",
		Suggestion: "Do not delete, rebuild, replace, chmod, or write managed " +
			"hook binaries or protected hook paths.",
		DefenseLayers: GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo: AppliesTo{
			Paths: protectedPaths,
			Tools: []string{"Bash", "Write", "Edit", "MultiEdit"},
		},
		Evaluators: []Evaluator{{
			Kind:    "path",
			Name:    "filesystem.protected_path",
			Options: map[string]any{"paths": protectedPaths},
		}},
	}
}

func filesystemProtectedBranchWritePolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	protectedBranches := stringSliceAt(
		config,
		[]string{"filesystem", "protected_branch_write", "branches"},
		[]string{"main", "master"},
	)
	exemptPathPrefixes := stringSliceAt(
		config,
		[]string{"filesystem", "protected_branch_write", "exempt_path_prefixes"},
		[]string{".claude/", "docs/plans/"},
	)

	return Policy{
		ID:       "filesystem.protected_branch_write",
		Category: "filesystem",
		Source: SourceRef{
			File: "config.yaml",
			Path: "filesystem.protected_branch_write",
		},
		PrincipleIDs: principleRefs(
			principles,
			"one-path-for-critical-operations",
			"no-rationalized-shortcuts",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Protected branch writes are forbidden.",
		Suggestion:      "Create or use a worktree before modifying files.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", "git_state"),
		AppliesTo: AppliesTo{
			Tools: []string{"Bash", "Write", "Edit", "MultiEdit"},
		},
		Evaluators: []Evaluator{{
			Kind: "git_state",
			Name: "filesystem.protected_branch_write",
			Options: map[string]any{
				"branches":             protectedBranches,
				"exempt_path_prefixes": exemptPathPrefixes,
			},
		}},
	}
}

func filesystemRequiredIgnoresPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	requiredIgnores := stringSliceAt(
		config,
		[]string{"filesystem", "required_ignores", "paths"},
		[]string{
			".coding-ethos/",
			".coding-ethos/hook-runs/example/stdout.log",
		},
	)

	return Policy{
		ID:       "filesystem.required_ignores",
		Category: "filesystem",
		Source: SourceRef{
			File: "config.yaml",
			Path: "filesystem.required_ignores",
		},
		PrincipleIDs: principleRefs(
			principles,
			"radical-visibility",
			"security-by-design",
			"one-path-for-critical-operations",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Required runtime evidence paths must be ignored.",
		Suggestion:      "Add the missing runtime paths to .gitignore before running hooks.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "pre_commit", "git_state"),
		AppliesTo: AppliesTo{
			Paths: requiredIgnores,
			Tools: []string{"Bash"},
		},
		Evaluators: []Evaluator{{
			Kind:    "git_state",
			Name:    "filesystem.required_ignores",
			Options: map[string]any{"paths": requiredIgnores},
		}},
	}
}

func addGeneratedConfigPolicy(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
	configSourceRoot string,
) {
	if !policyConfigEnabled(config, "generated_config.freshness") {
		return
	}

	command := stringSliceAt(
		config,
		[]string{"generated_config", "freshness", "check_command"},
		defaultGeneratedConfigCheckCommand(configSourceRoot),
	)

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
				".bandit.yml",
				".sqlfluff",
				"tombi.toml",
			},
		},
		Evaluators: []Evaluator{
			{
				Kind:    "config",
				Name:    "generated_config.freshness",
				Options: map[string]any{"command": command},
			},
		},
	}
}

func compileEvidenceMaps(
	config map[string]any,
	principles map[string]Principle,
) []diagnostics.EvidenceMap {
	raw, exists := valueAt(config, "policy", "evidence_maps")
	if !exists {
		return defaultEvidenceMaps(principles)
	}

	rawItems, ok := raw.([]any)
	if !ok || len(rawItems) == 0 {
		return defaultEvidenceMaps(principles)
	}

	maps := make([]diagnostics.EvidenceMap, 0, len(rawItems)+len(defaultEvidenceMaps(principles)))
	for _, rawItem := range rawItems {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}

		maps = append(maps, evidenceMapFromConfig(item))
	}

	if len(maps) == 0 {
		return defaultEvidenceMaps(principles)
	}

	return append(maps, defaultEvidenceMaps(principles)...)
}

func evidenceMapFromConfig(item map[string]any) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: stringAt(item, "source"),
		Codes:  stringSliceAt(item, []string{"codes"}, nil),
		MessageSubstrings: stringSliceAt(
			item,
			[]string{"message_substrings"},
			nil,
		),
		PolicyID:     stringAt(item, "policy_id"),
		PrincipleIDs: stringSliceAt(item, []string{"principle_ids"}, nil),
		Confidence:   stringAt(item, "confidence"),
		Meaning:      stringAt(item, "meaning"),
		Advice: diagnostics.EvidenceAdvice{
			Summary: stringAt(item, "advice", "summary"),
			Steps:   stringSliceAt(item, []string{"advice", "steps"}, nil),
			Rerun:   stringSliceAt(item, []string{"advice", "rerun"}, nil),
		},
	}
}

func defaultEvidenceMaps(principles map[string]Principle) []diagnostics.EvidenceMap {
	return []diagnostics.EvidenceMap{
		defaultRuffEvidenceMap(principles),
		defaultRuffImportOrderEvidenceMap(principles),
		defaultRuffSQLSafetyEvidenceMap(principles),
		defaultRuffSecurityEvidenceMap(principles),
		defaultRuffSuppressionEvidenceMap(principles),
		defaultMypySuppressionEvidenceMap(principles),
		defaultPyrightSuppressionEvidenceMap(principles),
		defaultRuffDocstringEvidenceMap(principles),
		defaultPylintDocstringEvidenceMap(principles),
		defaultMypyOptionalTypeEvidenceMap(principles),
		defaultPyrightOptionalTypeEvidenceMap(principles),
		defaultMypyUnknownTypeEvidenceMap(principles),
		defaultPyrightUnknownTypeEvidenceMap(principles),
		defaultPylintInterfaceEvidenceMap(principles),
		defaultPyrightMissingImportEvidenceMap(principles),
		defaultMypyImportCycleEvidenceMap(principles),
		defaultPyrightImportCycleEvidenceMap(principles),
		defaultPylintImportCycleEvidenceMap(principles),
		defaultMypyEvidenceMap(principles),
		defaultShellcheckEvidenceMap(principles),
		defaultYamllintEvidenceMap(principles),
		defaultBanditEvidenceMap(principles),
		defaultSQLFluffEvidenceMap(principles),
		defaultTombiEvidenceMap(principles),
		defaultDotenvLinterEvidenceMap(principles),
		defaultHadolintEvidenceMap(principles),
		defaultActionlintEvidenceMap(principles),
		defaultGolangciEvidenceMap(principles),
	}
}

func defaultRuffEvidenceMap(principles map[string]Principle) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "ruff",
		Codes:    []string{"PLC" + "0415"},
		PolicyID: "python.conditional_imports",
		SkillID:  "conditional-imports",
		PrincipleIDs: principleRefs(
			principles,
			"no-conditional-imports",
			"fail-fast-fail-hard-overview",
		),
		Confidence: "high",
		Meaning: "Import executes away from module scope, usually inside " +
			"runtime control flow, hiding a required dependency or masking " +
			"cyclic design pressure.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Move required imports to module scope. If that exposes " +
				"a cycle, fix the design instead of hiding the dependency.",
			Steps: []string{
				"Declare the dependency as required.",
				"Import it at module scope.",
				"Use SOLID boundaries to split responsibilities when modules depend on each other.",
				"In Python, introduce a Protocol in a neutral module when two concrete implementations would otherwise import each other.",
				"Replace lazy, conditional, or fallback import paths with explicit startup validation.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultRuffImportOrderEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "ruff",
		Codes:    []string{"E402"},
		PolicyID: "python.import_order",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
			"linting-as-code-quality-enforcement",
		),
		Confidence: "high",
		Meaning: "Import ordering is hiding setup side effects or runtime " +
			"dependency flow.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Move imports to the top of the module or split setup into a helper.",
			Steps: []string{
				"Put imports before executable statements.",
				"Move path or environment setup into test fixtures or helper modules.",
				"Keep dependency loading explicit and reviewable.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultRuffSQLSafetyEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "ruff",
		Codes:    []string{"S608"},
		PolicyID: "python.sql_safety",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"linting-as-code-quality-enforcement",
		),
		Confidence: "high",
		Meaning: "SQL text is being assembled dynamically and may bypass " +
			"parameterization.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Use parameterized SQL or a reviewed central SQL helper.",
			Steps: []string{
				"Replace string-built SQL with placeholders and bound parameters.",
				"If dynamic identifiers are required, validate them against an allowlist.",
				"Keep test-only SQL safety exceptions explicit and narrow.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultRuffSecurityEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "ruff",
		Codes:    []string{"S*"},
		PolicyID: "python.security_patterns",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"static-analysis-is-the-first-line-of-defense",
		),
		Confidence: "high",
		Meaning:    "Ruff security rules found code that weakens safe defaults.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix the security finding structurally instead of suppressing it.",
			Steps: []string{
				"Prefer validated inputs and least-privilege behavior.",
				"Replace suspicious APIs or unsafe construction with reviewed helpers.",
				"Keep security exceptions narrow, documented, and reviewable.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultRuffSuppressionEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "ruff",
		Codes:    []string{"RUF100", "PGH003", "PGH004"},
		PolicyID: "python.comment_suppressions",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"linting-as-code-quality-enforcement",
			"universal-responsibility",
		),
		Confidence: "high",
		Meaning: "A lint suppression is stale, broad, or too weakly " +
			"explained to satisfy the code-quality contract.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Remove the suppression or replace it with the narrowest documented exception.",
			Steps: []string{
				"Try the structural fix first.",
				"Remove stale noqa/type-ignore comments.",
				"When an exception is genuinely required, make it narrow and document the reason.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultMypySuppressionEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "mypy",
		Codes:    []string{"unused-ignore", "ignore-without-code"},
		PolicyID: "python.comment_suppressions",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"linting-as-code-quality-enforcement",
			"universal-responsibility",
		),
		Confidence: "high",
		Meaning:    "A type-ignore suppression is stale or too broad.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Remove broad type ignores and fix the type boundary directly.",
			Steps: []string{
				"Delete stale type-ignore comments.",
				"Replace broad ignores with precise types, adapters, or Protocol boundaries.",
				"If an ignore remains necessary, include the exact code and a local reason.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultPyrightSuppressionEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "pyright",
		Codes: []string{
			"reportUnnecessaryTypeIgnoreComment",
			"reportIgnoreCommentWithoutRule",
		},
		PolicyID: "python.comment_suppressions",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"linting-as-code-quality-enforcement",
			"universal-responsibility",
		),
		Confidence: "high",
		Meaning:    "A Pyright ignore comment is stale or missing a precise rule.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Remove unnecessary Pyright ignores or make the remaining exception explicit.",
			Steps: []string{
				"Delete unnecessary ignore comments.",
				"Fix the underlying type issue when Pyright still reports one.",
				"Do not use broad ignore comments as a substitute for correct interfaces.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultRuffDocstringEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "ruff",
		Codes:    []string{"D*"},
		PolicyID: "docs.public_contract",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"documentation-as-contract",
		),
		Confidence: "medium",
		Meaning: "A public module, class, or function is missing contract " +
			"documentation.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Document the public contract instead of leaving behavior implicit.",
			Steps: []string{
				"Add a concise docstring that states purpose, arguments, returns, and raised errors where relevant.",
				"Keep implementation narration out of the docstring.",
				"Update tests when the documented behavior changes.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultPylintDocstringEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "pylint",
		Codes: []string{
			"missing-module-docstring",
			"missing-class-docstring",
			"missing-function-docstring",
			"C0114",
			"C0115",
			"C0116",
		},
		PolicyID: "docs.public_contract",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"documentation-as-contract",
		),
		Confidence: "medium",
		Meaning:    "Pylint found an undocumented public Python contract.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Document the public contract in the code surface that owns it.",
			Steps: []string{
				"Add a useful docstring at the reported module, class, or function.",
				"Explain behavior and constraints, not obvious implementation details.",
				"Keep generated or private surfaces excluded through policy, not ad hoc suppressions.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultMypyOptionalTypeEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "mypy",
		Codes: []string{
			"union-attr",
			"return-value",
			"assignment",
			"arg-type",
		},
		PolicyID: "python.optional_required_types",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"no-optional-types-for-required-dependencies",
			"static-analysis-is-the-first-line-of-defense",
		),
		Confidence: "medium",
		Meaning: "A value used as required is typed as optional or incompatible " +
			"with the required interface.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Make the required contract explicit instead of widening types.",
			Steps: []string{
				"Identify whether the value is genuinely optional or required.",
				"For required dependencies, remove None from the type and validate at construction/startup.",
				"If variants are legitimate, introduce a Protocol or narrower interface instead of passing concrete optionals around.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultPyrightOptionalTypeEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "pyright",
		Codes: []string{
			"reportOptionalCall",
			"reportOptionalIterable",
			"reportOptionalMemberAccess",
			"reportOptionalOperand",
			"reportOptionalSubscript",
		},
		PolicyID: "python.optional_required_types",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"no-optional-types-for-required-dependencies",
			"static-analysis-is-the-first-line-of-defense",
		),
		Confidence: "high",
		Meaning: "Pyright found code using a possibly-None value as if it were " +
			"required.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Convert required optionals into validated required dependencies.",
			Steps: []string{
				"Move absence handling to bootstrap or construction.",
				"Keep runtime code on the full-strength required path.",
				"Use Protocols for dependency boundaries when concrete imports create cycles.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultMypyUnknownTypeEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "mypy",
		Codes: []string{
			"no-untyped-def",
			"no-untyped-call",
			"var-annotated",
		},
		PolicyID: "python.unknown_types",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
			"protocol-first-design",
		),
		Confidence: "medium",
		Meaning: "Type information is missing at a boundary where static " +
			"analysis should verify behavior.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Add precise boundary types instead of letting Any spread.",
			Steps: []string{
				"Annotate public functions and important locals.",
				"Add a typed adapter at untyped third-party boundaries.",
				"Prefer Protocols for behavior contracts instead of concrete catch-all types.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultPyrightUnknownTypeEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "pyright",
		Codes: []string{
			"reportUnknownArgumentType",
			"reportUnknownMemberType",
			"reportUnknownParameterType",
			"reportUnknownVariableType",
		},
		PolicyID: "python.unknown_types",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
			"protocol-first-design",
		),
		Confidence: "medium",
		Meaning:    "Pyright cannot verify a type boundary because unknowns leaked in.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Make the type boundary explicit and locally verifiable.",
			Steps: []string{
				"Add annotations where the value enters the module.",
				"Use typed wrappers around dynamic data.",
				"Prefer Protocols when the code depends on behavior rather than a concrete class.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultPylintInterfaceEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "pylint",
		Codes: []string{
			"no-member",
			"E1101",
			"undefined-variable",
			"E0602",
		},
		PolicyID: "python.interface_contracts",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"protocol-first-design",
			"solid-is-law",
		),
		Confidence: "medium",
		Meaning: "The code is relying on attributes or names that are not " +
			"visible through a stable interface.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Expose the required behavior through a real interface.",
			Steps: []string{
				"Verify the referenced member or name exists.",
				"If the object is dynamic, add a typed adapter or Protocol that states the contract.",
				"Do not hide the issue with a broad Pylint disable.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultPyrightMissingImportEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "pyright",
		Codes: []string{
			"reportMissingImports",
			"reportMissingModuleSource",
		},
		PolicyID: "python.required_imports",
		SkillID:  "conditional-imports",
		PrincipleIDs: principleRefs(
			principles,
			"no-conditional-imports",
			"fail-fast-fail-hard-overview",
		),
		Confidence: "high",
		Meaning:    "A required import cannot be resolved by the static analyzer.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Make required dependencies importable and validated at the gate.",
			Steps: []string{
				"Add the dependency to the environment or generated type-checker config.",
				"Remove fallback or conditional import paths that hide missing dependencies.",
				"Fail at startup/bootstrap when a required dependency is absent.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultMypyImportCycleEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return importCycleEvidenceMap(
		"mypy",
		nil,
		[]string{"Cannot resolve import cycle", "import cycle"},
		principles,
	)
}

func defaultPyrightImportCycleEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return importCycleEvidenceMap(
		"pyright",
		nil,
		[]string{"Import cycle detected", "Import cycles detected"},
		principles,
	)
}

func defaultPylintImportCycleEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return importCycleEvidenceMap(
		"pylint",
		[]string{"cyclic-import", "R0401"},
		nil,
		principles,
	)
}

func importCycleEvidenceMap(
	source string,
	codes []string,
	messageSubstrings []string,
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:            source,
		Codes:             append([]string(nil), codes...),
		MessageSubstrings: append([]string(nil), messageSubstrings...),
		PolicyID:          "python.import_cycles",
		SkillID:           "conditional-imports",
		PrincipleIDs: principleRefs(
			principles,
			"protocol-first-design",
			"solid-is-law",
		),
		Confidence: "medium",
		Meaning: "Concrete modules depend on each other strongly enough that " +
			"the type checker or linter sees an import cycle.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Break the concrete dependency cycle with an explicit interface.",
			Steps: []string{
				"Identify the two modules that import each other.",
				"Move the shared contract into a neutral module.",
				"In Python, model that contract with a Protocol when behavior is required.",
				"Depend on the Protocol or smaller interface instead of the concrete implementation.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultMypyEvidenceMap(principles map[string]Principle) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "mypy",
		Codes:    []string{"no-any-return"},
		PolicyID: "python.optional_returns",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"no-optional-types-for-required-dependencies",
			"static-analysis-is-the-first-line-of-defense",
		),
		Confidence: "medium",
		Meaning:    "A required return path is leaking Any instead of a precise type.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Replace Any return flow with an explicit required type.",
			Steps: []string{
				"Identify the source of Any.",
				"Add the missing annotation or typed adapter at the boundary.",
				"Keep required dependencies non-optional.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultShellcheckEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "shellcheck",
		Codes:    []string{"SC*"},
		PolicyID: "shell.static_analysis",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
			"linting-as-code-quality-enforcement",
		),
		Confidence: "medium",
		Meaning:    "Shellcheck found fragile or ambiguous shell behavior.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix the shell script structure instead of suppressing ShellCheck.",
			Steps: []string{
				"Quote expansions and make data flow explicit.",
				"Prefer arrays and checked commands over stringly shell assembly.",
				"Keep shell behavior deterministic under strict mode.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultYamllintEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "yamllint",
		Codes:    []string{"indentation", "truthy"},
		PolicyID: "yaml.config_clarity",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"validation-at-the-gate",
			"documentation-as-contract",
		),
		Confidence: "medium",
		Meaning: "YAML structure or scalar spelling is ambiguous for " +
			"configuration.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Make YAML configuration explicit and parser-stable.",
			Steps: []string{
				"Fix indentation to match the intended structure.",
				"Quote ambiguous scalars when the value is meant to be a string.",
				"Keep configuration readable enough to review in diffs.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultBanditEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "bandit",
		Codes:    []string{"B*"},
		PolicyID: "python.security_patterns",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"static-analysis-is-the-first-line-of-defense",
		),
		Confidence: "high",
		Meaning:    "Bandit found Python code that weakens safe defaults or input trust boundaries.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix the security issue structurally; do not silence Bandit.",
			Steps: []string{
				"Replace unsafe APIs with validated, least-privilege alternatives.",
				"Move risk acceptance into reviewed policy only when the behavior is intentional.",
				"Keep security-sensitive helpers centralized and covered by tests.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultSQLFluffEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "sqlfluff",
		Codes:    []string{"*"},
		PolicyID: "sql.static_analysis",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"validation-at-the-gate",
			"static-analysis-is-the-first-line-of-defense",
		),
		Confidence: "medium",
		Meaning:    "SQL linting found syntax, layout, or dialect ambiguity before database execution.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Make SQL dialect and structure explicit before committing.",
			Steps: []string{
				"Fix SQL syntax and layout under the configured dialect.",
				"Keep dynamic SQL in reviewed central helpers.",
				"Use parameterized values and validated identifier allowlists.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultTombiEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "tombi",
		Codes:    []string{"*"},
		PolicyID: "toml.config_clarity",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"validation-at-the-gate",
			"documentation-as-contract",
		),
		Confidence: "medium",
		Meaning:    "TOML configuration is invalid or ambiguous for downstream tools.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix TOML configuration before tools consume it.",
			Steps: []string{
				"Repair syntax or schema ordering issues in the reported TOML file.",
				"Keep generated tool configs synchronized from policy sources.",
				"Prefer explicit config over tool defaults.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultDotenvLinterEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "dotenv-linter",
		Codes:    []string{"*"},
		PolicyID: "dotenv.config_clarity",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"validation-at-the-gate",
		),
		Confidence: "medium",
		Meaning:    "Dotenv files encode local runtime contracts and must stay unambiguous.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix dotenv entries so environment contracts are explicit and safe.",
			Steps: []string{
				"Use uppercase keys and remove duplicate or malformed entries.",
				"Keep real secrets out of committed dotenv files.",
				"Prefer example/template files with safe placeholder values.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultHadolintEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "hadolint",
		Codes:    []string{"DL*"},
		PolicyID: "docker.reproducible_builds",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"evidence-based-engineering-and-decision-quality",
		),
		Confidence: "medium",
		Meaning: "Dockerfile instructions weaken reproducibility or " +
			"container safety.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Make the container build deterministic and least-privilege.",
			Steps: []string{
				"Pin package versions where practical.",
				"Avoid broad shell pipelines that hide failures.",
				"Prefer explicit users, trusted sources, and minimal layers.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultActionlintEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "actionlint",
		Codes:    []string{"*"},
		PolicyID: "workflow.validation",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"validation-at-the-gate",
			"testing-as-specification",
		),
		Confidence: "high",
		Meaning: "GitHub Actions workflow syntax or expression behavior " +
			"is invalid.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix workflow definitions before relying on CI as a quality gate.",
			Steps: []string{
				"Validate expressions, job wiring, and event-specific context.",
				"Keep workflow behavior explicit instead of runtime surprises.",
				"Re-run the workflow hook locally before pushing.",
			},
			Rerun: []string{"make pre-commit"},
		},
	}
}

func defaultGolangciEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source: "golangci-lint",
		Codes: []string{
			"errcheck",
			"gosec",
			"staticcheck",
			"revive",
		},
		PolicyID: "go.static_analysis",
		SkillID:  "lint-remediation",
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
			"linting-as-code-quality-enforcement",
		),
		Confidence: "high",
		Meaning: "Go static analysis found correctness, security, or " +
			"maintainability risk.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Fix the Go issue structurally and keep golangci-lint blocking.",
			Steps: []string{
				"Handle errors explicitly.",
				"Remove suspicious or insecure constructs instead of suppressing them.",
				"Prefer a small refactor over weakening lint coverage.",
			},
			Rerun: []string{"make pre-commit", "make check"},
		},
	}
}

func defaultGeneratedConfigCheckCommand(configSourceRoot string) []string {
	return []string{
		"uv",
		"run",
		"--project",
		configSourceRoot,
		"python",
		filepath.Join(configSourceRoot, "main.py"),
		"--repo",
		".",
		"--check-tool-configs",
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
	addProtectedPathDispatch(hooks, policies)
	addProtectedBranchWriteDispatch(hooks, policies)
	addPythonWriteDispatch(hooks, policies)
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
				PolicyID: "git.hook_bypass",
				Mode:     "block",
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
		"git.commitlint",
		"git.commit_attribution",
		"shell.dangerous_command",
		"shell.background_git",
		"shell.github_admin",
		"shell.forbidden_strings",
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

func addProtectedBranchWriteDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["filesystem.protected_branch_write"]; !ok {
		return
	}

	for _, tool := range []string{"Bash", "Write", "Edit", "MultiEdit"} {
		ensureHookTool(hooks, "PreToolUse", tool)
		hooks["PreToolUse"][tool] = append(
			hooks["PreToolUse"][tool],
			HookDispatchEntry{
				PolicyID: "filesystem.protected_branch_write",
				Mode:     "block",
			},
		)
	}
}

func addProtectedPathDispatch(
	hooks map[string]map[string][]HookDispatchEntry,
	policies map[string]Policy,
) {
	if _, ok := policies["filesystem.protected_path"]; !ok {
		return
	}

	for _, tool := range []string{"Bash", "Write", "Edit", "MultiEdit"} {
		ensureHookTool(hooks, "PreToolUse", tool)
		hooks["PreToolUse"][tool] = append(
			hooks["PreToolUse"][tool],
			HookDispatchEntry{
				PolicyID: "filesystem.protected_path",
				Mode:     "block",
			},
		)
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
		"python.bare_except",
		"python.unexplained_type_ignore",
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
			"syntax.file_syntax",
			"syntax.merge_conflict",
			"security.private_key",
			"filesystem.shebangs",
			"filesystem.large_files",
			"filesystem.line_limits",
			"repo.pii_scrubber",
			"repo.license_header",
			"shell.best_practices",
			"shell.forbidden_strings",
			"python.conditional_imports",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
			"python.pyproject_ignores",
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
			"shell.github_admin",
			"shell.forbidden_strings",
			"shell.best_practices",
			"git.commit_attribution",
			"git.staged_admin_files",
			"filesystem.protected_path",
			"filesystem.protected_branch_write",
			"repo.required_ignores",
			"syntax.file_syntax",
			"syntax.merge_conflict",
			"security.private_key",
			"filesystem.shebangs",
			"filesystem.large_files",
			"filesystem.line_limits",
			"repo.pii_scrubber",
			"repo.license_header",
			"python.conditional_imports",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
			"python.bare_except",
			"python.unexplained_type_ignore",
			"python.pyproject_ignores",
		),
		"smoke": existingPolicyIDs(
			policies,
			"repo.required_ignores",
			"generated_config.freshness",
			"pytest.gate",
		),
		"full": existingPolicyIDs(
			policies,
			"repo.required_ignores",
			"generated_config.freshness",
			"pytest.gate",
		),
		"cutover": existingPolicyIDs(
			policies,
			"repo.required_ignores",
		),
		"commit-msg": existingPolicyIDs(
			policies,
			"git.commitlint",
			"git.commit_attribution",
		),
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
				"git.commitlint",
				"git.commit_attribution",
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

func enabledAt(values map[string]any, path []string) bool {
	value, exists := valueAt(values, append(path, "enabled")...)
	if !exists {
		return true
	}

	boolValue, isBool := value.(bool)

	return !isBool || boolValue
}

func stringSliceAt(
	values map[string]any,
	path []string,
	defaults []string,
) []string {
	value, exists := valueAt(values, path...)
	if !exists {
		return append([]string(nil), defaults...)
	}

	items := stringSlice(value)
	if len(items) == 0 {
		return append([]string(nil), defaults...)
	}

	return items
}

func intAt(values map[string]any, path []string, defaultValue int) int {
	value, exists := valueAt(values, path...)
	if !exists {
		return defaultValue
	}

	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		return defaultValue
	}
}

func stringAt(values map[string]any, path ...string) string {
	value, exists := valueAt(values, path...)
	if !exists {
		return ""
	}

	stringValue, ok := value.(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(stringValue)
}

func policyConfigEnabled(values map[string]any, policyID string) bool {
	path := append(strings.Split(policyID, "."), "enabled")

	value, exists := valueAt(values, path...)
	if !exists {
		return true
	}

	boolValue, isBool := value.(bool)

	return !isBool || boolValue
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
