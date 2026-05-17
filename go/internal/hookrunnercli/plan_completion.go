// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

func loadPlanCompletionSettings() (planCompletionSettings, error) {
	var settings planCompletionSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}

	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.plan_completion",
		"plan_completion",
		&settings,
	)
	if err != nil {
		return settings, err
	}

	if !sectionFound {
		return settings, nil
	}

	if strings.TrimSpace(settings.MetadataFilename) == "" {
		settings.MetadataFilename = "metadata.yaml"
	}

	if len(settings.RootMarkers) == 0 {
		settings.RootMarkers = []string{"docs/plans/"}
	}

	if len(settings.CompletedStatusValues) == 0 {
		settings.CompletedStatusValues = []string{"review", "complete"}
	}

	return settings, nil
}

func stagedFiles() []string {
	output := gitOutput("diff", "--cached", "--name-only")
	if output == "" {
		return []string{}
	}

	return strings.Fields(output)
}

func findPlanMetadataFiles(
	paths []string,
	settings planCompletionSettings,
) []string {
	matches := make([]string, 0)

	for _, raw := range paths {
		path := strings.TrimSpace(raw)
		if path == "" {
			continue
		}

		if filepath.Base(path) != settings.MetadataFilename {
			continue
		}

		normalized := normalizeGeminiPath(path)
		for _, marker := range settings.RootMarkers {
			marker = normalizeGeminiPath(marker)
			if marker != "" && strings.Contains(normalized, marker) {
				matches = append(matches, path)

				break
			}
		}
	}

	return matches
}

func planStatus(metadataPath string) (string, error) {
	content, err := os.ReadFile(metadataPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", metadataPath, err)
	}

	var data map[string]any

	err = yaml.Unmarshal(content, &data)
	if err != nil {
		return "", fmt.Errorf("parse %s: %w", metadataPath, err)
	}

	return normalizedConfigString(data["status"]), nil
}

type uncheckedPlanItem struct {
	File string
	Text string
	Line int
}

func findUncheckedPlanItems(planDir string) ([]uncheckedPlanItem, error) {
	items := make([]uncheckedPlanItem, 0)
	pattern := regexp.MustCompile(`^-\s*\[\s*\]\s+.+`)

	err := filepath.WalkDir(
		planDir,
		func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			if entry.IsDir() {
				return nil
			}

			if filepath.Ext(path) != ".md" {
				return nil
			}

			content, err := readWalkedPlanFile(planDir, path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}

			for index, line := range strings.Split(string(content), "\n") {
				if pattern.MatchString(line) {
					items = append(items, uncheckedPlanItem{
						File: path,
						Line: index + 1,
						Text: strings.TrimSpace(line),
					})
				}
			}

			return nil
		},
	)
	if err != nil {
		return items, fmt.Errorf("walk %s: %w", planDir, err)
	}

	return items, nil
}

func readWalkedPlanFile(root, path string) ([]byte, error) {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return nil, fmt.Errorf("relativize plan path: %w", err)
	}

	if filepath.IsAbs(relative) || strings.HasPrefix(relative, "..") {
		return nil, fmt.Errorf("%w: %s", errPlanPathEscapesRoot, path)
	}

	content, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		return nil, fmt.Errorf("read walked plan file: %w", err)
	}

	return content, nil
}

func checkPlanCompletionErrors(
	metadataPath string,
	settings planCompletionSettings,
) ([]hookFinding, string, error) {
	status, err := planStatus(metadataPath)
	if err != nil {
		return nil, "", fmt.Errorf("read %s status: %w", metadataPath, err)
	}

	completed := make(map[string]struct{}, len(settings.CompletedStatusValues))
	for _, value := range settings.CompletedStatusValues {
		value = strings.TrimSpace(value)
		if value != "" {
			completed[value] = struct{}{}
		}
	}

	if _, ok := completed[status]; !ok {
		return nil, status, nil
	}

	planDir := filepath.Dir(metadataPath)

	unchecked, err := findUncheckedPlanItems(planDir)
	if err != nil {
		return nil, status, fmt.Errorf("scan %s plan items: %w", planDir, err)
	}

	if len(unchecked) == 0 {
		return nil, status, nil
	}

	findings := make([]hookFinding, 0, len(unchecked))
	for _, item := range unchecked {
		relative, relErr := filepath.Rel(planDir, item.File)
		if relErr != nil {
			relative = item.File
		}

		findings = append(findings, hookFinding{
			Tool:    "plan_completion",
			File:    filepath.Join(filepath.Base(planDir), relative),
			Line:    item.Line,
			Message: "unchecked plan item",
			Detail:  item.Text,
		})
	}

	return findings, status, nil
}

func checkPlanCompletionCommand(_ Config, args []string) int {
	settings, err := loadPlanCompletionSettings()
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled {
		return 0
	}

	paths := args
	if len(paths) == 0 {
		paths = stagedFiles()
	}

	metadataFiles := findPlanMetadataFiles(paths, settings)

	allFindings := make([]hookFinding, 0)
	status := ""

	for _, metadataPath := range metadataFiles {
		findings, foundStatus, err := checkPlanCompletionErrors(metadataPath, settings)
		if err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: %s: %v\n", metadataPath, err)

			return 1
		}

		if foundStatus != "" {
			status = foundStatus
		}

		allFindings = append(allFindings, findings...)
	}

	if len(allFindings) > 0 {
		emitHookReport(os.Stderr, hookReport{
			Tool:  "plan_completion",
			Title: "PLAN COMPLETION FRAUD DETECTED",
			Summary: fmt.Sprintf(
				"Cannot mark plan as %q with incomplete items.",
				status,
			),
			Findings: allFindings,
			Guidance: []string{
				"Complete the work and check off items when done.",
				"Get explicit user approval to remove items from scope.",
				"Change status back to in_progress.",
			},
		}, selectedHookOutputFormat())

		return 1
	}

	return 0
}
