// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package toolconfigs

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/configdata"
	"blackcat.ca/coding-ethos/go/internal/configprofiles"
)

const (
	generatedConfigDirMode  = 0o700
	generatedConfigFileMode = 0o600
)

func Sync(ethosRoot, repoRoot, repoConfig string) ([]string, error) {
	rendered, err := renderForRepo(ethosRoot, repoRoot, repoConfig)
	if err != nil {
		return nil, err
	}

	err = removeDisabledRenderedConfigs(repoRoot, rendered)
	if err != nil {
		return nil, err
	}

	written := make([]string, 0, len(rendered)+1)
	for relativePath, content := range rendered {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))

		err = os.MkdirAll(filepath.Dir(absolutePath), generatedConfigDirMode)
		if err != nil {
			return nil, fmt.Errorf(
				"create config dir %s: %w",
				filepath.Dir(absolutePath),
				err,
			)
		}

		err = os.WriteFile(absolutePath, []byte(content), generatedConfigFileMode)
		if err != nil {
			return nil, fmt.Errorf("write config %s: %w", absolutePath, err)
		}

		written = append(written, absolutePath)
	}

	manifest, err := RenderHashManifest(rendered)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(repoRoot, filepath.FromSlash(HashManifestPath))

	err = os.MkdirAll(filepath.Dir(manifestPath), generatedConfigDirMode)
	if err != nil {
		return nil, fmt.Errorf("create manifest dir: %w", err)
	}

	err = os.WriteFile(manifestPath, []byte(manifest), generatedConfigFileMode)
	if err != nil {
		return nil, fmt.Errorf("write manifest %s: %w", manifestPath, err)
	}

	written = append(written, manifestPath)

	return written, nil
}

func Check(ethosRoot, repoRoot, repoConfig string) ([]string, error) {
	rendered, err := renderForRepo(ethosRoot, repoRoot, repoConfig)
	if err != nil {
		return nil, err
	}

	mismatched := []string{}

	for relativePath, expected := range rendered {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))

		current, readErr := os.ReadFile(filepath.Clean(absolutePath))
		if readErr != nil || string(current) != expected {
			mismatched = append(mismatched, absolutePath)
		}
	}

	for _, relativePath := range disabledRendererPaths(rendered) {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))

		switch _, statErr := os.Stat(filepath.Clean(absolutePath)); {
		case statErr == nil:
			mismatched = append(mismatched, absolutePath)
		case !errors.Is(statErr, os.ErrNotExist):
			return nil, fmt.Errorf("stat disabled config %s: %w", absolutePath, statErr)
		}
	}

	manifest, err := RenderHashManifest(rendered)
	if err != nil {
		return nil, err
	}

	manifestPath := filepath.Join(repoRoot, filepath.FromSlash(HashManifestPath))

	current, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil || string(current) != manifest {
		mismatched = append(mismatched, manifestPath)
	}

	return mismatched, nil
}

func removeDisabledRenderedConfigs(
	repoRoot string,
	rendered map[string]string,
) error {
	for _, relativePath := range disabledRendererPaths(rendered) {
		absolutePath := filepath.Join(repoRoot, filepath.FromSlash(relativePath))
		err := os.Remove(filepath.Clean(absolutePath))

		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove disabled config %s: %w", absolutePath, err)
		}
	}

	return nil
}

func disabledRendererPaths(rendered map[string]string) []string {
	paths := make([]string, 0)

	for _, renderer := range renderers() {
		if _, enabled := rendered[renderer.Path]; !enabled {
			paths = append(paths, renderer.Path)
		}
	}

	return paths
}

func renderForRepo(
	ethosRoot string,
	repoRoot string,
	repoConfig string,
) (map[string]string, error) {
	config, err := LoadMergedConfig(ethosRoot, repoRoot, repoConfig)
	if err != nil {
		return nil, fmt.Errorf("load merged config: %w", err)
	}

	return RenderAll(config)
}

func LoadMergedConfig(ethosRoot, repoRoot, repoConfig string) (map[string]any, error) {
	base, err := configdata.LoadYAMLMap(filepath.Join(ethosRoot, "config.yaml"))
	if err != nil {
		return nil, fmt.Errorf("load base config: %w", err)
	}

	base, err = applyPrincipleToolConfig(ethosRoot, repoRoot, base)
	if err != nil {
		return nil, fmt.Errorf("apply principle tool config: %w", err)
	}

	if strings.TrimSpace(repoConfig) != "" {
		override, err := configdata.LoadYAMLMap(repoConfig)
		if err != nil {
			return nil, fmt.Errorf("load repo config %s: %w", repoConfig, err)
		}

		return pruneToolConfigProvenance(
			configprofiles.ApplyWithEthosRoot(base, override, repoRoot, ethosRoot),
		), nil
	}

	for _, name := range repoConfigCandidates(base) {
		override, err := configdata.LoadYAMLMap(filepath.Join(repoRoot, name))
		if err == nil {
			return pruneToolConfigProvenance(
				configprofiles.ApplyWithEthosRoot(base, override, repoRoot, ethosRoot),
			), nil
		}

		if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("load repo config candidate %s: %w", name, err)
		}
	}

	return pruneToolConfigProvenance(base), nil
}

func repoConfigCandidates(config configMap) []string {
	names := stringList(getPath(config, "bundle.consumer_override_candidates", []any{}))
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
