// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"errors"
	"fmt"
	"io"
	"strings"
)

var errUnknownPolicy = errors.New("unknown policy")

const policyExplanationParts = 7

func ExplainPolicy(writer io.Writer, bundle Bundle, policyID string) error {
	policy, ok := bundle.Policies[policyID]
	if !ok {
		return fmt.Errorf("%w %q", errUnknownPolicy, policyID)
	}

	output := policyExplanation(policy)

	_, err := io.WriteString(writer, output)
	if err != nil {
		return fmt.Errorf("write policy output: %w", err)
	}

	return nil
}

func policyExplanation(policy Policy) string {
	parts := make([]string, 0, policyExplanationParts)
	parts = append(parts,
		fmt.Sprintf("# %s\n\n", policy.ID),
		fmt.Sprintf("- Category: `%s`\n", policy.Category),
		fmt.Sprintf("- Severity: `%s`\n", policy.DefaultSeverity),
		fmt.Sprintf("- Source: `%s%s`\n", policy.Source.File, sourcePath(policy)),
	)

	parts = append(parts, principleExplanation(policy.PrincipleIDs))
	parts = append(parts, fmt.Sprintf("\n%s\n", policy.Message))
	parts = append(parts, suggestionExplanation(policy.Suggestion))

	return strings.Join(parts, "")
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
