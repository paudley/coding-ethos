// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package lintcapture_test

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
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

func TestLoadRuntimeConfigRejectsAbsoluteSourcePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), `
bundle:
  consumer_override_candidates:
    - repo_config.yml
python:
  source_paths:
    - /opt/app/src
`)

	config, err := lintcapture.LoadRuntimeConfig(ethos, consumer)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig(): %v", err)
	}
	if _, err := config.LintSourceRoots(); err == nil {
		t.Fatal("LintSourceRoots() accepted absolute python.source_paths entry")
	}
}

func TestLoadRuntimeConfigMergesSandboxReadWritePaths(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), `
bundle:
  consumer_override_candidates:
    - repo_config.yml
sandbox:
  read_write_paths:
    - .code-ethos/cache/
    - .coding-ethos/cache/
    - .pytest_cache/
    - .mypy_cache/
    - .ruff_cache/
    - .uv-cache/
    - .venv/
    - __pycache__/
`)
	writeFile(t, filepath.Join(consumer, "repo_config.yml"), `
sandbox:
  read_write_paths:
    - /opt/foundation
    - /opt/src/vllm
  rw_paths:
    - /scratch/lbox
    - /opt/foundation
`)

	config, err := lintcapture.LoadRuntimeConfig(ethos, consumer)
	if err != nil {
		t.Fatalf("LoadRuntimeConfig(): %v", err)
	}

	got := config.SandboxReadWritePaths()
	for _, want := range []string{
		".code-ethos/cache/",
		".coding-ethos/cache/",
		".pytest_cache/",
		".mypy_cache/",
		".ruff_cache/",
		".uv-cache/",
		".venv/",
		"__pycache__/",
		"/opt/foundation",
		"/opt/src/vllm",
		"/scratch/lbox",
	} {
		if !slices.Contains(got, want) {
			t.Fatalf("SandboxReadWritePaths() missing %q: %#v", want, got)
		}
	}
}

func TestCheckGeneratedToolConfigIntegrityReportsDrift(t *testing.T) {
	ethos, root := setupToolConfigChecker(t, 1, "ruff.toml\n")

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethos, root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}
	if len(drift) != 1 || drift[0].File != "ruff.toml" {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestCheckGeneratedToolConfigIntegrityDoesNotTrustManifestOnly(t *testing.T) {
	ethos, root := setupToolConfigChecker(t, 1, filepath.Join(rootPlaceholder, "ruff.toml")+"\n")
	writeFile(t, filepath.Join(root, lintcapture.ToolConfigHashManifest), `{"configs":{}}
`)

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethos, root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}
	if len(drift) != 1 || drift[0].File != "ruff.toml" {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestCheckGeneratedToolConfigIntegrityPassesRendererCheck(t *testing.T) {
	ethos, root := setupToolConfigChecker(t, 0, "")

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethos, root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}
	if len(drift) != 0 {
		t.Fatalf("drift = %#v, want none", drift)
	}
}

const rootPlaceholder = "__ROOT__"

func setupToolConfigChecker(t *testing.T, exitCode int, output string) (string, string) {
	t.Helper()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	uv := filepath.Join(root, "bin", "uv")
	scriptOutput := filepath.ToSlash(strings.ReplaceAll(output, rootPlaceholder, consumer))
	writeFile(t, filepath.Join(ethos, "main.py"), "raise SystemExit(0)\n")
	writeFile(t, uv, "#!/usr/bin/env sh\nprintf '%s' '"+scriptOutput+"'\nexit "+strconv.Itoa(exitCode)+"\n")
	if err := os.Chmod(uv, 0o700); err != nil {
		t.Fatalf("chmod uv fixture: %v", err)
	}
	t.Setenv("UV", uv)

	return ethos, consumer
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
