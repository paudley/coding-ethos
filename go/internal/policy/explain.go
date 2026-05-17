// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"fmt"
	"io"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/celexpr"
)

var errUnknownPolicy = apperror.StaticError("unknown policy")

const policyExplanationParts = 7

func ExplainPolicy(writer io.Writer, bundle Bundle, policyID string) error {
	policy, found := bundle.Policies[policyID]
	if !found {
		return fmt.Errorf("%w %q", errUnknownPolicy, policyID)
	}

	output := policyExplanation(bundle, policy)

	_, err := io.WriteString(writer, output)
	if err != nil {
		return fmt.Errorf("write policy output: %w", err)
	}

	return nil
}

func policyExplanation(bundle Bundle, policy Policy) string {
	parts := make([]string, 0, policyExplanationParts)
	parts = append(parts,
		fmt.Sprintf("# %s\n\n", policy.ID),
		fmt.Sprintf("- Category: `%s`\n", policy.Category),
		fmt.Sprintf("- Severity: `%s`\n", policy.DefaultSeverity),
		fmt.Sprintf("- Source: `%s%s`\n", policy.Source.File, sourcePath(policy)),
	)

	parts = append(parts, principleExplanation(policy.PrincipleIDs))
	parts = append(parts, expressionExplanation(bundle, policy))
	parts = append(parts, fmt.Sprintf("\n%s\n", policy.Message))
	parts = append(parts, suggestionExplanation(policy.Suggestion))

	return strings.Join(parts, "")
}

func expressionExplanation(bundle Bundle, policy Policy) string {
	evaluator, found := expressionEvaluator(policy)
	if !found {
		return ""
	}

	parts := []string{
		"\n## CEL Expression\n\n",
		fmt.Sprintf(
			"```cel\n%s\n```\n",
			strings.TrimSpace(explainStringOption(evaluator.Options, "when")),
		),
		fmt.Sprintf(
			"\n- Scope: `%s`\n",
			explainStringOption(evaluator.Options, "scope"),
		),
		fmt.Sprintf(
			"- Dispatch scopes: `%s`\n",
			strings.Join(
				explainStringSliceOption(evaluator.Options, "dispatch_scopes"),
				"`, `",
			),
		),
	}
	if skillID := explainStringOption(evaluator.Options, "skill_id"); skillID != "" {
		parts = append(parts, fmt.Sprintf("- Skill: `%s`", skillID))
		if skill, found := bundle.Skills[skillID]; found && skill.ShortHint != "" {
			parts = append(parts, " - "+skill.ShortHint)
		}

		parts = append(parts, "\n")
	}

	parts = append(parts, "\nEvidence fields:\n")
	for _, item := range []string{
		"argv", "command", "files", "scope", "when", "skill_id",
	} {
		parts = append(parts, fmt.Sprintf("- `%s`\n", item))
	}

	parts = append(parts, "\nInput schema:\n")
	for _, item := range celexpr.InputSchema() {
		parts = append(parts, fmt.Sprintf("- `%s`\n", item))
	}

	parts = append(parts, "\nReviewed helpers:\n")
	for _, item := range celexpr.HelperSchema() {
		parts = append(parts, fmt.Sprintf("- `%s`\n", item))
	}

	return strings.Join(parts, "")
}

func expressionEvaluator(policy Policy) (Evaluator, bool) {
	for _, evaluator := range policy.Evaluators {
		if evaluator.Kind == "cel" && evaluator.Name == "cel.expression" {
			return evaluator, true
		}
	}

	return Evaluator{}, false
}

func explainStringOption(options map[string]any, key string) string {
	raw, found := options[key].(string)
	if !found {
		return ""
	}

	return strings.TrimSpace(raw)
}

func explainStringSliceOption(options map[string]any, key string) []string {
	rawStrings, found := options[key].([]string)
	if found {
		return append([]string(nil), rawStrings...)
	}

	rawItems, found := options[key].([]any)
	if !found {
		return nil
	}

	items := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, found := rawItem.(string)
		if found && item != "" {
			items = append(items, item)
		}
	}

	return items
}

func sourcePath(policy Policy) string {
	if policy.Source.Path == "" {
		return ""
	}

	return ":" + policy.Source.Path
}

func principleExplanation(principleIDs []string) string {
	if len(principleIDs) == 0 {
		return ""
	}

	return fmt.Sprintf("- Principles: `%s`\n", strings.Join(principleIDs, "`, `"))
}

func suggestionExplanation(suggestion string) string {
	if suggestion == "" {
		return ""
	}

	return fmt.Sprintf("\nSuggested fix: %s\n", suggestion)
}

func writePolicyLine(writer io.Writer, format string, args ...any) error {
	_, err := fmt.Fprintf(writer, format, args...)
	if err != nil {
		return fmt.Errorf("write policy output: %w", err)
	}

	return nil
}
