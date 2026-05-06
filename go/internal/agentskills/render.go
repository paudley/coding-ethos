// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package agentskills

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	generatedDirMode    = 0o700
	generatedFileMode   = 0o600
	geminiManifestPath  = ".gemini/extensions/coding-ethos/gemini-extension.json"
	principleDetailBase = 2
	principleDetailSize = 8
	skillSPDXCopyright  = "<!-- SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. " +
		"<paudley@blackcat.ca> -->"
	skillDefaultFocus = "Use this skill when a code-quality finding needs " +
		"ETHOS-grounded remediation."
	skillOutputDiscipline = "When explaining a fix, name the ETHOS principle, " +
		"the concrete code change, and the verification evidence. Do not recommend " +
		"weakening lint config or adding suppressions unless the ETHOS policy " +
		"explicitly allows it."
)

func skillSurfaceFormats() []string {
	return []string{
		".agents/skills/%s/SKILL.md",
		".claude/skills/%s/SKILL.md",
		".codex/skills/%s/SKILL.md",
		".gemini/extensions/coding-ethos/skills/%s/SKILL.md",
	}
}

func Sync(options Options) ([]string, error) {
	rendered, err := Render(options)
	if err != nil {
		return nil, err
	}

	written := make([]string, 0, len(rendered))
	for relativePath, content := range rendered {
		absolutePath := filepath.Join(options.RepoRoot, filepath.FromSlash(relativePath))

		err := os.MkdirAll(filepath.Dir(absolutePath), generatedDirMode)
		if err != nil {
			return nil, fmt.Errorf("create skill dir %s: %w", filepath.Dir(absolutePath), err)
		}

		err = os.WriteFile(absolutePath, []byte(content), generatedFileMode)
		if err != nil {
			return nil, fmt.Errorf("write skill %s: %w", absolutePath, err)
		}

		written = append(written, absolutePath)
	}

	sort.Strings(written)

	return written, nil
}

func Check(options Options) ([]string, error) {
	rendered, err := Render(options)
	if err != nil {
		return nil, err
	}

	mismatched := []string{}

	for relativePath, expected := range rendered {
		absolutePath := filepath.Join(options.RepoRoot, filepath.FromSlash(relativePath))

		current, err := os.ReadFile(filepath.Clean(absolutePath))
		if err != nil || string(current) != expected {
			mismatched = append(mismatched, absolutePath)
		}
	}

	sort.Strings(mismatched)

	return mismatched, nil
}

func Render(options Options) (map[string]string, error) {
	options = resolveOptions(options)

	loaded, err := loadBundle(options)
	if err != nil {
		return nil, err
	}

	rendered := map[string]string{}

	for _, item := range loaded.Skills {
		content := renderSkillMarkdown(loaded, item)
		for _, surface := range skillSurfaceFormats() {
			rendered[fmt.Sprintf(surface, item.ID)] = content
		}
	}

	rendered[geminiManifestPath] = renderGeminiManifest(loaded)

	return rendered, nil
}

func renderSkillMarkdown(loaded bundle, item skill) string {
	principles := skillPrinciples(loaded, item)

	lines := []string{
		"---",
		`name: "` + item.ID + `"`,
		`description: "` + escapeYAMLString(item.Description) + `"`,
		"metadata:",
		"  source: coding_ethos.yml",
		"  generated_by: coding-ethos",
		"  ethos_principles:",
	}
	for _, principle := range principles {
		lines = append(lines, "    - "+principle.ID)
	}

	lines = append(
		lines,
		"---",
		skillSPDXCopyright,
		"<!-- SPDX-License-Identifier: MIT -->",
		"",
		"# "+item.Title,
		"",
	)
	if item.Focus != "" {
		lines = append(lines, item.Focus, "")
	} else {
		lines = append(lines, skillDefaultFocus, "")
	}

	lines = append(lines, skillGroundingLines(principles)...)
	lines = append(lines, skillHintLines(item)...)
	lines = append(lines, skillPrincipleDetailLines(principles)...)

	lines = append(
		lines,
		"",
		"## Output Discipline",
		skillOutputDiscipline,
	)

	return strings.Join(lines, "\n") + "\n"
}

func skillGroundingLines(principles []principle) []string {
	lines := make([]string, 0, 1+len(principles))
	lines = append(lines, "## ETHOS Grounding")

	for _, item := range principles {
		directive := firstNonEmpty(item.Directive, item.Summary)
		lines = append(lines, "- `"+item.ID+"`: "+directive)
	}

	return lines
}

func skillHintLines(item skill) []string {
	lines := []string{}
	if item.ShortHint != "" {
		lines = append(lines, "", "## Short Hint", item.ShortHint)
	}

	if len(item.TriggerTerms) > 0 {
		lines = append(lines, "", "## Use When")
		for _, term := range item.TriggerTerms {
			lines = append(lines, "- "+term)
		}
	}

	if len(item.RemediationSteps) > 0 {
		lines = append(lines, "", "## Remediation Workflow")
		for index, step := range item.RemediationSteps {
			lines = append(lines, fmt.Sprintf("%d. %s", index+1, step))
		}
	}

	return lines
}

func skillPrincipleDetailLines(principles []principle) []string {
	lines := make(
		[]string,
		0,
		principleDetailBase+(len(principles)*principleDetailSize),
	)
	lines = append(lines, "", "## Principle Details")

	for _, item := range principles {
		lines = append(lines, singlePrincipleDetailLines(item, principles)...)
	}

	return lines
}

func singlePrincipleDetailLines(item principle, principles []principle) []string {
	lines := []string{
		"### " + item.Title,
		"",
		item.Summary,
		"",
		"Directive: " + item.Directive,
	}
	if len(item.QuickRef) > 0 {
		lines = append(lines, "", "Quick ref:")
		for _, ref := range item.QuickRef {
			lines = append(lines, "- "+ref)
		}
	}

	for _, section := range item.Sections {
		lines = append(
			lines,
			"",
			"#### "+section.Title,
			normalizeSkillCrossReferences(section.Body, principles),
		)
	}

	return lines
}

func skillPrinciples(loaded bundle, item skill) []principle {
	principles := make([]principle, 0, len(item.PrincipleIDs))
	for _, id := range item.PrincipleIDs {
		if itemPrinciple, ok := loaded.Principles[id]; ok {
			principles = append(principles, itemPrinciple)
		}
	}

	return principles
}

func renderGeminiManifest(loaded bundle) string {
	skillIDs := make([]string, 0, len(loaded.Skills))
	for _, item := range loaded.Skills {
		skillIDs = append(skillIDs, item.ID)
	}

	payload := map[string]string{
		"name":    "coding-ethos",
		"version": "1.0.0",
		"description": "ETHOS skills for " + loaded.RepoName + ": " + strings.Join(
			skillIDs,
			", ",
		),
		"contextFileName": "GEMINI.md",
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "{}\n"
	}

	return string(data) + "\n"
}

func normalizeSkillCrossReferences(text string, principles []principle) string {
	result := text
	for _, principle := range principles {
		result = strings.ReplaceAll(
			result,
			fmt.Sprintf("[Section %d: %s]", principle.Order, principle.Title),
			principle.Title,
		)
	}

	return result
}

func escapeYAMLString(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
