// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package policy

import (
	"fmt"
	"io"
	"sort"
)

func WriteSummary(writer io.Writer, bundle Bundle) error {
	if _, err := fmt.Fprintf(writer, "# Policy Bundle Summary\n\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Bundle: `%s`\n", bundle.BundleID); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Version: `%d`\n", bundle.Version); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Generated: `%s`\n", bundle.GeneratedAt); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Principles: `%d`\n", len(bundle.Principles)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(writer, "- Policies: `%d`\n\n", len(bundle.Policies)); err != nil {
		return err
	}

	if err := writePrincipleSummary(writer, bundle); err != nil {
		return err
	}
	return writePolicySummary(writer, bundle)
}

func writePrincipleSummary(writer io.Writer, bundle Bundle) error {
	if _, err := fmt.Fprintf(writer, "## Principles\n\n"); err != nil {
		return err
	}
	ids := sortedKeys(bundle.Principles)
	for _, id := range ids {
		principle := bundle.Principles[id]
		if _, err := fmt.Fprintf(writer, "- `%s`: %s\n", principle.ID, principle.Title); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(writer); err != nil {
		return err
	}
	return nil
}

func writePolicySummary(writer io.Writer, bundle Bundle) error {
	if _, err := fmt.Fprintf(writer, "## Policies\n\n"); err != nil {
		return err
	}
	ids := sortedKeys(bundle.Policies)
	for _, id := range ids {
		policy := bundle.Policies[id]
		if _, err := fmt.Fprintf(
			writer,
			"- `%s` [%s/%s]: %s\n",
			policy.ID,
			policy.Category,
			policy.DefaultSeverity,
			policy.Message,
		); err != nil {
			return err
		}
	}
	return nil
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
