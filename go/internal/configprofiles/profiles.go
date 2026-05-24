// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package configprofiles

import (
	"io/fs"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/repoignore"
)

type pythonSourceDetector interface {
	HasPythonSources(repoRoot, ethosRoot string) bool
}

type walkingPythonSourceDetector struct {
	pythonSources map[string]bool
}

const generatedSiteExtraIgnoreCount = 2

// Apply merges repo-kind/profile defaults into base before explicit repo
// overrides are applied. Explicit repo_config.yaml values always win.
func Apply(base, repoConfig configdata.Map, repoRoot string) configdata.Map {
	return ApplyWithEthosRoot(base, repoConfig, repoRoot, "")
}

// ApplyWithEthosRoot applies profile defaults while excluding the actual
// coding-ethos checkout from parent source discovery.
func ApplyWithEthosRoot(
	base,
	repoConfig configdata.Map,
	repoRoot,
	ethosRoot string,
) configdata.Map {
	detector := newWalkingPythonSourceDetector()

	profileOverlay := overlayForRepoConfig(
		repoConfig,
		repoRoot,
		ethosRoot,
		detector,
	)
	if len(profileOverlay) == 0 {
		return configdata.DeepMerge(base, repoConfig)
	}

	return configdata.DeepMerge(configdata.DeepMerge(base, profileOverlay), repoConfig)
}

func overlayForRepoConfig(
	repoConfig configdata.Map,
	repoRoot,
	ethosRoot string,
	detector pythonSourceDetector,
) configdata.Map {
	overlay := configdata.Map{}
	profiles := repoProfiles(repoConfig)

	for _, profile := range profiles {
		overlay = configdata.DeepMerge(overlay, overlayForProfile(
			profile,
			repoRoot,
			ethosRoot,
			detector,
		))
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

func overlayForProfile(
	profile,
	repoRoot,
	ethosRoot string,
	detector pythonSourceDetector,
) configdata.Map {
	switch profile {
	case "go", "go-static-site":
		return goProfileOverlay(repoRoot, ethosRoot, detector)
	case "static-site", "generated-site-output":
		return generatedSiteOutputOverlay()
	default:
		return nil
	}
}

func goProfileOverlay(
	repoRoot,
	ethosRoot string,
	detector pythonSourceDetector,
) configdata.Map {
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

	if !detector.HasPythonSources(repoRoot, ethosRoot) {
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
	paths := make(
		[]any,
		0,
		len(repoignore.RuntimePaths())+generatedSiteExtraIgnoreCount,
	)
	for _, path := range repoignore.RuntimePaths() {
		paths = append(paths, path)
	}

	paths = append(paths, "site/", "dist/")

	return configdata.Map{
		"filesystem": map[string]any{
			"required_ignores": map[string]any{
				"paths": paths,
			},
		},
	}
}

func newWalkingPythonSourceDetector() *walkingPythonSourceDetector {
	return &walkingPythonSourceDetector{pythonSources: map[string]bool{}}
}

func (detector *walkingPythonSourceDetector) HasPythonSources(
	repoRoot,
	ethosRoot string,
) bool {
	cleanRepoRoot := filepath.Clean(repoRoot)
	cacheKey := cleanRepoRoot + "\x00" + filepath.Clean(ethosRoot)

	if found, ok := detector.pythonSources[cacheKey]; ok {
		return found
	}

	found := false
	skipDirs := pythonSourceSkipDirs(repoRoot, ethosRoot)

	err := filepath.WalkDir(
		cleanRepoRoot,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil || found {
				return err
			}

			if entry.IsDir() && shouldSkipDir(path, entry.Name(), skipDirs) {
				return filepath.SkipDir
			}

			if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".py") {
				found = true

				return filepath.SkipAll
			}

			return nil
		},
	)

	found = err == nil && found
	detector.pythonSources[cacheKey] = found

	return found
}

func pythonSourceSkipDirs(repoRoot, ethosRoot string) map[string]bool {
	dirs := map[string]bool{}

	if ethosRoot = strings.TrimSpace(ethosRoot); ethosRoot != "" {
		rel, err := filepath.Rel(filepath.Clean(repoRoot), filepath.Clean(ethosRoot))
		if err == nil && rel != "." && !strings.HasPrefix(rel, "..") {
			dirs[filepath.Clean(ethosRoot)] = true
		}
	}

	return dirs
}

func shouldSkipDir(path, name string, dynamicDirs map[string]bool) bool {
	if dynamicDirs[filepath.Clean(path)] {
		return true
	}

	switch name {
	case ".git", ".venv", "node_modules", "dist", "site":
		return true
	default:
		return false
	}
}
