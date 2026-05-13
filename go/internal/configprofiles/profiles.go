// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package configprofiles

import (
	"io/fs"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
)

// Apply merges repo-kind/profile defaults into base before explicit repo
// overrides are applied. Explicit repo_config.yaml values always win.
func Apply(base, repoConfig configdata.Map, repoRoot string) configdata.Map {
	profileOverlay := overlayForRepoConfig(repoConfig, repoRoot)
	if len(profileOverlay) == 0 {
		return configdata.DeepMerge(base, repoConfig)
	}

	return configdata.DeepMerge(configdata.DeepMerge(base, profileOverlay), repoConfig)
}

func overlayForRepoConfig(repoConfig configdata.Map, repoRoot string) configdata.Map {
	profiles := repoProfiles(repoConfig)
	if len(profiles) == 0 {
		return nil
	}

	overlay := configdata.Map{}
	for _, profile := range profiles {
		overlay = configdata.DeepMerge(overlay, overlayForProfile(profile, repoRoot))
	}

	return overlay
}

func repoProfiles(repoConfig configdata.Map) []string {
	profiles := []string{}
	repo := configdata.MapValue(repoConfig["repo"])
	kind := strings.TrimSpace(configdata.StringAt(repo, "kind"))
	if kind != "" {
		profiles = append(profiles, kind)
	}

	profiles = append(profiles, configdata.StringList(repoConfig["profiles"])...)

	return dedupeProfiles(profiles)
}

func dedupeProfiles(values []string) []string {
	seen := map[string]bool{}
	deduped := []string{}
	for _, value := range values {
		profile := strings.TrimSpace(value)
		if profile == "" || seen[profile] {
			continue
		}

		seen[profile] = true
		deduped = append(deduped, profile)
	}

	return deduped
}

func overlayForProfile(profile, repoRoot string) configdata.Map {
	switch profile {
	case "go", "go-static-site":
		return goProfileOverlay(repoRoot)
	case "static-site", "generated-site-output":
		return generatedSiteOutputOverlay()
	default:
		return nil
	}
}

func goProfileOverlay(repoRoot string) configdata.Map {
	overlay := configdata.Map{
		"go": map[string]any{"enabled": true},
		"hooks": map[string]any{"enabled_groups": []any{
			"syntax",
			"docs",
			"security",
			"shell",
			"workflow",
			"go",
			"ai",
			"commit-msg",
		}},
	}

	if !repoHasPythonSources(repoRoot) {
		overlay = configdata.DeepMerge(overlay, nonPythonOverlay())
	}

	return overlay
}

func nonPythonOverlay() configdata.Map {
	return configdata.Map{
		"python": map[string]any{
			"pytest_gate": map[string]any{"enabled": false},
			"file_docstrings": map[string]any{
				"enabled": false,
			},
			"docstring_coverage": map[string]any{
				"enabled": false,
			},
			"type_check": map[string]any{"enabled": false},
		},
	}
}

func generatedSiteOutputOverlay() configdata.Map {
	return configdata.Map{
		"filesystem": map[string]any{
			"required_ignores": map[string]any{
				"paths": []any{".code-ethos/cache/", ".coding-ethos/", "site/", "dist/"},
			},
		},
	}
}

func repoHasPythonSources(repoRoot string) bool {
	found := false
	_ = filepath.WalkDir(filepath.Clean(repoRoot), func(path string, entry fs.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}

		if entry.IsDir() && shouldSkipDir(entry.Name()) {
			return filepath.SkipDir
		}

		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
			found = true

			return filepath.SkipAll
		}

		return nil
	})

	return found
}

func shouldSkipDir(name string) bool {
	switch name {
	case ".git", ".venv", "node_modules", "coding-ethos", "dist", "site":
		return true
	default:
		return false
	}
}
