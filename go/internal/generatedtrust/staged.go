// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

// Package generatedtrust verifies staged generated surfaces against the active
// Coding Ethos authority before path-protection policies consider exemptions.
package generatedtrust

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agenthooks"
	"blackcat.ca/coding-ethos/go/internal/evaluators"
	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/shellquote"
	"blackcat.ca/coding-ethos/go/internal/syncstate"
	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
)

// ExactStagedFiles returns only files whose Git index bytes exactly match an
// artifact rendered by the active authority. Rendering and index-read errors
// fail closed by omitting the affected artifact.
func ExactStagedFiles(bundle policy.Bundle, cwd string, files []string) []string {
	if strings.TrimSpace(cwd) == "" || len(files) == 0 {
		return nil
	}

	ethosRoot := generatedConfigEthosRoot(bundle)
	if ethosRoot == "" {
		return nil
	}

	artifacts := generatedArtifacts(ethosRoot, cwd)
	requested := normalizedPathSet(files)

	staged := stagedChangedPathSet(cwd)
	if staged == nil {
		return nil
	}

	trusted := map[string]bool{}

	for _, artifact := range artifacts {
		path := filepath.ToSlash(filepath.Clean(artifact.Path))
		if !requested[path] || !staged[path] || artifact.ExpectedSHA256 == "" {
			continue
		}

		content, err := evaluators.GitCommand(cwd, "show", ":"+path).Output()
		if err == nil && sha256Bytes(content) == artifact.ExpectedSHA256 {
			trusted[path] = true
		}
	}

	result := make([]string, 0, len(trusted))
	for path := range trusted {
		result = append(result, path)
	}

	sort.Strings(result)

	return result
}

func stagedChangedPathSet(cwd string) map[string]bool {
	content, err := evaluators.GitCommand(
		cwd,
		"diff",
		"--cached",
		"--name-only",
		"--diff-filter=ACMR",
		"-z",
		"--",
	).Output()
	if err != nil {
		return nil
	}

	paths := map[string]bool{}

	for rawPath := range bytes.SplitSeq(content, []byte{0}) {
		path := string(rawPath)
		if path == "" {
			continue
		}

		paths[filepath.ToSlash(filepath.Clean(path))] = true
	}

	return paths
}

func generatedArtifacts(ethosRoot, repoRoot string) []syncstate.Artifact {
	artifacts := []syncstate.Artifact{}

	toolArtifacts, err := toolconfigs.StateArtifacts(ethosRoot, repoRoot, "")
	if err == nil {
		artifacts = append(artifacts, toolArtifacts...)
	}

	hookCommand := shellquote.Command(
		filepath.Join(ethosRoot, "bin", "coding-ethos-run"),
		"agent-hook",
	)

	hookArtifacts, err := agenthooks.StateArtifacts(repoRoot, hookCommand)
	if err == nil {
		artifacts = append(artifacts, hookArtifacts...)
	}

	return artifacts
}

func generatedConfigEthosRoot(bundle policy.Bundle) string {
	policyDef, found := bundle.Policies["generated_config.freshness"]
	if !found {
		return ""
	}

	for _, evaluator := range policyDef.Evaluators {
		value, ok := evaluator.Options["ethos_root"].(string)
		if ok && strings.TrimSpace(value) != "" {
			return filepath.Clean(value)
		}
	}

	return ""
}

func normalizedPathSet(files []string) map[string]bool {
	paths := make(map[string]bool, len(files))
	for _, file := range files {
		paths[filepath.ToSlash(filepath.Clean(file))] = true
	}

	return paths
}

func sha256Bytes(content []byte) string {
	sum := sha256.Sum256(content)

	return "sha256:" + hex.EncodeToString(sum[:])
}
