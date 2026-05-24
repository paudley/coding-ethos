// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"fmt"
	"maps"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
)

const (
	severityRankBlock   = 50
	severityRankAsk     = 40
	severityRankAdvise  = 30
	severityRankRecord  = 20
	severityRankDefault = 0
)

type expressionPolicySource struct {
	File        string
	PathPrefix  string
	Expressions []any
}

type expressionPolicyGovernance struct {
	OverrideReason      string
	Override            bool
	AllowOverride       bool
	AllowSeverityWeaken bool
	Protected           bool
}

func expressionPolicySourceFromConfig(
	config map[string]any,
	file string,
) (expressionPolicySource, bool, error) {
	rawExpressions, found := valueAt(config, "policy", "expressions")
	if !found {
		return expressionPolicySource{}, false, nil
	}

	expressions, found := rawExpressions.([]any)
	if !found {
		return expressionPolicySource{}, false, apperror.Wrapf(
			apperror.StaticError("%s policy.expressions must be a list"),
			"%s policy.expressions must be a list",
			file,
		)
	}

	return expressionPolicySource{
		File:        file,
		PathPrefix:  "policy.expressions",
		Expressions: expressions,
	}, true, nil
}

func expressionPolicySourcesFromPrinciples(
	ethos map[string]any,
	file string,
) []expressionPolicySource {
	rawPrinciples, found := ethos["principles"].([]any)
	if !found {
		return nil
	}

	sources := []expressionPolicySource{}

	for index, rawPrinciple := range rawPrinciples {
		principle, found := rawPrinciple.(map[string]any)
		if !found {
			continue
		}

		rawExpressions, found := valueAt(principle, "policy", "expressions")
		if !found {
			continue
		}

		expressions, found := rawExpressions.([]any)
		if !found {
			sources = append(sources, expressionPolicySource{
				File:        file,
				PathPrefix:  fmt.Sprintf("principles[%d].policy.expressions", index),
				Expressions: []any{rawExpressions},
			})

			continue
		}

		principleID := strings.TrimSpace(fmt.Sprint(principle["id"]))
		sources = append(sources, expressionPolicySource{
			File: file,
			PathPrefix: fmt.Sprintf(
				"principles[%s].policy.expressions",
				principleID,
			),
			Expressions: principleExpressionDefaults(expressions, principleID),
		})
	}

	return sources
}

func expressionPolicySourcesFromRepoEthos(
	ethos map[string]any,
	file string,
) []expressionPolicySource {
	rawOverrides, found := valueAt(ethos, "principles", "overrides")
	if !found {
		return nil
	}

	overrides, found := rawOverrides.(map[string]any)
	if !found {
		return []expressionPolicySource{{
			File:        file,
			PathPrefix:  "principles.overrides",
			Expressions: []any{rawOverrides},
		}}
	}

	sources := []expressionPolicySource{}

	for principleID, rawOverride := range overrides {
		override, found := rawOverride.(map[string]any)
		if !found {
			continue
		}

		rawExpressions, found := valueAt(override, "policy", "expressions")
		if !found {
			continue
		}

		expressions, found := rawExpressions.([]any)
		if !found {
			sources = append(sources, expressionPolicySource{
				File: file,
				PathPrefix: fmt.Sprintf(
					"principles.overrides[%s].policy.expressions",
					principleID,
				),
				Expressions: []any{rawExpressions},
			})

			continue
		}

		sources = append(sources, expressionPolicySource{
			File: file,
			PathPrefix: fmt.Sprintf(
				"principles.overrides[%s].policy.expressions",
				principleID,
			),
			Expressions: principleExpressionDefaults(expressions, principleID),
		})
	}

	return sources
}

func principleExpressionDefaults(expressions []any, principleID string) []any {
	normalized := make([]any, 0, len(expressions))
	for _, rawExpression := range expressions {
		expression, found := rawExpression.(map[string]any)
		if !found {
			normalized = append(normalized, rawExpression)

			continue
		}

		copied := map[string]any{}
		maps.Copy(copied, expression)

		if _, found := copied["principle_ids"]; !found && principleID != "" {
			copied["principle_ids"] = []any{principleID}
		}

		normalized = append(normalized, copied)
	}

	return normalized
}

func addExpressionPolicies(
	policies map[string]Policy,
	sources []expressionPolicySource,
	config map[string]any,
	principles map[string]Principle,
) error {
	for _, source := range sources {
		err := addExpressionPoliciesFromSource(
			policies,
			source,
			config,
			principles,
		)
		if err != nil {
			return err
		}
	}

	return nil
}

func addExpressionPoliciesFromSource(
	policies map[string]Policy,
	source expressionPolicySource,
	config map[string]any,
	principles map[string]Principle,
) error {
	for index, rawExpression := range source.Expressions {
		expression, found := rawExpression.(map[string]any)
		if !found {
			return apperror.Wrapf(
				apperror.StaticError("%s %s[%d] must be a mapping"),
				"%s %s[%d] must be a mapping",
				source.File,
				source.PathPrefix,
				index,
			)
		}

		policyDef, enabled, governance, err := expressionPolicy(
			expression,
			index,
			source.File,
			source.PathPrefix,
			config,
			principles,
		)
		if err != nil {
			return err
		}

		if !enabled {
			continue
		}

		if existing, exists := policies[policyDef.ID]; exists {
			err := validateExpressionPolicyOverride(
				source.File,
				index,
				policyDef,
				governance,
				existing,
			)
			if err != nil {
				return err
			}
		} else if governance.Override {
			return apperror.Wrapf(
				apperror.StaticError(
					"%s %s[%d].id %q declares override but no existing policy matches",
				),
				"%s %s[%d].id %q declares override but no existing policy matches",
				source.File,
				source.PathPrefix,
				index,
				policyDef.ID,
			)
		}

		policies[policyDef.ID] = policyDef
	}

	return nil
}

func expressionPolicy(
	expression map[string]any,
	index int,
	sourceFile string,
	sourcePathPrefix string,
	config map[string]any,
	principles map[string]Principle,
) (Policy, bool, expressionPolicyGovernance, error) {
	parts, governance, enabled, err := compileExpressionPolicyParts(
		expression,
		index,
		sourceFile,
		sourcePathPrefix,
		config,
		principles,
	)
	if err != nil {
		return Policy{}, false, expressionPolicyGovernance{}, err
	}

	if !enabled {
		return Policy{}, false, governance, nil
	}

	return buildExpressionPolicy(parts), true, governance, nil
}

func compileExpressionPolicyParts(
	expression map[string]any,
	index int,
	sourceFile string,
	sourcePathPrefix string,
	config map[string]any,
	principles map[string]Principle,
) (expressionPolicyParts, expressionPolicyGovernance, bool, error) {
	policyID, governance, enabled, err := expressionPolicyPrelude(
		expression,
		index,
		sourceFile,
		sourcePathPrefix,
	)
	if err != nil {
		return expressionPolicyParts{}, expressionPolicyGovernance{}, false, err
	}

	if !enabled {
		return expressionPolicyParts{}, governance, false, nil
	}

	content, err := expressionPolicyContent(
		expression,
		index,
		sourceFile,
		sourcePathPrefix,
		policyID,
		principles,
	)
	if err != nil {
		return expressionPolicyParts{}, expressionPolicyGovernance{}, false, err
	}

	return expressionPolicyPartsFromContent(
		expression,
		config,
		governance,
		sourceFile,
		sourcePathPrefix,
		index,
		content,
	), governance, true, nil
}

func expressionPolicyPrelude(
	expression map[string]any,
	index int,
	sourceFile string,
	sourcePathPrefix string,
) (string, expressionPolicyGovernance, bool, error) {
	policyID, err := expressionRequiredString(
		expression,
		"id",
		sourceFile,
		sourcePathPrefix,
		index,
	)
	if err != nil {
		return "", expressionPolicyGovernance{}, false, err
	}

	governance, err := expressionGovernance(expression, index, sourceFile)
	if err != nil {
		return "", expressionPolicyGovernance{}, false, err
	}

	enabled, err := boolOptionFromMap(expression, "enabled", true)
	if err != nil {
		return "", expressionPolicyGovernance{}, false,
			invalidExpressionEnabled(sourceFile, sourcePathPrefix, index)
	}

	if !enabled && governance.Protected {
		return "", expressionPolicyGovernance{}, false, protectedExpressionDisabled(
			sourceFile,
			sourcePathPrefix,
			index,
			policyID,
		)
	}

	return policyID, governance, enabled, nil
}

type expressionPolicyContentFields struct {
	policyID     string
	when         string
	message      string
	advice       string
	principleIDs []string
}

func expressionPolicyContent(
	expression map[string]any,
	index int,
	sourceFile string,
	sourcePathPrefix string,
	policyID string,
	principles map[string]Principle,
) (expressionPolicyContentFields, error) {
	when, err := expressionRequiredString(
		expression,
		"when",
		sourceFile,
		sourcePathPrefix,
		index,
	)
	if err != nil {
		return expressionPolicyContentFields{}, err
	}

	err = celexpr.Validate(policyID, when)
	if err != nil {
		return expressionPolicyContentFields{}, fmt.Errorf(
			"validate CEL policy %q: %w",
			policyID,
			err,
		)
	}

	principleIDs, err := requiredExpressionPrinciples(
		expression,
		sourceFile,
		sourcePathPrefix,
		index,
	)
	if err != nil {
		return expressionPolicyContentFields{}, err
	}

	err = validateExpressionPrinciples(sourceFile, policyID, principleIDs, principles)
	if err != nil {
		return expressionPolicyContentFields{}, err
	}

	message, err := expressionRequiredString(
		expression,
		"message",
		sourceFile,
		sourcePathPrefix,
		index,
	)
	if err != nil {
		return expressionPolicyContentFields{}, err
	}

	advice, err := expressionRequiredString(
		expression,
		"advice",
		sourceFile,
		sourcePathPrefix,
		index,
	)
	if err != nil {
		return expressionPolicyContentFields{}, err
	}

	return expressionPolicyContentFields{
		policyID:     policyID,
		when:         when,
		message:      message,
		advice:       advice,
		principleIDs: principleIDs,
	}, nil
}

func expressionPolicyPartsFromContent(
	expression map[string]any,
	config map[string]any,
	governance expressionPolicyGovernance,
	sourceFile string,
	sourcePathPrefix string,
	index int,
	content expressionPolicyContentFields,
) expressionPolicyParts {
	scope := stringOptionFromMap(expression, "scope", "command")
	severity := stringOptionFromMap(expression, "severity", "block")
	mode := stringOptionFromMap(expression, "mode", severity)
	dispatchScopes := stringSliceValue(
		firstPresentValue(expression, "lint_scopes", "dispatch_scopes"),
		defaultExpressionDispatchScopes(scope),
	)
	hookEvents := stringSliceValueAllowEmpty(
		expression["hook_events"],
		[]string{"PreToolUse"},
	)
	tools := stringSliceValue(expression["tools"], expressionHookTools(scope))
	commandPatterns := stringSliceValue(expression["command_patterns"], nil)
	pathPatterns := stringSliceValue(expression["path_patterns"], nil)

	return expressionPolicyParts{
		expression:      expression,
		config:          config,
		governance:      governance,
		policyID:        content.policyID,
		sourceFile:      sourceFile,
		sourcePath:      fmt.Sprintf("%s[%d]", sourcePathPrefix, index),
		severity:        severity,
		message:         content.message,
		advice:          content.advice,
		scope:           scope,
		mode:            mode,
		when:            content.when,
		principleIDs:    content.principleIDs,
		tools:           tools,
		commandPatterns: commandPatterns,
		dispatchScopes:  dispatchScopes,
		hookEvents:      hookEvents,
		pathPatterns:    pathPatterns,
	}
}

type expressionPolicyParts struct {
	expression      map[string]any
	config          map[string]any
	governance      expressionPolicyGovernance
	policyID        string
	sourceFile      string
	sourcePath      string
	severity        string
	message         string
	advice          string
	scope           string
	mode            string
	when            string
	principleIDs    []string
	tools           []string
	commandPatterns []string
	dispatchScopes  []string
	hookEvents      []string
	pathPatterns    []string
}

func buildExpressionPolicy(parts expressionPolicyParts) Policy {
	return Policy{
		ID:       parts.policyID,
		Category: "expression",
		Source: SourceRef{
			File: parts.sourceFile,
			Path: parts.sourcePath,
		},
		PrincipleIDs:    parts.principleIDs,
		DefaultSeverity: parts.severity,
		SupportedModes:  []string{"block", "warn", "record", "advise"},
		Message:         parts.message,
		Suggestion:      parts.advice,
		DefenseLayers:   expressionDefenseLayers(parts.policyID),
		AppliesTo: AppliesTo{
			Tools: parts.tools,
		},
		Evaluators: []Evaluator{{
			Kind:    "cel",
			Name:    "cel.expression",
			Options: expressionEvaluatorOptions(parts),
		}},
	}
}

func expressionEvaluatorOptions(parts expressionPolicyParts) map[string]any {
	options := map[string]any{
		"command_patterns": parts.commandPatterns,
		"dispatch_scopes":  parts.dispatchScopes,
		"hook_events":      parts.hookEvents,
		"mode":             parts.mode,
		"override":         parts.governance.Override,
		"override_reason":  parts.governance.OverrideReason,
		"path_patterns":    parts.pathPatterns,
		"protected_branches": stringSliceAt(
			parts.config,
			[]string{"filesystem", "protected_branch_write", "branches"},
			[]string{"main", "master"},
		),
		"protected_paths": expressionProtectedPaths(parts.config),
		"protected":       parts.governance.Protected,
		"python_version":  stringAt(parts.config, "style", "python_version"),
		"config_candidates": consumerOverrideCandidateNames(
			parts.config,
		),
		"required_ignore_paths": expressionRequiredIgnorePaths(
			parts.policyID,
			parts.config,
		),
		"scope":       parts.scope,
		"skill_id":    stringOptionFromMap(parts.expression, "skill_id", ""),
		"source_file": parts.sourceFile,
		"source_roots": stringSliceAt(
			parts.config,
			[]string{"python", "source_paths"},
			nil,
		),
		"tools":                 parts.tools,
		"when":                  parts.when,
		"allow_override":        parts.governance.AllowOverride,
		"allow_severity_weaken": parts.governance.AllowSeverityWeaken,
	}

	const lineLimitThresholdsOption = "line_limit_thresholds"

	if lineLimitThresholds, found := expressionMapOption(
		parts.expression,
		lineLimitThresholdsOption,
	); found {
		options["line_limit_thresholds"] = maps.Clone(lineLimitThresholds)
	}

	const coverageThresholdsOption = "coverage_thresholds"

	if coverageThresholds, found := expressionMapOption(
		parts.expression,
		coverageThresholdsOption,
	); found {
		options["coverage_thresholds"] = maps.Clone(coverageThresholds)
	}

	return options
}

func expressionMapOption(
	expression map[string]any,
	key string,
) (map[string]any, bool) {
	value, found := expression[key].(map[string]any)

	return value, found
}

func expressionProtectedPaths(config map[string]any) []string {
	return stringSliceAt(
		config,
		[]string{"filesystem", "protected_path", "paths"},
		[]string{
			".bandit.yml",
			".pylintrc",
			".sqlfluff",
			".yamllint.yml",
			"coding-ethos-hooks/coding-ethos-git-hook",
			"coding-ethos-hooks/bin/coding-ethos-agent-hooks",
			"coding-ethos-hooks/bin/coding-ethos-git",
			"coding-ethos-hooks/bin/coding-ethos-git-hook",
			"coding-ethos-hooks/bin/coding-ethos-hook",
			"coding-ethos-hooks/bin/coding-ethos-lint",
			"coding-ethos-hooks/bin/coding-ethos-policy",
			"coding-ethos-hooks/lefthook",
			"mypy.ini",
			"pyrightconfig.json",
			"ruff.toml",
			"tombi.toml",
		},
	)
}

func expressionRequiredString(
	expression map[string]any,
	field string,
	sourceFile string,
	sourcePathPrefix string,
	index int,
) (string, error) {
	value := strings.TrimSpace(fmt.Sprint(expression[field]))
	if value == "" || value == fmtNilValue {
		return "", missingExpressionField(
			field,
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	return value, nil
}

func requiredExpressionPrinciples(
	expression map[string]any,
	sourceFile string,
	sourcePathPrefix string,
	index int,
) ([]string, error) {
	principleIDs := expressionPrincipleIDs(expression)
	if len(principleIDs) == 0 {
		return nil, missingExpressionField(
			"principle_ids",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	return principleIDs, nil
}

func missingExpressionField(
	field string,
	sourceFile string,
	sourcePathPrefix string,
	index int,
) error {
	return apperror.Wrapf(
		apperror.StaticError("%s %s[%d].%s is required"),
		"%s %s[%d].%s is required",
		sourceFile,
		sourcePathPrefix,
		index,
		field,
	)
}

func invalidExpressionEnabled(
	sourceFile string,
	sourcePathPrefix string,
	index int,
) error {
	return apperror.Wrapf(
		apperror.StaticError("%s %s[%d].enabled must be a boolean"),
		"%s %s[%d].enabled must be a boolean",
		sourceFile,
		sourcePathPrefix,
		index,
	)
}

func protectedExpressionDisabled(
	sourceFile string,
	sourcePathPrefix string,
	index int,
	policyID string,
) error {
	return apperror.Wrapf(
		apperror.StaticError("%s %s[%d].id %q is protected and cannot be disabled"),
		"%s %s[%d].id %q is protected and cannot be disabled",
		sourceFile,
		sourcePathPrefix,
		index,
		policyID,
	)
}

func validateExpressionPrinciples(
	sourceFile string,
	policyID string,
	principleIDs []string,
	principles map[string]Principle,
) error {
	for _, principleID := range principleIDs {
		if _, found := principles[principleID]; !found {
			return apperror.Wrapf(
				apperror.StaticError(
					"%s policy expression %q references unknown principle %q",
				),
				"%s policy expression %q references unknown principle %q",
				sourceFile,
				policyID,
				principleID,
			)
		}
	}

	return nil
}

func expressionDefenseLayers(policyID string) DefenseLayers {
	if strings.HasPrefix(policyID, "git.") {
		return GitDefenseLayers("block", "wrapper", "block", "pre_commit", "git_state")
	}

	return CodeDefenseLayers()
}

func expressionRequiredIgnorePaths(policyID string, config map[string]any) []string {
	if policyID != "repo.required_ignores" {
		return nil
	}

	return stringSliceAt(
		config,
		[]string{"filesystem", "required_ignores", "paths"},
		[]string{
			".code-ethos/cache/",
			".coding-ethos/cache/",
			".coding-ethos/code-intel.db",
			".coding-ethos/hook-runs/",
			".coding-ethos/lint-runs/",
			".coding-ethos/prune-runs/",
			".coding-ethos/state/",
		},
	)
}

func consumerOverrideCandidateNames(config map[string]any) []string {
	return stringSliceAt(
		config,
		[]string{"bundle", "consumer_override_candidates"},
		[]string{
			"repo_config.yaml",
			"repo_config.yml",
			"code-ethos.repo.yaml",
			"code-ethos.repo.yml",
			"coding-ethos.repo.yaml",
			"coding-ethos.repo.yml",
			"code-ethos.pre-commit.yaml",
			"code-ethos.pre-commit.yml",
			"coding-ethos.pre-commit.yaml",
			"coding-ethos.pre-commit.yml",
		},
	)
}

func expressionGovernance(
	expression map[string]any,
	index int,
	sourceFile string,
) (expressionPolicyGovernance, error) {
	protected, err := boolOptionFromMap(expression, "protected", true)
	if err != nil {
		return expressionPolicyGovernance{}, apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].protected must be a boolean",
			),
			"%s policy.expressions[%d].protected must be a boolean",
			sourceFile,
			index,
		)
	}

	override, err := boolOptionFromMap(expression, "override", false)
	if err != nil {
		return expressionPolicyGovernance{}, apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].override must be a boolean",
			),
			"%s policy.expressions[%d].override must be a boolean",
			sourceFile,
			index,
		)
	}

	allowOverride, err := boolOptionFromMap(expression, "allow_override", false)
	if err != nil {
		return expressionPolicyGovernance{}, apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].allow_override must be a boolean",
			),
			"%s policy.expressions[%d].allow_override must be a boolean",
			sourceFile,
			index,
		)
	}

	allowSeverityWeaken, err := boolOptionFromMap(
		expression,
		"allow_severity_weaken",
		false,
	)
	if err != nil {
		return expressionPolicyGovernance{}, apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].allow_severity_weaken must be a boolean",
			),
			"%s policy.expressions[%d].allow_severity_weaken must be a boolean",
			sourceFile,
			index,
		)
	}

	return expressionPolicyGovernance{
		Override:            override,
		AllowOverride:       allowOverride,
		AllowSeverityWeaken: allowSeverityWeaken,
		Protected:           protected,
		OverrideReason:      stringOptionFromMap(expression, "override_reason", ""),
	}, nil
}

func validateExpressionPolicyOverride(
	sourceFile string,
	index int,
	replacement Policy,
	governance expressionPolicyGovernance,
	existing Policy,
) error {
	if !governance.Override {
		return apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].id %q conflicts with an existing policy",
			),
			"%s policy.expressions[%d].id %q conflicts with an existing policy",
			sourceFile,
			index,
			replacement.ID,
		)
	}

	if governance.OverrideReason == "" {
		return apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].override_reason is required for override of %q",
			),
			"%s policy.expressions[%d].override_reason is required for override of %q",
			sourceFile,
			index,
			replacement.ID,
		)
	}

	existingGovernance, found := expressionPolicyGovernanceFromPolicy(existing)
	if !found || !existingGovernance.AllowOverride {
		return apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].id %q cannot override protected policy from %s",
			),
			"%s policy.expressions[%d].id %q cannot override protected policy from %s",
			sourceFile,
			index,
			replacement.ID,
			existing.Source.File,
		)
	}

	if severityRank(replacement.DefaultSeverity) <
		severityRank(existing.DefaultSeverity) &&
		!existingGovernance.AllowSeverityWeaken {
		return apperror.Wrapf(
			apperror.StaticError(
				"%s policy.expressions[%d].id %q weakens severity from %q to %q",
			),
			"%s policy.expressions[%d].id %q weakens severity from %q to %q",
			sourceFile,
			index,
			replacement.ID,
			existing.DefaultSeverity,
			replacement.DefaultSeverity,
		)
	}

	return nil
}

func expressionPolicyGovernanceFromPolicy(
	policyDef Policy,
) (expressionPolicyGovernance, bool) {
	if policyDef.Category != "expression" {
		return expressionPolicyGovernance{}, false
	}

	for _, evaluator := range policyDef.Evaluators {
		if evaluator.Kind != "cel" || evaluator.Name != "cel.expression" {
			continue
		}

		return expressionPolicyGovernance{
			Override: boolValue(evaluator.Options["override"]),
			AllowOverride: boolValue(
				evaluator.Options["allow_override"],
			),
			AllowSeverityWeaken: boolValue(
				evaluator.Options["allow_severity_weaken"],
			),
			Protected: boolValue(evaluator.Options["protected"]),
			OverrideReason: stringOptionFromMap(
				evaluator.Options,
				"override_reason",
				"",
			),
		}, true
	}

	return expressionPolicyGovernance{}, false
}

func severityRank(severity string) int {
	switch severity {
	case "block":
		return severityRankBlock
	case "ask", "prepare":
		return severityRankAsk
	case "warn", "advise", "annotate":
		return severityRankAdvise
	case "record":
		return severityRankRecord
	default:
		return severityRankDefault
	}
}
