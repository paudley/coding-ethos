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

	_, err := validatedConfigSections(path)
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

	sections, err := validatedConfigSections(path)
	if err != nil {
		t.Fatalf("validate config sections: %v", err)
	}

	want := strings.Join([]string{"hooks", "python", "style"}, ",")
	if strings.Join(sections, ",") != want {
		t.Fatalf("sections = %#v, want %s", sections, want)
	}
}
