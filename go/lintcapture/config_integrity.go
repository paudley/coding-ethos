// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const ToolConfigHashManifest = ".code-ethos/tool-config-hashes.json"

type ConfigDrift struct {
	File string
}

func CheckGeneratedToolConfigIntegrity(repoRoot string) ([]ConfigDrift, error) {
	manifestPath := filepath.Join(repoRoot, ToolConfigHashManifest)
	data, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil {
		return nil, fmt.Errorf("read generated config hash manifest: %w", err)
	}

	var manifest struct {
		Configs map[string]string `json:"configs"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("parse generated config hash manifest: %w", err)
	}

	drift := make([]ConfigDrift, 0)
	for path, expectedHash := range manifest.Configs {
		actual, err := fileSHA256(filepath.Join(repoRoot, path))
		if err != nil || !strings.EqualFold("sha256:"+actual, expectedHash) {
			drift = append(drift, ConfigDrift{File: filepath.ToSlash(path)})
		}
	}

	return drift, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:]), nil
}
