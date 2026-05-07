// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml/v2"
)

func normalizeStringList(value any) []string {
	switch typed := value.(type) {
	case nil:
		return []string{}
	case []string:
		return append([]string{}, typed...)
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if item != nil {
				items = append(items, fmt.Sprint(item))
			}
		}

		return items
	case map[string]any:
		return []string{fmt.Sprint(typed)}
	default:
		return []string{fmt.Sprint(value)}
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}

	return nil
}

func pyprojectMap(value any) map[string]any {
	if typed, ok := value.(map[string]any); ok {
		return typed
	}

	return nil
}

func loadPyprojectConfig(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("unable to read file: %w", err)
	}

	var config map[string]any

	err = toml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("invalid TOML: %w", err)
	}

	return config, nil
}
