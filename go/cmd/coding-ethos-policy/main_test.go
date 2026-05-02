// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"blackcat.ca/coding-ethos/go/internal/policy"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidatedConfigSectionsRejectsUnknownTopLevelKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(
		path,
		[]byte("style: {}\nwrong_section: {}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, _, err := validatedConfigSections(path, nil)
	if err == nil {
		t.Fatal("expected unknown top-level section error")
	}
	if !strings.Contains(err.Error(), `wrong_section`) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidatedConfigSectionsSortsKnownTopLevelKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(
		path,
		[]byte("python: {}\nstyle: {}\nhooks: {}\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, sections, err := validatedConfigSections(path, nil)
	if err != nil {
		t.Fatalf("validate config sections: %v", err)
	}

	want := strings.Join([]string{"hooks", "python", "style"}, ",")
	if strings.Join(sections, ",") != want {
		t.Fatalf("sections = %#v, want %s", sections, want)
	}
}

func TestValidateRepoConfigSectionsRejectsNestedTypos(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")
	if err := os.WriteFile(
		configPath,
		[]byte("python:\n  comment_suppressions:\n    enabled: true\n"),
		0o600,
	); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(
		repoConfigPath,
		[]byte("python:\n  comment_supressions:\n    enabled: false\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	configShape, _, err := validatedConfigSections(configPath, nil)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	_, err = validateRepoConfigSections(repoConfigPath, configShape)
	if err == nil {
		t.Fatal("expected nested typo error")
	}
	if !strings.Contains(err.Error(), "python.comment_supressions") {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRepoConfigSectionsAllowsRepoLicenseOverlay(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	repoConfigPath := filepath.Join(dir, "repo_config.yaml")
	if err := os.WriteFile(configPath, []byte("style:\n  line_length: 100\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if err := os.WriteFile(
		repoConfigPath,
		[]byte("repo:\n  license:\n    spdx_identifier: MIT\n    copyright: Example Inc.\n"),
		0o600,
	); err != nil {
		t.Fatalf("write repo config: %v", err)
	}

	configShape, _, err := validatedConfigSections(configPath, nil)
	if err != nil {
		t.Fatalf("validate config: %v", err)
	}
	sections, err := validateRepoConfigSections(repoConfigPath, configShape)
	if err != nil {
		t.Fatalf("validate repo config: %v", err)
	}
	if strings.Join(sections, ",") != "repo" {
		t.Fatalf("sections = %#v", sections)
	}
}

func TestValidateMetadataCommandChecksPolicySources(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "config.yaml")
	metadataPath := filepath.Join(dir, "policy-metadata.json")
	if err := os.WriteFile(sourcePath, []byte("style: {}\n"), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}

	metadata := policy.Metadata{
		SourceHashes: map[string]string{
			sourcePath: "sha256:99faa993bc5910bf699657cd8af777791cd11bf48267e1bdb68fa6f6e9181921",
		},
		BundleHash:  "sha256:bundle",
		GeneratedAt: "2026-05-01T00:00:00Z",
	}
	file, err := os.Create(metadataPath)
	if err != nil {
		t.Fatalf("create metadata: %v", err)
	}
	if err := policy.EncodeMetadata(file, metadata); err != nil {
		t.Fatalf("encode metadata: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close metadata: %v", err)
	}

	if err := validateMetadata([]string{"--metadata", metadataPath}); err != nil {
		t.Fatalf("validate metadata: %v", err)
	}

	if err := os.WriteFile(sourcePath, []byte("style:\n  line_length: 88\n"), 0o600); err != nil {
		t.Fatalf("rewrite source: %v", err)
	}
	err = validateMetadata([]string{"--metadata", metadataPath})
	if err == nil || !strings.Contains(err.Error(), "policy source hash mismatch") {
		t.Fatalf("validate stale metadata error = %v", err)
	}
}
