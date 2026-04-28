// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package evaluators

func stringSliceOption(
	options map[string]any,
	key string,
	defaults []string,
) []string {
	raw, exists := options[key]
	if !exists {
		return append([]string(nil), defaults...)
	}

	rawStringItems, isStringSlice := raw.([]string)
	if isStringSlice {
		items := make([]string, 0, len(rawStringItems))
		for _, item := range rawStringItems {
			if item != "" {
				items = append(items, item)
			}
		}

		if len(items) > 0 {
			return items
		}

		return append([]string(nil), defaults...)
	}

	rawItems, ok := raw.([]any)
	if !ok {
		return append([]string(nil), defaults...)
	}

	items := make([]string, 0, len(rawItems))
	for _, rawItem := range rawItems {
		item, ok := rawItem.(string)
		if ok && item != "" {
			items = append(items, item)
		}
	}

	if len(items) == 0 {
		return append([]string(nil), defaults...)
	}

	return items
}

func stringSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}

	return set
}
