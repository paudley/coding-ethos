// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package policy

import (
	"io"
	"sort"
)

func WriteSummary(writer io.Writer, bundle Bundle) error {
	err := writePolicyLine(writer, "# Policy Bundle Summary\n\n")
	if err != nil {
		return err
	}

	err = writePolicyLine(writer, "- Bundle: `%s`\n", bundle.BundleID)
	if err != nil {
		return err
	}

	err = writePolicyLine(writer, "- Version: `%d`\n", bundle.Version)
	if err != nil {
		return err
	}

	err = writePolicyLine(writer, "- Generated: `%s`\n", bundle.GeneratedAt)
	if err != nil {
		return err
	}

	err = writePolicyLine(writer, "- Principles: `%d`\n", len(bundle.Principles))
	if err != nil {
		return err
	}

	err = writePolicyLine(writer, "- Policies: `%d`\n\n", len(bundle.Policies))
	if err != nil {
		return err
	}

	err = writePrincipleSummary(writer, bundle)
	if err != nil {
		return err
	}

	return writePolicySummary(writer, bundle)
}

func writePrincipleSummary(writer io.Writer, bundle Bundle) error {
	err := writePolicyLine(writer, "## Principles\n\n")
	if err != nil {
		return err
	}

	ids := sortedKeys(bundle.Principles)
	for _, id := range ids {
		principle := bundle.Principles[id]

		err = writePolicyLine(writer, "- `%s`: %s\n", principle.ID, principle.Title)
		if err != nil {
			return err
		}
	}

	err = writePolicyLine(writer, "\n")
	if err != nil {
		return err
	}

	return nil
}

func writePolicySummary(writer io.Writer, bundle Bundle) error {
	err := writePolicyLine(writer, "## Policies\n\n")
	if err != nil {
		return err
	}

	ids := sortedKeys(bundle.Policies)
	for _, id := range ids {
		policy := bundle.Policies[id]

		err = writePolicyLine(
			writer,
			"- `%s` [%s/%s]: %s\n",
			policy.ID,
			policy.Category,
			policy.DefaultSeverity,
			policy.Message,
		)
		if err != nil {
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
