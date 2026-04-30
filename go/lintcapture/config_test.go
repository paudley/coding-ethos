// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"blackcat.ca/coding-ethos/go/lintcapture"
)

func TestLoadRuntimeConfigMergesConsumerConfigAndSourceRoots(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), `
bundle:
  consumer_override_candidates:
    - repo_config.yml
python:
  extra_paths:
    - lib/python
  source_paths:
    - src/pkg
`)
	writeFile(t, filepath.Join(consumer, "repo_config.yml"), `
python:
  extra_paths:
    - lbox-platform/lib/python
`)

	config, err := lintcapture.LoadRuntimeConfig(ethos, consumer)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig(): %v", err)
	}

	roots, err := config.LintSourceRoots()
	if err != nil {
		t.Fatalf("LintSourceRoots(): %v", err)
	}
	want := []string{"lbox-platform/lib/python", "src", "src/pkg"}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("LintSourceRoots() = %#v, want %#v", roots, want)
	}
}

func TestCheckGeneratedToolConfigIntegrityReportsDrift(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ruff.toml"), "line-length = 88\n")
	writeManifest(t, root, map[string]string{
		"ruff.toml": configHash("different\n"),
	})

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}
	if len(drift) != 1 || drift[0].File != "ruff.toml" {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestCheckGeneratedToolConfigIntegrityPassesMatchingManifest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	content := "line-length = 88\n"
	writeFile(t, filepath.Join(root, "ruff.toml"), content)
	writeManifest(t, root, map[string]string{
		"ruff.toml": configHash(content),
	})

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("drift = %#v, want none", drift)
	}
}

func writeManifest(t *testing.T, root string, configs map[string]string) {
	t.Helper()

	body := "{\n  \"configs\": {\n"
	first := true
	for path, hash := range configs {
		if !first {
			body += ",\n"
		}
		first = false
		body += fmt.Sprintf("    %q: %q", path, hash)
	}
	body += "\n  },\n  \"version\": 1\n}\n"
	writeFile(t, filepath.Join(root, lintcapture.ToolConfigHashManifest), body)
}

func configHash(content string) string {
	sum := sha256.Sum256([]byte(content))

	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestRequestCloneCopiesArgv(t *testing.T) {
	t.Parallel()

	original := lintcapture.Request{OriginalArgv: []string{"check", "pkg"}}
	clone := original.Clone()
	clone.OriginalArgv[0] = "format"
	if original.OriginalArgv[0] != "check" {
		t.Fatalf("Clone() shared OriginalArgv backing array: %#v", original)
	}
}
