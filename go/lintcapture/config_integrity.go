// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture

import (
	"fmt"
	"path/filepath"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

const ToolConfigHashManifest = ".code-ethos/tool-config-hashes.json"

type ConfigDrift struct {
	File string
}

func CheckGeneratedToolConfigIntegrity(
	ethosRoot, repoRoot string,
) ([]ConfigDrift, error) {
	mismatched, err := toolconfigs.Check(ethosRoot, repoRoot, "")
	if err != nil {
		return nil, fmt.Errorf("check generated tool configs: %w", err)
	}

	drift := make([]ConfigDrift, 0, len(mismatched))
	for _, path := range mismatched {
		file := strings.TrimSpace(path)
		if rel, ok := repoRelativePath(repoRoot, file); ok {
			file = rel
		}

		drift = append(drift, ConfigDrift{File: file})
	}

	return drift, nil
}

func parseToolConfigDrift(repoRoot, output string) []ConfigDrift {
	drift := make([]ConfigDrift, 0)

	for line := range strings.SplitSeq(strings.TrimSpace(output), "\n") {
		path := strings.TrimSpace(line)
		if path == "" {
			continue
		}

		if rel, ok := repoRelativePath(repoRoot, path); ok {
			path = rel
		}

		drift = append(drift, ConfigDrift{File: filepath.ToSlash(path)})
	}

	if len(drift) == 0 {
		drift = append(drift, ConfigDrift{File: "generated tool configs"})
	}

	return drift
}

func repoRelativePath(repoRoot, path string) (string, bool) {
	if !filepath.IsAbs(path) {
		return path, true
	}

	rel, err := filepath.Rel(filepath.Clean(repoRoot), filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}

	return filepath.ToSlash(rel), true
}
