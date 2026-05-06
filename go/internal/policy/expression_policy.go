// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"fmt"
	"maps"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/celexpr"
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
	rawExpressions, ok := valueAt(config, "policy", "expressions")
	if !ok {
		return expressionPolicySource{}, false, nil
	}

	expressions, ok := rawExpressions.([]any)
	if !ok {
		return expressionPolicySource{}, false, fmt.Errorf(
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
	rawPrinciples, ok := ethos["principles"].([]any)
	if !ok {
		return nil
	}

	sources := []expressionPolicySource{}

	for index, rawPrinciple := range rawPrinciples {
		principle, ok := rawPrinciple.(map[string]any)
		if !ok {
			continue
		}

		rawExpressions, ok := valueAt(principle, "policy", "expressions")
		if !ok {
			continue
		}

		expressions, ok := rawExpressions.([]any)
		if !ok {
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

func principleExpressionDefaults(expressions []any, principleID string) []any {
	normalized := make([]any, 0, len(expressions))
	for _, rawExpression := range expressions {
		expression, ok := rawExpression.(map[string]any)
		if !ok {
			normalized = append(normalized, rawExpression)

			continue
		}

		copied := map[string]any{}
		maps.Copy(copied, expression)

		if _, ok := copied["principle_ids"]; !ok && principleID != "" {
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
		expression, ok := rawExpression.(map[string]any)
		if !ok {
			return fmt.Errorf(
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
			return fmt.Errorf(
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
	policyID := strings.TrimSpace(fmt.Sprint(expression["id"]))
	if policyID == "" || policyID == "<nil>" {
		return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
			"%s %s[%d].id is required",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	governance, err := expressionGovernance(expression, index, sourceFile)
	if err != nil {
		return Policy{}, false, expressionPolicyGovernance{}, err
	}

	enabled, err := boolOptionFromMap(expression, "enabled", true)
	if err != nil {
		return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
			"%s %s[%d].enabled must be a boolean",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	if !enabled {
		if governance.Protected {
			return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
				"%s %s[%d].id %q is protected and cannot be disabled",
				sourceFile,
				sourcePathPrefix,
				index,
				policyID,
			)
		}

		return Policy{}, false, governance, nil
	}

	when := strings.TrimSpace(fmt.Sprint(expression["when"]))
	if when == "" || when == "<nil>" {
		return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
			"%s %s[%d].when is required",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	if err := celexpr.Validate(policyID, when); err != nil {
		return Policy{}, false, expressionPolicyGovernance{}, err
	}

	principleIDs := expressionPrincipleIDs(expression)
	if len(principleIDs) == 0 {
		return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
			"%s %s[%d].principle_ids is required",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	for _, principleID := range principleIDs {
		if _, ok := principles[principleID]; !ok {
			return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
				"%s policy expression %q references unknown principle %q",
				sourceFile,
				policyID,
				principleID,
			)
		}
	}

	message := stringOptionFromMap(expression, "message", "")
	if message == "" {
		return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
			"%s %s[%d].message is required",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

	advice := stringOptionFromMap(expression, "advice", "")
	if advice == "" {
		return Policy{}, false, expressionPolicyGovernance{}, fmt.Errorf(
			"%s %s[%d].advice is required",
			sourceFile,
			sourcePathPrefix,
			index,
		)
	}

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

	return Policy{
		ID:       policyID,
		Category: "expression",
		Source: SourceRef{
			File: sourceFile,
			Path: fmt.Sprintf("%s[%d]", sourcePathPrefix, index),
		},
		PrincipleIDs:    principleIDs,
		DefaultSeverity: severity,
		SupportedModes:  []string{"block", "record", "advise"},
		Message:         message,
		Suggestion:      advice,
		DefenseLayers:   expressionDefenseLayers(policyID),
		AppliesTo: AppliesTo{
			Tools: tools,
		},
		Evaluators: []Evaluator{{
			Kind: "cel",
			Name: "cel.expression",
			Options: map[string]any{
				"command_patterns": commandPatterns,
				"dispatch_scopes":  dispatchScopes,
				"hook_events":      hookEvents,
				"mode":             mode,
				"override":         governance.Override,
				"override_reason":  governance.OverrideReason,
				"path_patterns":    pathPatterns,
				"protected_branches": stringSliceAt(
					config,
					[]string{"filesystem", "protected_branch_write", "branches"},
					[]string{"main", "master"},
				),
				"protected_paths": stringSliceAt(
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
				),
				"protected":             governance.Protected,
				"python_version":        stringAt(config, "style", "python_version"),
				"config_candidates":     consumerOverrideCandidateNames(config),
				"required_ignore_paths": expressionRequiredIgnorePaths(policyID, config),
				"scope":                 scope,
				"skill_id":              stringOptionFromMap(expression, "skill_id", ""),
				"source_file":           sourceFile,
				"source_roots": stringSliceAt(
					config,
					[]string{"python", "source_paths"},
					nil,
				),
				"tools":                 tools,
				"when":                  when,
				"allow_override":        governance.AllowOverride,
				"allow_severity_weaken": governance.AllowSeverityWeaken,
			},
		}},
	}, true, governance, nil
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
			".coding-ethos/",
			".coding-ethos/hook-runs/example/stdout.log",
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
		return expressionPolicyGovernance{}, fmt.Errorf(
			"%s policy.expressions[%d].protected must be a boolean",
			sourceFile,
			index,
		)
	}

	override, err := boolOptionFromMap(expression, "override", false)
	if err != nil {
		return expressionPolicyGovernance{}, fmt.Errorf(
			"%s policy.expressions[%d].override must be a boolean",
			sourceFile,
			index,
		)
	}

	allowOverride, err := boolOptionFromMap(expression, "allow_override", false)
	if err != nil {
		return expressionPolicyGovernance{}, fmt.Errorf(
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
		return expressionPolicyGovernance{}, fmt.Errorf(
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
		return fmt.Errorf(
			"%s policy.expressions[%d].id %q conflicts with an existing policy",
			sourceFile,
			index,
			replacement.ID,
		)
	}

	if governance.OverrideReason == "" {
		return fmt.Errorf(
			"%s policy.expressions[%d].override_reason is required for override of %q",
			sourceFile,
			index,
			replacement.ID,
		)
	}

	existingGovernance, ok := expressionPolicyGovernanceFromPolicy(existing)
	if !ok || !existingGovernance.AllowOverride {
		return fmt.Errorf(
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
		return fmt.Errorf(
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
			Protected:      boolValue(evaluator.Options["protected"]),
			OverrideReason: stringOptionFromMap(evaluator.Options, "override_reason", ""),
		}, true
	}

	return expressionPolicyGovernance{}, false
}

func severityRank(severity string) int {
	switch severity {
	case "block":
		return 50
	case "ask", "prepare":
		return 40
	case "advise", "annotate":
		return 30
	case "record":
		return 20
	default:
		return 0
	}
}
