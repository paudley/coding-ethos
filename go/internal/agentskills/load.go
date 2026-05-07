// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentskills

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

const skillQuickRefLimit = 3

func loadBundle(options Options) (bundle, error) {
	options = resolveOptions(options)

	payload, err := configdata.LoadYAMLMap(options.Primary)
	if err != nil {
		return bundle{}, fmt.Errorf("load primary ethos %s: %w", options.Primary, err)
	}

	overlay, hasOverlay, err := loadRepoEthosOverlay(options.RepoEthos)
	if err != nil {
		return bundle{}, err
	}

	if hasOverlay {
		payload = mergeEthos(payload, overlay)
	}

	principles := loadPrinciples(payload)

	return bundle{
		RepoName:   repoName(payload, options.RepoRoot),
		Principles: principles,
		Skills:     loadSkills(payload, principles),
	}, nil
}

func loadRepoEthosOverlay(path string) (configdata.Map, bool, error) {
	if strings.TrimSpace(path) == "" {
		return nil, false, nil
	}

	_, err := os.Stat(filepath.Clean(path))
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

func resolveOptions(options Options) Options {
	if strings.TrimSpace(options.EthosRoot) == "" {
		options.EthosRoot = "."
	}

	if strings.TrimSpace(options.RepoRoot) == "" {
		options.RepoRoot = "."
	}

	if strings.TrimSpace(options.Primary) == "" {
		options.Primary = filepath.Join(options.EthosRoot, "coding_ethos.yml")
	}

	if strings.TrimSpace(options.RepoEthos) == "" {
		options.RepoEthos = filepath.Join(options.RepoRoot, "repo_ethos.yml")
	}

	return options
}

func mergeEthos(base, overlay configdata.Map) configdata.Map {
	merged := make(configdata.Map, len(base)+len(overlay))
	maps.Copy(merged, base)

	for key, value := range overlay {
		if key == "principles" {
			merged[key] = mergePrincipleOverlay(merged[key], value)

			continue
		}

		baseMap := configdata.MapValue(merged[key])

		overlayMap := configdata.MapValue(value)
		if len(baseMap) > 0 && len(overlayMap) > 0 {
			merged[key] = mergeEthos(baseMap, overlayMap)

			continue
		}

		merged[key] = value
	}

	return merged
}

func mergePrincipleOverlay(base, overlay any) []any {
	baseItems := configdata.ListValue(base)

	overlayMap := configdata.MapValue(overlay)
	if len(overlayMap) == 0 {
		return mergeByID(baseItems, configdata.ListValue(overlay))
	}

	merged := append([]any(nil), baseItems...)
	indexByID := map[string]int{}

	for index, item := range merged {
		if id := configdata.StringAt(configdata.MapValue(item), "id"); id != "" {
			indexByID[id] = index
		}
	}

	for principleID, override := range configdata.MapValue(overlayMap["overrides"]) {
		index, ok := indexByID[principleID]
		if !ok {
			continue
		}

		merged[index] = applyPrincipleOverride(
			configdata.MapValue(merged[index]),
			configdata.MapValue(override),
		)
	}

	merged = append(merged, configdata.ListValue(overlayMap["additional"])...)

	return merged
}

func applyPrincipleOverride(base, override configdata.Map) configdata.Map {
	merged := configdata.DeepMerge(base, override)

	sections := append([]any(nil), configdata.ListValue(base["sections"])...)
	if prepend := configdata.StringAt(override, "prepend"); prepend != "" {
		sections = append(
			[]any{repoContextSection("repo-preface", "Repo Preface", prepend)},
			sections...)
	}

	if appendText := configdata.StringAt(override, "append"); appendText != "" {
		sections = append(
			sections,
			repoContextSection("repo-addendum", "Repo Addendum", appendText),
		)
	}

	if len(sections) > 0 {
		merged["sections"] = sections
	}

	delete(merged, "prepend")
	delete(merged, "append")

	return merged
}

func repoContextSection(id, title, body string) map[string]any {
	return map[string]any{
		"id":      id,
		"kind":    "repo_context",
		"title":   title,
		"summary": firstLine(body),
		"body":    body,
	}
}

func firstLine(value string) string {
	for line := range strings.SplitSeq(value, "\n") {
		if text := strings.TrimSpace(line); text != "" {
			return text
		}
	}

	return ""
}

func mergeByID(base, overlay []any) []any {
	merged := append([]any(nil), base...)
	indexByID := map[string]int{}

	for index, item := range merged {
		if id := configdata.StringAt(configdata.MapValue(item), "id"); id != "" {
			indexByID[id] = index
		}
	}

	for _, item := range overlay {
		itemMap := configdata.MapValue(item)

		itemID := configdata.StringAt(itemMap, "id")
		if itemID == "" {
			merged = append(merged, item)

			continue
		}

		index, ok := indexByID[itemID]
		if !ok {
			indexByID[itemID] = len(merged)
			merged = append(merged, item)

			continue
		}

		baseMap := configdata.MapValue(merged[index])
		if len(baseMap) == 0 {
			merged[index] = item

			continue
		}

		merged[index] = mergeEthos(baseMap, itemMap)
	}

	return merged
}

func repoName(payload configdata.Map, repoRoot string) string {
	repo := configdata.MapValue(payload["repo"])
	if name := configdata.StringAt(repo, "name"); name != "" {
		return name
	}

	if base := filepath.Base(
		filepath.Clean(repoRoot),
	); base != "." &&
		base != string(filepath.Separator) {
		return base
	}

	return "repository"
}

func loadPrinciples(payload configdata.Map) map[string]principle {
	rawItems := configdata.ListValue(payload["principles"])

	items := make([]principle, 0, len(rawItems))
	for _, item := range rawItems {
		mapping := configdata.MapValue(item)
		principleID := configdata.StringAt(mapping, "id")

		title := configdata.StringAt(mapping, "title")
		if principleID == "" || title == "" {
			continue
		}

		items = append(items, principle{
			ID:        principleID,
			Order:     configdata.IntAt(mapping, "order"),
			Title:     title,
			Summary:   configdata.StringAt(mapping, "summary"),
			Directive: configdata.StringAt(mapping, "directive"),
			QuickRef: firstStrings(
				configdata.StringList(mapping["quick_ref"]),
				skillQuickRefLimit,
			),
			Sections: loadPrincipleSections(mapping),
		})
	}

	sort.SliceStable(items, func(left, right int) bool {
		return items[left].Order < items[right].Order
	})

	byID := make(map[string]principle, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}

	return byID
}

func loadPrincipleSections(mapping configdata.Map) []principleSection {
	rawItems := configdata.ListValue(mapping["sections"])

	sections := make([]principleSection, 0, len(rawItems))
	for _, item := range rawItems {
		section := configdata.MapValue(item)
		title := configdata.StringAt(section, "title")

		body := configdata.StringAt(section, "body")
		if title == "" || body == "" {
			continue
		}

		sections = append(sections, principleSection{Title: title, Body: body})
	}

	return sections
}

func loadSkills(payload configdata.Map, principles map[string]principle) []skill {
	rawItems := configdata.ListValue(payload["skills"])

	items := make([]skill, 0, len(rawItems))
	for _, item := range rawItems {
		mapping := configdata.MapValue(item)
		skillID := configdata.StringAt(mapping, "id")

		title := configdata.StringAt(mapping, "title")
		if skillID == "" || title == "" {
			continue
		}

		principleIDs := existingPrincipleIDs(
			configdata.StringList(mapping["principle_ids"]),
			principles,
		)
		items = append(items, skill{
			ID:               skillID,
			Title:            title,
			Description:      configdata.StringAt(mapping, "description"),
			PrincipleIDs:     principleIDs,
			TriggerTerms:     configdata.StringList(mapping["trigger_terms"]),
			ShortHint:        configdata.StringAt(mapping, "short_hint"),
			Focus:            configdata.StringAt(mapping, "focus"),
			RemediationSteps: configdata.StringList(mapping["remediation_steps"]),
		})
	}

	return items
}

func existingPrincipleIDs(
	principleIDs []string,
	principles map[string]principle,
) []string {
	existing := make([]string, 0, len(principleIDs))
	for _, principleID := range principleIDs {
		if _, ok := principles[principleID]; ok {
			existing = append(existing, principleID)
		}
	}

	return existing
}

func firstStrings(values []string, limit int) []string {
	if len(values) <= limit {
		return values
	}

	return values[:limit]
}
