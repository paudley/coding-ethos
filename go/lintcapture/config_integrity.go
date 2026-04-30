// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const ToolConfigHashManifest = ".code-ethos/tool-config-hashes.json"

type ConfigDrift struct {
	File string
}

func CheckGeneratedToolConfigIntegrity(ethosRoot string, repoRoot string) ([]ConfigDrift, error) {
	uvBin := strings.TrimSpace(os.Getenv("UV"))
	if uvBin == "" {
		uvBin = "uv"
	}
	command := exec.Command(
		uvBin,
		"run",
		"--quiet",
		"--project",
		filepath.Clean(ethosRoot),
		"python",
		filepath.Join(filepath.Clean(ethosRoot), "main.py"),
		"--repo",
		filepath.Clean(repoRoot),
		"--check-tool-configs",
	)
	command.Dir = filepath.Clean(ethosRoot)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if strings.TrimSpace(stdout.String()+stderr.String()) != "" &&
			containsExitError(err, &exitErr) {
			return parseToolConfigDrift(repoRoot, stdout.String()+stderr.String()), nil
		}

		return nil, fmt.Errorf("check generated tool configs: %w", err)
	}

	return nil, nil
}

func parseToolConfigDrift(repoRoot string, output string) []ConfigDrift {
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

func containsExitError(err error, target **exec.ExitError) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		*target = exitErr

		return true
	}

	return false
}

func repoRelativePath(repoRoot string, path string) (string, bool) {
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
