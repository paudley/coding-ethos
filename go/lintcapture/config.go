// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcapture

import (
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const sourceRootCapacityMultiplier = 2

// proxyInterceptionDefaultMaxNormalizeBytes bounds how many bytes of an
// intercepted payload are buffered for structural normalization when the repo
// config does not set an explicit limit.
const proxyInterceptionDefaultMaxNormalizeBytes int64 = 8 * 1024 * 1024

// proxyInterceptionDefaultOnError is the fail-closed error policy applied when
// the repo config does not specify proxy.interception.on_error.
const proxyInterceptionDefaultOnError = "fail_closed"

type RuntimeConfig struct {
	Merged       map[string]any
	EthosRoot    string
	ConsumerRoot string
}

func LoadRuntimeConfig(ethosRoot, consumerRoot string) (RuntimeConfig, error) {
	return LoadRuntimeConfigWithRepoConfig(ethosRoot, consumerRoot, "")
}

func LoadRuntimeConfigWithRepoConfig(
	ethosRoot string,
	consumerRoot string,
	repoConfig string,
) (RuntimeConfig, error) {
	resolvedEthos, err := filepath.Abs(ethosRoot)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("resolve ethos root: %w", err)
	}

	resolvedConsumer, err := filepath.Abs(consumerRoot)
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("resolve consumer root: %w", err)
	}

	resolvedRepoConfig := ""
	if strings.TrimSpace(repoConfig) != "" {
		resolvedRepoConfig, err = filepath.Abs(repoConfig)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf("resolve repo config: %w", err)
		}
	}

	base, err := configdata.LoadYAMLMap(filepath.Join(resolvedEthos, "config.yaml"))
	if err != nil {
		return RuntimeConfig{}, fmt.Errorf("load base config: %w", err)
	}

	if resolvedRepoConfig != "" {
		override, err := configdata.LoadYAMLMap(resolvedRepoConfig)
		if err != nil {
			return RuntimeConfig{}, fmt.Errorf(
				"load repo config %s: %w",
				resolvedRepoConfig,
				err,
			)
		}

		return RuntimeConfig{
			EthosRoot:    filepath.Clean(resolvedEthos),
			ConsumerRoot: filepath.Clean(resolvedConsumer),
			Merged:       deepMergeMaps(base, override),
		}, nil
	}

	for _, name := range repoConfigCandidates(base) {
		override, err := configdata.LoadYAMLMap(filepath.Join(resolvedConsumer, name))
		if err == nil {
			return RuntimeConfig{
				EthosRoot:    filepath.Clean(resolvedEthos),
				ConsumerRoot: filepath.Clean(resolvedConsumer),
				Merged:       deepMergeMaps(base, override),
			}, nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return RuntimeConfig{}, fmt.Errorf(
				"load repo config candidate %s: %w",
				name,
				err,
			)
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

func (config RuntimeConfig) SandboxReadWritePaths() []string {
	return uniqueStrings(append(
		configValues(config.Merged, "sandbox", "read_write_paths"),
		configValues(config.Merged, "sandbox", "rw_paths")...,
	))
}

// SandboxNetworkTools returns managed tool names that the consumer explicitly
// allows to use networking in the runtime sandbox.
func (config RuntimeConfig) SandboxNetworkTools() []string {
	return uniqueStrings(configValues(config.Merged, "sandbox", "network_tools"))
}

// SandboxToolRequiresNetwork reports whether a managed tool has a consumer
// network opt-in in repo configuration.
func (config RuntimeConfig) SandboxToolRequiresNetwork(toolName string) bool {
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return false
	}

	return slices.Contains(config.SandboxNetworkTools(), toolName)
}

// ProxyInterceptionMode returns the configured agent-proxy HTTPS interception
// mode, defaulting to "off" when the proxy stanza is absent.
func (config RuntimeConfig) ProxyInterceptionMode() string {
	mode := proxyInterceptionString(config.Merged, "mode")
	if mode == "" {
		return "off"
	}

	return mode
}

// ProxyInterceptionCAApproval returns the operator-pinned CA fingerprint that
// gates HTTPS interception, or an empty string when none is configured.
func (config RuntimeConfig) ProxyInterceptionCAApproval() string {
	return proxyInterceptionString(config.Merged, "ca_approval")
}

// ProxyInterceptionAllowHosts returns the trimmed, lowercased host allow list
// that gates which CONNECT targets the interception proxy decrypts, or nil when
// none is configured.
func (config RuntimeConfig) ProxyInterceptionAllowHosts() []string {
	section := proxyInterceptionSection(config.Merged)
	hosts := configdata.StringList(section["allow_hosts"])

	normalized := make([]string, 0, len(hosts))
	for _, host := range hosts {
		text := strings.ToLower(strings.TrimSpace(host))
		if text != "" {
			normalized = append(normalized, text)
		}
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

// ProxyInterceptionMaxNormalizeBytes returns the configured byte bound for
// structural payload normalization, defaulting to 8 MiB when unset or
// non-positive.
func (config RuntimeConfig) ProxyInterceptionMaxNormalizeBytes() int64 {
	section := proxyInterceptionSection(config.Merged)

	configured := int64(configdata.IntAt(section, "max_normalize_bytes"))
	if configured <= 0 {
		return proxyInterceptionDefaultMaxNormalizeBytes
	}

	return configured
}

// ProxyInterceptionOnError returns the configured on-error policy for the
// interception proxy, defaulting to "fail_closed" when unset.
func (config RuntimeConfig) ProxyInterceptionOnError() string {
	section := proxyInterceptionSection(config.Merged)

	onError := configdata.StringAt(section, "on_error")
	if onError == "" {
		return proxyInterceptionDefaultOnError
	}

	return onError
}

func proxyInterceptionString(config map[string]any, key string) string {
	return configdata.StringAt(proxyInterceptionSection(config), key)
}

func proxyInterceptionSection(config map[string]any) map[string]any {
	return configdata.MapValue(
		configdata.GetPath(config, "proxy.interception", map[string]any{}),
	)
}

func parentRoots(values []string) []string {
	roots := make([]string, 0, len(values)*sourceRootCapacityMultiplier)
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

func configValues(config map[string]any, sectionName, key string) []string {
	section, ok := config[sectionName].(map[string]any)
	if !ok {
		return nil
	}

	return configdata.StringList(section[key])
}

func repoConfigCandidates(config map[string]any) []string {
	names := configdata.StringList(
		configdata.GetPath(config, "bundle.consumer_override_candidates", []any{}),
	)
	if len(names) > 0 {
		return names
	}

	return []string{
		"repo_config.yaml",
		"repo_config.yml",
		"coding-ethos.repo.yaml",
		"coding-ethos.repo.yml",
	}
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	unique := []string{}

	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}

		if _, ok := seen[text]; ok {
			continue
		}

		seen[text] = struct{}{}
		unique = append(unique, text)
	}

	return unique
}

func deepMergeMaps(base, override map[string]any) map[string]any {
	return deepMergeMapsAt(base, override, nil)
}

func deepMergeMapsAt(base, override map[string]any, path []string) map[string]any {
	merged := make(map[string]any, len(base)+len(override))
	maps.Copy(merged, base)

	for key, overrideValue := range override {
		keyPath := append(append([]string(nil), path...), key)
		if baseMap, ok := merged[key].(map[string]any); ok {
			if overrideMap, ok := overrideValue.(map[string]any); ok {
				merged[key] = deepMergeMapsAt(baseMap, overrideMap, keyPath)

				continue
			}
		}

		if shouldAppendStringList(keyPath) {
			if baseValues, ok := merged[key].([]any); ok {
				if overrideValues, ok := overrideValue.([]any); ok {
					merged[key] = append(
						append([]any(nil), baseValues...),
						overrideValues...)

					continue
				}
			}
		}

		merged[key] = overrideValue
	}

	return merged
}

func shouldAppendStringList(path []string) bool {
	if len(path) != 2 || path[0] != "sandbox" {
		return false
	}

	return path[1] == "read"+"_write"+"_paths" ||
		path[1] == "rw"+"_paths" ||
		path[1] == "network"+"_tools"
}
