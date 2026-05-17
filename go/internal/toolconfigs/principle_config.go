// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolconfigs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const toolConfigProvenanceKey = "tooling_provenance"

var (
	errInvalidPrincipleToolConfig = apperror.StaticError("invalid principle tool_config")
	errInvalidToolConfigItem      = apperror.StaticError("invalid tool_config item")
)

type principleToolConfigSource struct {
	config      configMap
	principleID string
	source      string
}

type toolConfigItem struct {
	value     string
	rationale string
}

type toolConfigBool struct {
	rationale string
	value     bool
}

func applyPrincipleToolConfig(
	ethosRoot string,
	repoRoot string,
	base configMap,
) (configMap, error) {
	sources, err := loadPrincipleToolConfigSources(ethosRoot, repoRoot)
	if err != nil {
		return nil, err
	}

	if len(sources) == 0 {
		return base, nil
	}

	merged := cloneMap(base)
	for _, source := range sources {
		err = applyPrincipleToolConfigSource(merged, source)
		if err != nil {
			return nil, err
		}
	}

	return merged, nil
}

func loadPrincipleToolConfigSources(
	ethosRoot string,
	repoRoot string,
) ([]principleToolConfigSource, error) {
	primary, primaryPrincipleIDs, err := loadPrimaryPrincipleToolConfigSources(
		filepath.Join(ethosRoot, "coding_ethos.yml"),
	)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}

		return nil, err
	}

	repo, err := loadRepoPrincipleToolConfigSources(
		filepath.Join(repoRoot, "repo_ethos.yml"),
		primaryPrincipleIDs,
	)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return primary, nil
		}

		return nil, err
	}

	return append(primary, repo...), nil
}

func loadPrimaryPrincipleToolConfigSources(
	path string,
) ([]principleToolConfigSource, map[string]struct{}, error) {
	payload, err := configdata.LoadYAMLMap(path)
	if err != nil {
		return nil, nil, fmt.Errorf("load primary principle tool config %s: %w", path, err)
	}

	principles := configdata.ListValue(payload["principles"])

	sources, err := principleToolConfigSourcesFromList(principles, filepath.Base(path))
	if err != nil {
		return nil, nil, err
	}

	return sources, principleIDsFromList(principles), nil
}

func loadRepoPrincipleToolConfigSources(
	path string,
	primaryPrincipleIDs map[string]struct{},
) ([]principleToolConfigSource, error) {
	payload, err := configdata.LoadYAMLMap(path)
	if err != nil {
		return nil, fmt.Errorf("load repo principle tool config %s: %w", path, err)
	}

	principles := configdata.MapValue(payload["principles"])
	if len(principles) == 0 {
		return nil, nil
	}

	sources := make([]principleToolConfigSource, 0)
	overrides := configdata.MapValue(principles["overrides"])

	overrideIDs := make([]string, 0, len(overrides))
	for principleID := range overrides {
		overrideIDs = append(overrideIDs, principleID)
	}

	slices.Sort(overrideIDs)

	var (
		toolConfig configMap
		found      bool
	)

	for _, principleID := range overrideIDs {
		rawOverride := overrides[principleID]
		override := configdata.MapValue(rawOverride)

		toolConfig, found, err = optionalToolConfigMap(
			override,
			"tool_config",
			filepath.Base(path)+" overrides."+principleID,
		)
		if err != nil {
			return nil, err
		}

		if !found || len(toolConfig) == 0 {
			continue
		}

		if _, exists := primaryPrincipleIDs[principleID]; !exists {
			return nil, apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s overrides.%s references unknown principle",
				filepath.Base(path),
				principleID,
			)
		}

		sources = append(sources, principleToolConfigSource{
			principleID: principleID,
			source:      filepath.Base(path),
			config:      toolConfig,
		})
	}

	additional, err := principleToolConfigSourcesFromList(
		configdata.ListValue(principles["additional"]),
		filepath.Base(path),
	)
	if err != nil {
		return nil, err
	}

	sources = append(sources, additional...)

	return sources, nil
}

func principleIDsFromList(values []any) map[string]struct{} {
	principleIDs := make(map[string]struct{}, len(values))
	for _, value := range values {
		principleID := configdata.StringAt(configdata.MapValue(value), "id")
		if principleID != "" {
			principleIDs[principleID] = struct{}{}
		}
	}

	return principleIDs
}

func principleToolConfigSourcesFromList(
	values []any,
	source string,
) ([]principleToolConfigSource, error) {
	sources := make([]principleToolConfigSource, 0, len(values))
	for index, value := range values {
		principle := configdata.MapValue(value)
		if len(principle) == 0 {
			return nil, apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s principles[%d] must be a mapping",
				source,
				index,
			)
		}

		toolConfig, found, err := optionalToolConfigMap(
			principle,
			"tool_config",
			fmt.Sprintf("%s principles[%d]", source, index),
		)
		if err != nil {
			return nil, err
		}

		if !found || len(toolConfig) == 0 {
			continue
		}

		principleID := configdata.StringAt(principle, "id")
		if principleID == "" {
			return nil, apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s principles[%d] with tool_config must declare id",
				source,
				index,
			)
		}

		sources = append(sources, principleToolConfigSource{
			principleID: principleID,
			source:      source,
			config:      toolConfig,
		})
	}

	return sources, nil
}

func applyPrincipleToolConfigSource(
	config configMap,
	source principleToolConfigSource,
) error {
	tools := make([]string, 0, len(source.config))
	for tool := range source.config {
		tools = append(tools, tool)
	}

	slices.Sort(tools)

	for _, tool := range tools {
		rawConfig := source.config[tool]
		toolConfig := configdata.MapValue(rawConfig)

		if len(toolConfig) == 0 {
			return apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s %s tool_config.%s must be a mapping",
				source.source,
				source.principleID,
				tool,
			)
		}

		switch tool {
		case "golangci_lint":
			err := applyGolangCIPrincipleToolConfig(config, source, toolConfig)
			if err != nil {
				return err
			}
		case "bandit":
			err := applyBanditPrincipleToolConfig(config, source, toolConfig)
			if err != nil {
				return err
			}
		default:
			return apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s %s tool_config.%s is not supported",
				source.source,
				source.principleID,
				tool,
			)
		}
	}

	return nil
}

func optionalToolConfigMap(
	config configMap,
	key string,
	context string,
) (configMap, bool, error) {
	rawValue, found := config[key]
	if !found {
		return nil, false, nil
	}

	toolConfig, ok := rawValue.(map[string]any)
	if !ok {
		return nil, true, apperror.Wrapf(
			errInvalidPrincipleToolConfig,
			"%s.%s must be a mapping",
			context,
			key,
		)
	}

	return toolConfig, true, nil
}

func applyGolangCIPrincipleToolConfig(
	config configMap,
	source principleToolConfigSource,
	toolConfig configMap,
) error {
	for key := range toolConfig {
		if key != "linters" {
			return apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s %s tool_config.golangci_lint.%s is not supported",
				source.source,
				source.principleID,
				key,
			)
		}
	}

	linters := configdata.MapValue(toolConfig["linters"])
	if len(linters) == 0 {
		return apperror.Wrapf(
			errInvalidPrincipleToolConfig,
			"%s %s tool_config.golangci_lint.linters must be a mapping",
			source.source,
			source.principleID,
		)
	}

	for field := range linters {
		if field != "enable" && field != "disable" {
			return apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s %s tool_config.golangci_lint.linters.%s is not supported",
				source.source,
				source.principleID,
				field,
			)
		}
	}

	for field, path := range map[string]string{
		"enable":  "tooling.golangci_lint.linters.enable",
		"disable": "tooling.golangci_lint.linters.disable",
	} {
		rawItems, ok := linters[field]
		if !ok {
			continue
		}

		items, err := toolConfigItems(rawItems, source, "golangci_lint.linters."+field)
		if err != nil {
			return err
		}

		appendToolConfigItems(config, path, "golangci_lint", "linters."+field, source, items)
	}

	return nil
}

func applyBanditPrincipleToolConfig(
	config configMap,
	source principleToolConfigSource,
	toolConfig configMap,
) error {
	for key, rawValue := range toolConfig {
		switch key {
		case "enabled":
			enabled, err := toolConfigEnabled(rawValue, source, "bandit.enabled")
			if err != nil {
				return err
			}

			setPath(config, "tooling.bandit.enabled", enabled.value)
			appendToolConfigProvenance(
				config,
				"bandit",
				"enabled",
				strconv.FormatBool(enabled.value),
				source,
				enabled.rationale,
			)
		case "exclude_dirs", "skips":
			items, err := toolConfigItems(rawValue, source, "bandit."+key)
			if err != nil {
				return err
			}

			appendToolConfigItems(config, "tooling.bandit."+key, "bandit", key, source, items)
		default:
			return apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s %s tool_config.bandit.%s is not supported",
				source.source,
				source.principleID,
				key,
			)
		}
	}

	return nil
}

func appendToolConfigItems(
	config configMap,
	path string,
	tool string,
	field string,
	source principleToolConfigSource,
	items []toolConfigItem,
) {
	values := stringList(getPath(config, path, []any{}))
	for _, item := range items {
		if !slices.Contains(values, item.value) {
			values = append(values, item.value)
		}

		appendToolConfigProvenance(config, tool, field, item.value, source, item.rationale)
	}

	setPath(config, path, stringsToAny(values))
}

func toolConfigItems(
	value any,
	source principleToolConfigSource,
	field string,
) ([]toolConfigItem, error) {
	values := configdata.ListValue(value)
	if values == nil {
		return nil, apperror.Wrapf(
			errInvalidPrincipleToolConfig,
			"%s %s tool_config.%s must be a list",
			source.source,
			source.principleID,
			field,
		)
	}

	items := make([]toolConfigItem, 0, len(values))
	for index, rawItem := range values {
		item, err := toolConfigItemValue(rawItem)
		if err != nil {
			return nil, fmt.Errorf(
				"%s %s tool_config.%s[%d]: %w",
				source.source,
				source.principleID,
				field,
				index,
				err,
			)
		}

		items = append(items, item)
	}

	return items, nil
}

func toolConfigItemValue(rawItem any) (toolConfigItem, error) {
	if itemMap, ok := rawItem.(map[string]any); ok {
		err := validateToolConfigItemMap(itemMap)
		if err != nil {
			return toolConfigItem{}, err
		}

		name := firstNonEmptyString(itemMap, "name", "id", "rule", "path")
		if name == "" {
			return toolConfigItem{}, apperror.Wrapf(
				errInvalidToolConfigItem,
				"mapping item must declare name, id, rule, or path",
			)
		}

		return toolConfigItem{
			value:     name,
			rationale: configdata.StringAt(itemMap, "rationale"),
		}, nil
	}

	if rawItem == nil {
		return toolConfigItem{}, apperror.Wrapf(
			errInvalidToolConfigItem,
			"item must not be null",
		)
	}

	value := strings.TrimSpace(fmt.Sprint(rawItem))
	if value == "" {
		return toolConfigItem{}, apperror.Wrapf(
			errInvalidToolConfigItem,
			"item must not be empty",
		)
	}

	return toolConfigItem{value: value}, nil
}

func validateToolConfigItemMap(itemMap configMap) error {
	identityCount := 0

	for key := range itemMap {
		switch key {
		case "name", "id", "rule", "path":
			if configdata.StringAt(itemMap, key) != "" {
				identityCount++
			}
		case "rationale":
		default:
			return apperror.Wrapf(
				errInvalidToolConfigItem,
				"mapping item key %q is not supported",
				key,
			)
		}
	}

	if identityCount > 1 {
		return apperror.Wrapf(
			errInvalidToolConfigItem,
			"mapping item must declare only one of name, id, rule, or path",
		)
	}

	return nil
}

func toolConfigEnabled(
	value any,
	source principleToolConfigSource,
	field string,
) (toolConfigBool, error) {
	if itemMap := configdata.MapValue(value); len(itemMap) > 0 {
		for key := range itemMap {
			if key != "value" && key != "rationale" {
				return toolConfigBool{}, apperror.Wrapf(
					errInvalidPrincipleToolConfig,
					"%s %s tool_config.%s.%s is not supported",
					source.source,
					source.principleID,
					field,
					key,
				)
			}
		}

		boolValue, ok := itemMap["value"].(bool)
		if !ok {
			return toolConfigBool{}, apperror.Wrapf(
				errInvalidPrincipleToolConfig,
				"%s %s tool_config.%s.value must be a boolean",
				source.source,
				source.principleID,
				field,
			)
		}

		return toolConfigBool{
			value:     boolValue,
			rationale: configdata.StringAt(itemMap, "rationale"),
		}, nil
	}

	boolValue, ok := value.(bool)
	if !ok {
		return toolConfigBool{}, apperror.Wrapf(
			errInvalidPrincipleToolConfig,
			"%s %s tool_config.%s must be a boolean or mapping",
			source.source,
			source.principleID,
			field,
		)
	}

	return toolConfigBool{value: boolValue}, nil
}

func appendToolConfigProvenance(
	config configMap,
	tool string,
	field string,
	value string,
	source principleToolConfigSource,
	rationale string,
) {
	provenance := mapValueOrCreate(config, toolConfigProvenanceKey)
	toolProvenance := mapValueOrCreate(provenance, tool)

	entry := configMap{
		"field":        field,
		"value":        value,
		"principle_id": source.principleID,
		"source":       source.source,
	}
	if strings.TrimSpace(rationale) != "" {
		entry["rationale"] = strings.TrimSpace(rationale)
	}

	existing := configdata.ListValue(toolProvenance[field])
	toolProvenance[field] = append(existing, entry)
}

func renderToolConfigProvenance(config configMap, tool string) string {
	provenance := configdata.MapValue(
		getPath(config, toolConfigProvenanceKey+"."+tool, nil),
	)
	if len(provenance) == 0 {
		return ""
	}

	fields := make([]string, 0, len(provenance))
	for field := range provenance {
		fields = append(fields, field)
	}

	slices.Sort(fields)

	var builder strings.Builder
	builder.WriteString("# Principle-derived tool config:\n")

	for _, field := range fields {
		for _, rawEntry := range configdata.ListValue(provenance[field]) {
			entry := configdata.MapValue(rawEntry)
			if len(entry) == 0 {
				continue
			}

			line := "- " + field + " " +
				configdata.StringAt(entry, "value") + " from " +
				configdata.StringAt(entry, "principle_id")
			if source := configdata.StringAt(entry, "source"); source != "" {
				line += " (" + source + ")"
			}

			if rationale := configdata.StringAt(entry, "rationale"); rationale != "" {
				line += ": " + rationale
			}

			writeWrappedConfigComment(&builder, line)
		}
	}

	builder.WriteString("\n")

	return builder.String()
}

func pruneToolConfigProvenance(config configMap) configMap {
	provenance := configdata.MapValue(config[toolConfigProvenanceKey])
	if len(provenance) == 0 {
		return config
	}

	for tool, rawToolProvenance := range provenance {
		toolProvenance := configdata.MapValue(rawToolProvenance)
		for field, rawEntries := range toolProvenance {
			kept := make([]any, 0, len(configdata.ListValue(rawEntries)))
			for _, rawEntry := range configdata.ListValue(rawEntries) {
				entry := configdata.MapValue(rawEntry)
				if toolConfigProvenanceStillApplies(config, tool, field, entry) {
					kept = append(kept, rawEntry)
				}
			}

			if len(kept) == 0 {
				delete(toolProvenance, field)
			} else {
				toolProvenance[field] = kept
			}
		}

		if len(toolProvenance) == 0 {
			delete(provenance, tool)
		}
	}

	if len(provenance) == 0 {
		delete(config, toolConfigProvenanceKey)
	}

	return config
}

func toolConfigProvenanceStillApplies(
	config configMap,
	tool string,
	field string,
	entry configMap,
) bool {
	if len(entry) == 0 {
		return false
	}

	value := configdata.StringAt(entry, "value")

	if field == "enabled" {
		configured, ok := getPath(config, "tooling."+tool+".enabled", nil).(bool)

		return ok && strconv.FormatBool(configured) == value
	}

	return slices.Contains(
		stringList(getPath(config, "tooling."+tool+"."+field, []any{})),
		value,
	)
}

func mapValueOrCreate(config configMap, key string) configMap {
	value := configdata.MapValue(config[key])
	if value == nil {
		value = configMap{}
		config[key] = value
	}

	return value
}

func setPath(config configMap, path string, value any) {
	current := config
	segments := strings.Split(path, ".")

	for _, segment := range segments[:len(segments)-1] {
		next := configdata.MapValue(current[segment])
		if next == nil {
			next = configMap{}
			current[segment] = next
		}

		current = next
	}

	current[segments[len(segments)-1]] = value
}

func stringsToAny(values []string) []any {
	items := make([]any, 0, len(values))
	for _, value := range values {
		items = append(items, value)
	}

	return items
}

func writeWrappedConfigComment(builder *strings.Builder, text string) {
	const maxCommentWidth = 84

	remaining := strings.Join(strings.Fields(text), " ")
	for remaining != "" {
		if runeCount(remaining) <= maxCommentWidth {
			builder.WriteString("# ")
			builder.WriteString(remaining)
			builder.WriteString("\n")

			return
		}

		splitAt := configCommentSplitIndex(remaining, maxCommentWidth)

		builder.WriteString("# ")
		builder.WriteString(strings.TrimSpace(remaining[:splitAt]))
		builder.WriteString("\n")

		remaining = strings.TrimSpace(remaining[splitAt:])
	}
}

func configCommentSplitIndex(text string, maxRunes int) int {
	lastSpace := -1
	count := 0

	for index, char := range text {
		if count == maxRunes {
			if lastSpace > 0 {
				return lastSpace
			}

			return index
		}

		if char == ' ' {
			lastSpace = index
		}

		count++
	}

	return len(text)
}

func runeCount(text string) int {
	count := 0
	for range text {
		count++
	}

	return count
}

func firstNonEmptyString(values configMap, keys ...string) string {
	for _, key := range keys {
		value := configdata.StringAt(values, key)
		if value != "" {
			return value
		}
	}

	return ""
}
