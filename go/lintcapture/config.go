// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

type RuntimeConfig struct {
	EthosRoot    string
	ConsumerRoot string
	Merged       map[string]any
}

func LoadRuntimeConfig(ethosRoot string, consumerRoot string) (RuntimeConfig, error) {
	resolvedEthos, err := filepath.Abs(ethosRoot)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("resolve ethos root: %w", err)
	}
	resolvedConsumer, err := filepath.Abs(consumerRoot)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("resolve consumer root: %w", err)
	}

	base, err := loadYAMLMap(filepath.Join(resolvedEthos, "config.yaml"))
	if err != nil {
		return RuntimeConfig{}, err
	}

	for _, name := range repoConfigCandidates(base) {
		override, err := loadYAMLMap(filepath.Join(resolvedConsumer, name))
		if err == nil {
			return RuntimeConfig{
				EthosRoot:    filepath.Clean(resolvedEthos),
				ConsumerRoot: filepath.Clean(resolvedConsumer),
				Merged:       deepMergeMaps(base, override),
			}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return RuntimeConfig{}, err
		}
	}

	return RuntimeConfig{
		EthosRoot:    filepath.Clean(resolvedEthos),
		ConsumerRoot: filepath.Clean(resolvedConsumer),
		Merged:       base,
	}, nil
}

func (config RuntimeConfig) LintSourceRoots() ([]string, error) {
	values := append(
		configValues(config.Merged, "python", "extra_paths"),
		parentRoots(configValues(config.Merged, "python", "source_paths"))...,
	)

	return containedSourceRoots(config.ConsumerRoot, values)
}

func parentRoots(values []string) []string {
	roots := make([]string, 0, len(values)*2)
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}
		if filepath.IsAbs(filepath.FromSlash(text)) {
			roots = append(roots, text)

			continue
		}
		text = strings.Trim(text, "/")
		parent := filepath.ToSlash(filepath.Dir(filepath.FromSlash(text)))
		if parent != "." {
			roots = append(roots, parent)
		}
		roots = append(roots, text)
	}

	return roots
}

func configValues(config map[string]any, sectionName string, key string) []string {
	section, ok := config[sectionName].(map[string]any)
	if !ok {
		return nil
	}
	rawValues, ok := section[key].([]any)
	if !ok {
		return nil
	}

	values := make([]string, 0, len(rawValues))
	for _, value := range rawValues {
		text := strings.TrimSpace(fmt.Sprint(value))
		if text != "" {
			values = append(values, text)
		}
	}

	return values
}

func repoConfigCandidates(config map[string]any) []string {
	names := []string{}
	if bundle, ok := config["bundle"].(map[string]any); ok {
		if raw, ok := bundle["consumer_override_candidates"].([]any); ok {
			for _, item := range raw {
				name := strings.TrimSpace(fmt.Sprint(item))
				if name != "" {
					names = append(names, name)
				}
			}
		}
	}
	if len(names) > 0 {
		return names
	}

	return []string{
		"repo_config.yaml",
		"repo_config.yml",
		"code-ethos.repo.yaml",
		"code-ethos.repo.yml",
		"coding-ethos.repo.yaml",
		"coding-ethos.repo.yml",
	}
}

func loadYAMLMap(path string) (map[string]any, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("read yaml %s: %w", path, err)
	}
	var decoded map[string]any
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		return nil, fmt.Errorf("parse yaml %s: %w", path, err)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}

	return decoded, nil
}

func deepMergeMaps(base map[string]any, override map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(override))
	for key, value := range base {
		merged[key] = value
	}
	for key, overrideValue := range override {
		if baseMap, ok := merged[key].(map[string]any); ok {
			if overrideMap, ok := overrideValue.(map[string]any); ok {
				merged[key] = deepMergeMaps(baseMap, overrideMap)
				continue
			}
		}
		merged[key] = overrideValue
	}

	return merged
}
