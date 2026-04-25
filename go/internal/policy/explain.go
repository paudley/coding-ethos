// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"fmt"
	"io"
	"strings"
)

func ExplainPolicy(writer io.Writer, bundle Bundle, policyID string) error {
	policy, ok := bundle.Policies[policyID]
	if !ok {
		return fmt.Errorf("unknown policy %q", policyID)
	}

	if _, err := fmt.Fprintf(writer, "# %s\n\n", policy.ID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Category: `%s`\n", policy.Category); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Severity: `%s`\n", policy.DefaultSeverity); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Source: `%s", policy.Source.File); err != nil {
		return err
	}
	if policy.Source.Path != "" {
		if _, err := fmt.Fprintf(writer, ":%s", policy.Source.Path); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer, "`"); err != nil {
		return err
	}
	if len(policy.PrincipleIDs) > 0 {
		if _, err := fmt.Fprintf(writer, "- Principles: `%s`\n", strings.Join(policy.PrincipleIDs, "`, `")); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(writer, "\n%s\n", policy.Message); err != nil {
		return err
	}
	if policy.Suggestion != "" {
		if _, err := fmt.Fprintf(writer, "\nSuggested fix: %s\n", policy.Suggestion); err != nil {
			return err
		}
	}
	return nil
}
