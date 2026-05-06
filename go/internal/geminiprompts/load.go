// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package geminiprompts

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const principleQuickRefLimit = 3

func loadInputs(primaryPath, repoEthosPath string) ([]principle, repoData, error) {
	primary, err := configdata.LoadYAMLMap(primaryPath)
	if err != nil {
		return nil, repoData{}, fmt.Errorf("load primary ethos %s: %w", primaryPath, err)
	}

	overlay, found, err := loadRepoEthosOverlay(repoEthosPath)
	if err != nil {
		return nil, repoData{}, err
	}

	if found {
		primary = mergeMaps(primary, overlay)
	}

	return loadPrinciples(primary), loadRepo(primary), nil
}

func loadRepoEthosOverlay(path string) (configdata.Map, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, nil
	}

	cleanPath := filepath.Clean(path)

	_, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return nil, false, nil
	}

	if err != nil {
		return nil, false, fmt.Errorf("stat repo ethos %s: %w", path, err)
	}

	overlay, err := configdata.LoadYAMLMap(path)
	if err != nil {
		return nil, false, fmt.Errorf("load repo ethos %s: %w", path, err)
	}

	return overlay, true, nil
}

func mergeMaps(base, overlay configdata.Map) configdata.Map {
	merged := make(configdata.Map, len(base)+len(overlay))
	maps.Copy(merged, base)

	for key, value := range overlay {
		if key == "principles" {
			merged[key] = mergePrincipleLists(
				configdata.ListValue(merged[key]),
				configdata.ListValue(value),
			)

			continue
		}

		baseMap, baseOK := merged[key].(configdata.Map)

		overlayMap, overlayOK := value.(configdata.Map)
		if baseOK && overlayOK {
			merged[key] = mergeMaps(baseMap, overlayMap)

			continue
		}

		merged[key] = value
	}

	return merged
}

func mergePrincipleLists(base, overlay []any) []any {
	merged := append([]any(nil), base...)
	indexes := map[string]int{}

	for index, item := range merged {
		principleID := configdata.StringAt(configdata.MapValue(item), "id")
		if principleID != "" {
			indexes[principleID] = index
		}
	}

	for _, item := range overlay {
		itemMap := configdata.MapValue(item)

		principleID := configdata.StringAt(itemMap, "id")
		if principleID == "" {
			merged = append(merged, item)

			continue
		}

		index, ok := indexes[principleID]
		if !ok {
			indexes[principleID] = len(merged)
			merged = append(merged, item)

			continue
		}

		if baseMap := configdata.MapValue(merged[index]); len(baseMap) > 0 {
			merged[index] = mergeMaps(baseMap, itemMap)
		}
	}

	return merged
}

func loadPrinciples(payload configdata.Map) []principle {
	items := configdata.ListValue(payload["principles"])

	principles := make([]principle, 0, len(items))
	for _, item := range items {
		mapping := configdata.MapValue(item)
		principleID := configdata.StringAt(mapping, "id")

		title := configdata.StringAt(mapping, "title")
		if principleID == "" || title == "" {
			continue
		}

		principles = append(principles, principle{
			ID:        principleID,
			Order:     configdata.IntAt(mapping, "order"),
			Title:     title,
			Summary:   configdata.StringAt(mapping, "summary"),
			Directive: configdata.StringAt(mapping, "directive"),
			QuickRef: firstStrings(
				configdata.StringList(mapping["quick_ref"]),
				principleQuickRefLimit,
			),
			AgentHints: stringMap(mapping["agent_hints"]),
		})
	}

	sort.SliceStable(principles, func(left, right int) bool {
		return principles[left].Order < principles[right].Order
	})

	return principles
}

func loadRepo(payload configdata.Map) repoData {
	repo := configdata.MapValue(payload["repo"])
	metadata := configdata.MapValue(payload["metadata"])
	agents := configdata.MapValue(payload["agents"])
	geminiAgent := configdata.MapValue(agents["gemini"])
	agentNotes := configdata.MapValue(payload["agent_notes"])

	return repoData{
		Name: firstNonEmpty(
			configdata.StringAt(repo, "name"),
			configdata.StringAt(metadata, "title"),
		),
		Overview: firstNonEmpty(
			configdata.StringAt(repo, "overview"),
			configdata.StringAt(metadata, "overview"),
		),
		Commands: repoCommands(configdata.MapValue(repo["commands"])),
		Paths:    repoPaths(configdata.MapValue(repo["paths"])),
		Notes:    configdata.StringList(repo["notes"]),
		GeminiNotes: dedupeStrings(
			append(
				configdata.StringList(geminiAgent["notes"]),
				configdata.StringList(agentNotes["gemini"])...),
		),
	}
}

func repoCommands(values configdata.Map) []repoCommand {
	keys := sortedMapKeys(values)

	commands := make([]repoCommand, 0, len(keys))
	for _, key := range keys {
		examples := configdata.StringList(values[key])
		if len(examples) > 0 {
			commands = append(commands, repoCommand{Name: key, Examples: examples})
		}
	}

	return commands
}

func repoPaths(values configdata.Map) []repoPath {
	keys := sortedMapKeys(values)

	paths := make([]repoPath, 0, len(keys))
	for _, key := range keys {
		value := strings.TrimSpace(fmt.Sprint(values[key]))
		if value != "" {
			paths = append(paths, repoPath{Name: key, Path: value})
		}
	}

	return paths
}

func stringMap(value any) map[string]string {
	values := configdata.MapValue(value)
	if len(values) == 0 {
		return nil
	}

	result := make(map[string]string, len(values))
	for key, item := range values {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			result[key] = text
		}
	}

	return result
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}

	return values[:limit]
}

func dedupeStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := []string{}

	for _, value := range values {
		text := strings.TrimSpace(value)
		if text == "" {
			continue
		}

		if _, ok := seen[text]; ok {
			continue
		}

		seen[text] = struct{}{}
		result = append(result, text)
	}

	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}

func sortedMapKeys(values configdata.Map) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
