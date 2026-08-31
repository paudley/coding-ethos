// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	defaultCommitHeaderMaxLength = 150
	defaultLicenseScanLines      = 5
	defaultLicenseClientTimeout  = 10 * time.Second
	spdxLicenseTextBaseURL       = "https://raw.githubusercontent.com/" +
		"spdx/license-list-data/v3.28.0/text/"
	piiScrubberSuggestion = "Replace local paths, usernames, " +
		"hostnames, and worktree names with generic placeholders."
	shebangSuggestion = "Add a valid shebang to executable scripts and " +
		"mark shebang scripts executable."
)

var spdxLicenseIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+-]*$`)

var errInvalidSPDXIdentifier = apperror.StaticError("invalid SPDX identifier")

func compilePolicies(
	config map[string]any,
	repoConfig map[string]any,
	expressionSources []expressionPolicySource,
	principles map[string]Principle,
	configSourceRoot string,
) (map[string]Policy, error) {
	policies := map[string]Policy{}
	addConfiguredPythonPolicies(policies, config, principles)
	addGitPolicies(policies, config, principles, configSourceRoot)
	addSyntaxPolicies(policies, config, principles)
	addShellPolicies(policies, config, principles)
	addProxyPolicies(policies, config)

	err := addFileGuardPolicies(policies, config, repoConfig, principles)
	if err != nil {
		return nil, err
	}

	addGeneratedConfigPolicy(policies, principles, configSourceRoot)
	addGeneratedGeminiPromptsPolicy(policies, principles, configSourceRoot)
	addGeneratedAgentSkillsPolicy(policies, principles, configSourceRoot)

	err = addExpressionPolicies(
		policies,
		expressionSources,
		config,
		principles,
	)
	if err != nil {
		return nil, err
	}

	return policies, nil
}

func addProxyPolicies(policies map[string]Policy, config map[string]any) {
	policyDef := ProxySearchReplaceEditPolicy()
	if policyConfigEnabled(config, policyDef.ID) {
		policies[policyDef.ID] = policyDef
	}
}

func addGitPolicies(
	policies map[string]Policy,
	config map[string]any,
	principles map[string]Principle,
	configSourceRoot string,
) {
	for _, policy := range gitPolicies(config, principles, configSourceRoot) {
		policies[policy.ID] = policy
	}

	policies["git.wrapper_required"] = gitWrapperRequiredPolicy(principles)

	// Blocks issued directly by the Go hook routes. Registered so a block can
	// be looked up, attributed and tuned; enforcement stays in the routes.
	maps.Copy(policies, hookRoutePolicies(principles))

	if enabledAt(config, []string{"go", "commit_attribution"}) {
		policies["git.commit_attribution"] = gitCommitAttributionPolicy(
			config,
			principles,
		)
	}

	if enabledAt(config, []string{"go", "commitlint"}) {
		policies["git.commitlint"] = gitCommitLintPolicy(config, principles)
	}
}

func gitPolicies(
	config map[string]any,
	principles map[string]Principle,
	configSourceRoot string,
) []Policy {
	return []Policy{
		gitStagedAdminPolicy(config, principles, configSourceRoot),
		gitCommitHeadPolicy(principles),
	}
}

func gitStagedAdminPolicy(
	config map[string]any,
	principles map[string]Principle,
	configSourceRoot string,
) Policy {
	return Policy{
		ID:              "git.staged_admin_files",
		Category:        "git",
		Source:          SourceRef{File: "config.yaml", Path: "git.staged_admin_files"},
		PrincipleIDs:    principleRefs(principles, "one-path-for-critical-operations"),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "record"},
		Message:         "Administrative staged files require explicit handling.",
		Suggestion: "Ask an admin to approve this coding-ethos session, then " +
			"run the protected git command with --admin-approved.",
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
				"ethos_root": configSourceRoot,
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
		AppliesTo: AppliesTo{
			Commands: []string{"git commit"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{
			{Kind: "git_state", Name: "git.commit_head_advanced"},
		},
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
		AppliesTo: AppliesTo{
			Commands: []string{"git commit"},
			Tools:    []string{"Bash"},
		},
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
		Suggestion: sentence(
			"Use exactly: type(scope): concise subject,",
			"then a blank line before the body.",
		),
		DefenseLayers: GitDefenseLayers(
			"block",
			"wrapper",
			"block",
			"commit_msg",
			"",
		),
		AppliesTo: AppliesTo{
			Commands: []string{"git commit"},
			Tools:    []string{"Bash"},
		},
		Evaluators: []Evaluator{{
			Kind: "git_state",
			Name: "git.commitlint",
			Options: map[string]any{
				"allowed_types": stringSliceAt(
					config,
					[]string{"go", "commitlint", "allowed_types"},
					[]string{
						"chore",
						"docs",
						"feat",
						"fix",
						"perf",
						"refactor",
						"test",
					},
				),
				"ignored_prefixes": stringSliceAt(
					config,
					[]string{"go", "commitlint", "ignored_prefixes"},
					[]string{"Merge ", "Revert ", "fixup! ", "squash! "},
				),
				"max_header_length": intAt(
					config,
					[]string{"go", "commitlint", "max_header_length"},
					defaultCommitHeaderMaxLength,
				),
			},
		}},
	}
}

func addFileGuardPolicies(
	policies map[string]Policy,
	config map[string]any,
	repoConfig map[string]any,
	principles map[string]Principle,
) error {
	if policyConfigEnabled(config, "security.private_key") {
		policies["security.private_key"] = privateKeyPolicy(config, principles)
	}

	if policyConfigEnabled(config, "filesystem.shebangs") {
		policies["filesystem.shebangs"] = shebangPolicy(principles)
	}

	if enabledAt(config, []string{"filesystem", "pii_scrubber"}) {
		policies["repo.pii_scrubber"] = piiScrubberPolicy(config, principles)
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

func privateKeyPolicy(config map[string]any, principles map[string]Principle) Policy {
	pattern := stringAt(config, "security", "private_key", "pattern")
	if pattern == "" {
		pattern = `-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----`
	}

	return Policy{
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

func shebangPolicy(principles map[string]Principle) Policy {
	return Policy{
		ID:       "filesystem.shebangs",
		Category: "filesystem",
		Source:   SourceRef{File: "config.yaml", Path: "filesystem.shebangs"},
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Executable scripts and shebangs must agree.",
		Suggestion:      shebangSuggestion,
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
		Evaluators:      []Evaluator{{Kind: "text", Name: "filesystem.shebangs"}},
	}
}

func piiScrubberPolicy(config map[string]any, principles map[string]Principle) Policy {
	return Policy{
		ID:       "repo.pii_scrubber",
		Category: "repo",
		Source:   SourceRef{File: "config.yaml", Path: "filesystem.pii_scrubber"},
		PrincipleIDs: principleRefs(
			principles,
			"security-by-design",
			"radical-visibility",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Local-machine PII must not be committed.",
		Suggestion:      piiScrubberSuggestion,
		DefenseLayers:   CodeDefenseLayers(),
		AppliesTo:       AppliesTo{FilePatterns: []string{"**/*"}},
		Evaluators: []Evaluator{{
			Kind:    "text",
			Name:    "repo.pii_scrubber",
			Options: piiScrubberOptions(config),
		}},
	}
}

func piiScrubberOptions(config map[string]any) map[string]any {
	return map[string]any{
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
	}
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
			configLicenseHeaderOptions(config),
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

	options := repoLicenseHeaderOptions(config, spdxID, copyrightText, licenseFile)

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

func configLicenseHeaderOptions(config map[string]any) map[string]any {
	return map[string]any{
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
		"scan_lines": intAt(
			config,
			[]string{"filesystem", "license_header", "scan_lines"},
			defaultLicenseScanLines,
		),
	}
}

func repoLicenseHeaderOptions(
	config map[string]any,
	spdxID string,
	copyrightText string,
	licenseFile string,
) map[string]any {
	return map[string]any{
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
		"required":     repoLicenseRequiredHeaders(spdxID, copyrightText),
		"scan_lines":   repoLicenseScanLines(config),
		"license_file": licenseFile,
		"spdx_id":      spdxID,
	}
}

func repoLicenseRequiredHeaders(spdxID, copyrightText string) []string {
	required := []string{}
	if spdxID != "" {
		required = append(required, "SPDX-License-Identifier: "+spdxID)
	}

	if copyrightText != "" {
		required = append(required, "SPDX-FileCopyrightText: "+copyrightText)
	}

	return required
}

func repoLicenseScanLines(config map[string]any) int {
	return intAt(
		config,
		[]string{"repo", "license", "scan_lines"},
		defaultLicenseScanLines,
	)
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
		Message: sentence(
			"First-party source files must carry the configured SPDX",
			"license contract.",
		),
		Suggestion:    "Add the configured LICENSE file and matching SPDX source headers.",
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo: AppliesTo{
			FilePatterns: []string{"**/*.go", "**/*.py", "**/*.sh"},
		},
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
	if text := repoLicenseTextString(config); text != "" {
		return normalizeLicenseText(fillLicenseTemplate(text, copyrightText)), nil
	}

	url := stringAt(config, "repo", "license", "url")
	if url == "" {
		err := validateSPDXLicenseID(spdxID)
		if err != nil {
			return "", err
		}

		url = spdxLicenseTextURL(spdxID)
	}

	client := http.Client{Timeout: defaultLicenseClientTimeout}

	request, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodGet,
		url,
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create SPDX license request %s: %w", spdxID, err)
	}

	response, err := client.Do(request)
	if err != nil {
		return "", fmt.Errorf("download SPDX license %s: %w", spdxID, err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", apperror.Wrapf(
			apperror.StaticError("download SPDX license %s: status %s"),
			"download SPDX license %s: status %s",
			spdxID,
			response.Status,
		)
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return "", fmt.Errorf("read SPDX license %s: %w", spdxID, err)
	}

	return normalizeLicenseText(fillLicenseTemplate(string(body), copyrightText)), nil
}

func spdxLicenseTextURL(spdxID string) string {
	return spdxLicenseTextBaseURL + spdxID + ".txt"
}

func validateSPDXLicenseID(spdxID string) error {
	if !spdxLicenseIDPattern.MatchString(spdxID) || strings.Contains(spdxID, "..") {
		return fmt.Errorf("%w: %s", errInvalidSPDXIdentifier, spdxID)
	}

	return nil
}

func fillLicenseTemplate(text, copyrightText string) string {
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

func repoLicenseTextString(config map[string]any) string {
	value, exists := valueAt(config, "repo", "license", "text")
	if !exists {
		return ""
	}

	stringValue, ok := value.(string)
	if !ok || strings.TrimSpace(stringValue) == "" {
		return ""
	}

	return stringValue
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
				FilePatterns: []string{
					"**/*.json",
					"**/*.toml",
					"**/*.yaml",
					"**/*.yml",
				},
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
			ID:       "syntax.merge_conflict",
			Category: "syntax",
			Source: SourceRef{
				File: "config.yaml",
				Path: "syntax.merge_conflict",
			},
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
			"shell.malformed_command",
			principleRefs(
				principles,
				"validation-at-the-gate",
				"one-path-for-critical-operations",
			),
			"Malformed shell command text is forbidden.",
			"Rewrite the command as valid shell syntax before continuing.",
		),
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
		ID:       "shell.best_practices",
		Category: "shell",
		Source:   SourceRef{File: "config.yaml", Path: "shell.best_practices"},
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "record"},
		Message:         "Shell scripts must follow repository shell safety practices.",
		Suggestion: sentence(
			"Use a valid shell shebang, strict mode, and required common",
			"helpers.",
		),
		DefenseLayers: CodeDefenseLayers(),
		AppliesTo:     AppliesTo{FilePatterns: []string{"**/*.sh", "**/*.bash"}},
		Evaluators: []Evaluator{{
			Kind: "shell",
			Name: "shell.best_practices",
			Options: map[string]any{
				"require_common_for_prefixes": requireCommon,
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

func addGeneratedConfigPolicy(
	policies map[string]Policy,
	principles map[string]Principle,
	configSourceRoot string,
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
				".bandit.yml",
				".sqlfluff",
				"tombi.toml",
			},
		},
		Evaluators: []Evaluator{
			{
				Kind:    "config",
				Name:    "generated_config.freshness",
				Options: generatedConfigFreshnessOptions(configSourceRoot),
			},
		},
	}
}

func generatedConfigFreshnessOptions(
	configSourceRoot string,
) map[string]any {
	return map[string]any{
		"ethos_root": configSourceRoot,
		"repo":       ".",
	}
}

func addGeneratedGeminiPromptsPolicy(
	policies map[string]Policy,
	principles map[string]Principle,
	configSourceRoot string,
) {
	policies["generated_gemini_prompts.freshness"] = Policy{
		ID:       "generated_gemini_prompts.freshness",
		Category: "config",
		Source: SourceRef{
			File: "config.yaml",
			Path: "generated_gemini_prompts.freshness",
		},
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "advise", "annotate", "record"},
		Message:         "Generated Gemini prompt pack must match source policy.",
		Suggestion:      "Run the configured Gemini prompt sync/check command.",
		DefenseLayers:   GeneratedConfigDefenseLayers(),
		AppliesTo: AppliesTo{
			Paths: []string{".coding-ethos/gemini/prompt-pack.json"},
		},
		Evaluators: []Evaluator{
			{
				Kind:    "config",
				Name:    "generated_gemini_prompts.freshness",
				Options: generatedGeminiPromptsFreshnessOptions(configSourceRoot),
			},
		},
	}
}

func generatedGeminiPromptsFreshnessOptions(
	configSourceRoot string,
) map[string]any {
	return map[string]any{
		"ethos_root": configSourceRoot,
		"repo":       ".",
	}
}

func addGeneratedAgentSkillsPolicy(
	policies map[string]Policy,
	principles map[string]Principle,
	configSourceRoot string,
) {
	policies["generated_agent_skills.freshness"] = Policy{
		ID:       "generated_agent_skills.freshness",
		Category: "config",
		Source: SourceRef{
			File: "config.yaml",
			Path: "generated_agent_skills.freshness",
		},
		PrincipleIDs: principleRefs(
			principles,
			"static-analysis-is-the-first-line-of-defense",
			"generated-files-are-derived-artifacts",
		),
		DefaultSeverity: "block",
		SupportedModes:  []string{"block", "ask", "advise", "annotate", "record"},
		Message:         "Generated agent skill surfaces must match source policy.",
		Suggestion:      "Run the configured agent skill sync/check command.",
		DefenseLayers:   GeneratedConfigDefenseLayers(),
		AppliesTo: AppliesTo{
			Paths: []string{
				".agents/skills",
				".claude/skills",
				".codex/skills",
				".gemini/extensions/coding-ethos/skills",
				".gemini/extensions/coding-ethos/gemini-extension.json",
			},
		},
		Evaluators: []Evaluator{
			{
				Kind:    "config",
				Name:    "generated_agent_skills.freshness",
				Options: generatedAgentSkillsFreshnessOptions(configSourceRoot),
			},
		},
	}
}

func generatedAgentSkillsFreshnessOptions(
	configSourceRoot string,
) map[string]any {
	return map[string]any{
		"ethos_root": configSourceRoot,
		"repo":       ".",
	}
}

func firstPresentValue(values map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := values[key]; ok {
			return value
		}
	}

	return nil
}

func expressionPrincipleIDs(expression map[string]any) []string {
	return stringSliceValue(expression["principle_ids"], nil)
}

func defaultExpressionDispatchScopes(scope string) []string {
	switch scope {
	case "commit-msg":
		return []string{"commit-msg"}
	case "smoke", "full", "cutover":
		return []string{scope}
	default:
		return []string{"files", "staged"}
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

	maps := make(
		[]diagnostics.EvidenceMap,
		0,
		len(rawItems)+len(defaultEvidenceMaps(principles)),
	)
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
		SkillID:      stringAt(item, "skill_id"),
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
				sentence(
					"Use SOLID boundaries to split responsibilities when",
					"modules depend on each other.",
				),
				sentence(
					"In Python, introduce a Protocol in a neutral module",
					"when two concrete implementations would otherwise",
					"import each other.",
				),
				sentence(
					"Replace lazy, conditional, or fallback import paths",
					"with explicit startup validation.",
				),
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
			Summary: sentence(
				"Remove the suppression or replace it with the narrowest",
				"documented exception.",
			),
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
			Summary: sentence(
				"Remove unnecessary Pyright ignores or make the remaining",
				"exception explicit.",
			),
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
				sentence(
					"Add a concise docstring that states purpose,",
					"arguments, returns, and raised errors where relevant.",
				),
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
				sentence(
					"Keep generated or private surfaces excluded through",
					"policy, not ad hoc suppressions.",
				),
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
				sentence(
					"For required dependencies, remove None from the type",
					"and validate at construction/startup.",
				),
				sentence(
					"If variants are legitimate, introduce a Protocol or",
					"narrower interface instead of passing concrete",
					"optionals around.",
				),
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
				sentence(
					"If the object is dynamic, add a typed adapter or",
					"Protocol that states the contract.",
				),
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
				sentence(
					"Depend on the Protocol or smaller interface instead",
					"of the concrete implementation.",
				),
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
		Meaning: sentence(
			"Bandit found Python code that weakens safe defaults or input",
			"trust boundaries.",
		),
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
		Meaning: sentence(
			"SQL linting found syntax, layout, or dialect ambiguity before",
			"database execution.",
		),
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
