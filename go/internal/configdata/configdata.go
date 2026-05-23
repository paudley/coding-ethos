// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package configdata

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
	"go.yaml.in/yaml/v3"
)

type Map = map[string]any

// RepoConfigCandidates returns the repo-local config filenames recognized by
// runtime features that read consumer repository overrides.
func RepoConfigCandidates() []string {
	return []string{
		"repo_config.yaml",
		"repo_config.yml",
		"code-ethos.repo.yaml",
		"code-ethos.repo.yml",
		"coding-ethos.repo.yaml",
		"coding-ethos.repo.yml",
	}
}

func LoadYAMLMap(path string) (Map, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read yaml %s: %w", path, err)
	}

	var decoded map[string]any

	err = yaml.Unmarshal(data, &decoded)
	if err != nil {
		return nil, fmt.Errorf("parse yaml %s: %w", path, err)
	}

	if decoded == nil {
		decoded = map[string]any{}
	}

	return decoded, nil
}

func LoadTOMLMap(path string) (Map, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read toml %s: %w", path, err)
	}

	var decoded map[string]any

	err = toml.Unmarshal(data, &decoded)
	if err != nil {
		return nil, fmt.Errorf("parse toml %s: %w", path, err)
	}

	if decoded == nil {
		decoded = map[string]any{}
	}

	return decoded, nil
}

func DeepMerge(base, override Map) Map {
	merged := make(Map, len(base)+len(override))
	maps.Copy(merged, base)

	for key, overrideValue := range override {
		baseMap, baseOK := merged[key].(map[string]any)

		overrideMap, overrideOK := overrideValue.(map[string]any)
		if baseOK && overrideOK {
			merged[key] = DeepMerge(baseMap, overrideMap)

			continue
		}

		merged[key] = overrideValue
	}

	return merged
}

func GetPath(config Map, path string, fallback any) any {
	var current any = config
	for segment := range strings.SplitSeq(path, ".") {
		mapping, isMap := current.(map[string]any)
		if !isMap {
			return fallback
		}

		value, exists := mapping[segment]
		if !exists {
			return fallback
		}

		current = value
	}

	return current
}

func StringList(value any) []string {
	if value == nil {
		return nil
	}

	if values, ok := value.([]any); ok {
		items := make([]string, 0, len(values))
		for _, item := range values {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				items = append(items, text)
			}
		}

		return items
	}

	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return nil
	}

	return []string{text}
}

func StringAt(values Map, key string) string {
	if values == nil {
		return ""
	}

	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}

	return strings.TrimSpace(fmt.Sprint(value))
}

func IntAt(values Map, key string) int {
	switch value := values[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(value))
		if err == nil {
			return parsed
		}
	}

	return 0
}

func MapValue(value any) Map {
	if mapping, ok := value.(map[string]any); ok {
		return mapping
	}

	return nil
}

func ListValue(value any) []any {
	if values, ok := value.([]any); ok {
		return values
	}

	return nil
}
