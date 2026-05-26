// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunnercli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

func loadManifestValidationSettings() (manifestValidationSettings, error) {
	var settings manifestValidationSettings

	_, rootConfig, err := loadMergedRootConfig()
	if err != nil {
		return settings, err
	}

	sectionFound, err := decodeOptionalConfigSection(
		rootConfig,
		"python.manifest_validation",
		"manifest_validation",
		&settings,
	)
	if err != nil {
		return settings, err
	}

	if !sectionFound {
		return settings, nil
	}

	if len(settings.CandidatePaths) == 0 {
		settings.CandidatePaths = []string{"manifest.yaml", "coding-ethos/manifest.yaml"}
	}

	if len(settings.RequiredStringFields) == 0 {
		settings.RequiredStringFields = []string{"version"}
	}

	if len(settings.RequiredListSections) == 0 {
		settings.RequiredListSections = map[string]manifestValidationListSpec{
			"symlinks": {
				Required:             true,
				RequiredStringFields: []string{"source", "target"},
			},
			"repositories": {
				Required:             false,
				RequiredStringFields: []string{"name", "url"},
				OptionalStringFields: []string{"branch"},
			},
		}
	}

	return settings, nil
}

func findManifestPath(settings manifestValidationSettings) (string, error) {
	for _, raw := range settings.CandidatePaths {
		candidate := strings.TrimSpace(raw)
		if candidate == "" {
			continue
		}

		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(repoRoot(), candidate)
		}

		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate, nil
		}
	}

	return "", errManifestCandidateNotFound
}

func validateManifestData(
	data map[string]any,
	settings manifestValidationSettings,
) []string {
	validationErrors := validateManifestRequiredStrings(
		data,
		settings.RequiredStringFields,
	)
	for sectionName, spec := range settings.RequiredListSections {
		validationErrors = append(
			validationErrors,
			validateManifestListSection(data, sectionName, spec)...,
		)
	}

	return validationErrors
}

func validateManifestRequiredStrings(
	data map[string]any,
	fieldNames []string,
) []string {
	validationErrors := make([]string, 0)

	for _, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}

		value, ok := data[fieldName]
		if !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("Missing required '%s' field", fieldName),
			)

			continue
		}

		if _, ok := value.(string); !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("'%s' must be a string", fieldName),
			)
		}
	}

	return validationErrors
}

func validateManifestListSection(
	data map[string]any,
	sectionName string,
	spec manifestValidationListSpec,
) []string {
	sectionValue, hasSection := data[sectionName]
	if !hasSection || sectionValue == nil {
		if spec.Required {
			return []string{fmt.Sprintf("Missing required '%s' section", sectionName)}
		}

		return nil
	}

	entries, ok := sectionValue.([]any)
	if !ok {
		return []string{fmt.Sprintf("'%s' must be a list", sectionName)}
	}

	validationErrors := make([]string, 0)

	for index, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf("%s[%d]: Expected dict, got %T", sectionName, index, entry),
			)

			continue
		}

		validationErrors = append(
			validationErrors,
			validateManifestEntryStrings(
				entryMap,
				sectionName,
				index,
				spec.RequiredStringFields,
				true,
			)...,
		)
		validationErrors = append(
			validationErrors,
			validateManifestEntryStrings(
				entryMap,
				sectionName,
				index,
				spec.OptionalStringFields,
				false,
			)...,
		)
	}

	return validationErrors
}

func validateManifestEntryStrings(
	entryMap map[string]any,
	sectionName string,
	index int,
	fieldNames []string,
	required bool,
) []string {
	validationErrors := make([]string, 0)

	for _, fieldName := range fieldNames {
		fieldName = strings.TrimSpace(fieldName)
		if fieldName == "" {
			continue
		}

		value, ok := entryMap[fieldName]
		if !ok {
			if required {
				validationErrors = append(
					validationErrors,
					fmt.Sprintf(
						"%s[%d]: Missing '%s' field",
						sectionName,
						index,
						fieldName,
					),
				)
			}

			continue
		}

		if _, ok := value.(string); !ok {
			validationErrors = append(
				validationErrors,
				fmt.Sprintf(
					"%s[%d].%s: Expected string",
					sectionName,
					index,
					fieldName,
				),
			)
		}
	}

	return validationErrors
}

func validateManifestCommand(_ Config, _ []string) int {
	settings, err := loadManifestValidationSettings()
	if err != nil {
		writef(os.Stderr, "FATAL: %v\n", err)

		return 1
	}

	if !settings.Enabled {
		return 0
	}

	manifestPath, err := findManifestPath(settings)
	if err != nil {
		writef(os.Stderr, "ERROR: %v\n", err)

		return 1
	}

	content, err := os.ReadFile(manifestPath)
	if err != nil {
		writef(os.Stderr, "ERROR: Could not read %s: %v\n", manifestPath, err)

		return 1
	}

	var data map[string]any

	err = yaml.Unmarshal(content, &data)
	if err != nil {
		writef(os.Stderr, "ERROR: Invalid YAML syntax in %s:\n", manifestPath)
		writef(os.Stderr, "  %v\n", err)

		return 1
	}

	if data == nil {
		fmt.Fprintf(
			os.Stderr,
			"ERROR: %s must be a YAML mapping (dict)\n",
			manifestPath,
		)

		return 1
	}

	errors := validateManifestData(data, settings)
	if len(errors) > 0 {
		findings := make([]hookFinding, 0, len(errors))
		for _, item := range errors {
			findings = append(findings, hookFinding{
				Tool:    "manifest_validation",
				File:    manifestPath,
				Message: item,
			})
		}

		emitHookReport(os.Stderr, hookReport{
			Tool:     "manifest_validation",
			Title:    "MANIFEST VALIDATION FAILED",
			Findings: findings,
			Guidance: []string{
				"Update the manifest to satisfy required fields and sections.",
			},
		}, selectedHookOutputFormat())

		return 1
	}

	return 0
}
