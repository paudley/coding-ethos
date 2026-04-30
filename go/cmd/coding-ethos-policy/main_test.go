// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
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
