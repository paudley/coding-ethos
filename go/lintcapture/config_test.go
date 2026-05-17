// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package lintcapture_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/toolconfigs"
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

	_, err = config.LintSourceRoots()
	if err == nil {
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
	t.Parallel()

	ethos, root := setupToolConfigChecker(t)

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethos, root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}

	got := driftFiles(drift)

	want := []string{
		".bandit.yml",
		".code-ethos/tool-config-hashes.json",
		".golangci.yml",
		".pylintrc",
		".sqlfluff",
		".yamllint.yml",
		"mypy.ini",
		"pyrightconfig.json",
		"ruff.toml",
		"tombi.toml",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestCheckGeneratedToolConfigIntegrityDoesNotTrustManifestOnly(t *testing.T) {
	t.Parallel()

	ethos, root := setupToolConfigChecker(t)
	writeFile(
		t,
		filepath.Join(root, lintcapture.ToolConfigHashManifest),
		`{"configs":{}}
`,
	)

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethos, root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}

	if got := driftFiles(
		drift,
	); !slices.Contains(
		got,
		lintcapture.ToolConfigHashManifest,
	) {
		t.Fatalf("drift = %#v", drift)
	}
}

func TestCheckGeneratedToolConfigIntegrityPassesRendererCheck(t *testing.T) {
	t.Parallel()

	ethos, root := setupToolConfigChecker(t)

	_, err := toolconfigs.Sync(ethos, root, "")
	if err != nil {
		t.Fatalf("Sync(): %v", err)
	}

	drift, err := lintcapture.CheckGeneratedToolConfigIntegrity(ethos, root)
	if err != nil {
		t.Fatalf("CheckGeneratedToolConfigIntegrity(): %v", err)
	}

	if len(drift) != 0 {
		t.Fatalf("drift = %#v, want none", drift)
	}
}

func setupToolConfigChecker(t *testing.T) (string, string) {
	t.Helper()

	root := t.TempDir()
	ethos := filepath.Join(root, "coding-ethos")
	consumer := filepath.Join(root, "consumer")
	writeFile(t, filepath.Join(ethos, "config.yaml"), `
style:
  python_version: "3.13"
  line_length: 88
python:
  source_paths:
    - pkg
  extra_paths:
    - .
tooling:
  ruff:
    ignore: []
  mypy:
    plugins: []
  pyright: {}
  pylint: {}
  yamllint:
    rules: {}
  bandit: {}
  sqlfluff: {}
  golangci_lint: {}
generated_config:
  ci:
    github_actions:
      enabled: false
    gitlab:
      enabled: false
`)

	return ethos, consumer
}

func driftFiles(drift []lintcapture.ConfigDrift) []string {
	files := make([]string, 0, len(drift))
	for _, item := range drift {
		files = append(files, item.File)
	}

	slices.Sort(files)

	return files
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
