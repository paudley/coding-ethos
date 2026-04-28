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

	policies := compilePolicies(configPayload, principles, sourceRoot(options.Config))
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
	configSourceRoot string,
) map[string]Policy {
	policies := map[string]Policy{}
	addConfiguredPythonPolicies(policies, config, principles)
	addGitPolicies(policies, config, principles)
	addSyntaxPolicies(policies, config, principles)
	addShellPolicies(policies, config, principles)
	addFilesystemPolicies(policies, config, principles)
	addFileGuardPolicies(policies, config, principles)
	addGeneratedConfigPolicy(policies, config, principles, configSourceRoot)

	return policies
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

	if policyConfigEnabled(config, "git.commit_attribution") {
		policies["git.commit_attribution"] = gitCommitAttributionPolicy(config, principles)
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
	principles map[string]Principle,
) {
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
			"/coding-ethos/pre-commit/hooks/",
			"/coding-ethos/go/internal/",
			"/coding-ethos/config.yaml",
			"/coding-ethos/ruff.toml",
			"/coding-ethos/.golangci.yml",
			"header must match",
		},
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
		Message: "Commands must not contain or execute files containing " +
			"forbidden hook-system strings.",
		Suggestion: "Do not inspect, enumerate, or route around coding-ethos " +
			"hook implementation internals. Use the installed hook surfaces " +
			"and documented commands.",
		DefenseLayers: GitDefenseLayers("block", "", "block", "", ""),
		AppliesTo:     AppliesTo{Tools: []string{"Bash"}},
		Evaluators: []Evaluator{{
			Kind:    "shell",
			Name:    "shell.forbidden_strings",
			Options: map[string]any{"strings": strings},
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
}

func filesystemProtectedPathPolicy(
	config map[string]any,
	principles map[string]Principle,
) Policy {
	protectedPaths := stringSliceAt(
		config,
		[]string{"filesystem", "protected_path", "paths"},
		[]string{"/usr/bin/got"},
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
		Message:         "Protected paths must not be modified.",
		Suggestion:      "Do not write to protected system paths.",
		DefenseLayers:   GitDefenseLayers("block", "", "block", "", ""),
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
		Source:       stringAt(item, "source"),
		Codes:        stringSliceAt(item, []string{"codes"}, nil),
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
		defaultMypyEvidenceMap(principles),
		defaultShellcheckEvidenceMap(principles),
		defaultYamllintEvidenceMap(principles),
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
		PrincipleIDs: principleRefs(
			principles,
			"no-conditional-imports",
			"fail-fast-fail-hard-overview",
		),
		Confidence: "high",
		Meaning: "Import executes away from module scope, usually inside " +
			"runtime control flow.",
		Advice: diagnostics.EvidenceAdvice{
			Summary: "Move required imports to module scope and fail during startup.",
			Steps: []string{
				"Declare the dependency as required.",
				"Import it at module scope.",
				"Replace runtime fallback paths with startup validation.",
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

func defaultMypyEvidenceMap(principles map[string]Principle) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "mypy",
		Codes:    []string{"no-any-return"},
		PolicyID: "python.optional_returns",
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

func defaultHadolintEvidenceMap(
	principles map[string]Principle,
) diagnostics.EvidenceMap {
	return diagnostics.EvidenceMap{
		Source:   "hadolint",
		Codes:    []string{"DL*"},
		PolicyID: "docker.reproducible_builds",
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
			"shell.best_practices",
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
			"shell.github_admin",
			"shell.forbidden_strings",
			"shell.best_practices",
			"git.commit_attribution",
			"git.staged_admin_files",
			"filesystem.protected_path",
			"filesystem.protected_branch_write",
			"filesystem.required_ignores",
			"syntax.file_syntax",
			"syntax.merge_conflict",
			"security.private_key",
			"filesystem.shebangs",
			"filesystem.large_files",
			"filesystem.line_limits",
			"python.conditional_imports",
			"python.optional_returns",
			"python.catch_and_silence",
			"python.structured_logging",
			"python.direct_imports",
			"python.bare_except",
			"python.unexplained_type_ignore",
		),
		"smoke": existingPolicyIDs(
			policies,
			"filesystem.required_ignores",
			"generated_config.freshness",
			"pytest.gate",
		),
		"full": existingPolicyIDs(
			policies,
			"filesystem.required_ignores",
			"generated_config.freshness",
			"pytest.gate",
		),
		"cutover": existingPolicyIDs(
			policies,
			"filesystem.required_ignores",
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
