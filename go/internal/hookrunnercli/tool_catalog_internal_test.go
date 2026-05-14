// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package hookrunnercli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	diag "blackcat.ca/coding-ethos/go/diagnostics"
)

const toolCatalogGoFile = "pkg/app.go"

func TestToolchainFilesUsesCatalogMetadata(t *testing.T) {
	t.Parallel()

	paths := []string{
		"Dockerfile",
		"README.md",
		".github/workflows/ci.yml",
		toolCatalogGoFile,
		"config.yaml",
		"pyproject.toml",
		"queries/report.sql",
		"pkg/app.py",
		"web/app.js",
		"web/app.ts",
		".env.example",
		"script.sh",
	}

	assertToolchainFiles(t, paths, "hadolint", []string{"Dockerfile"})
	assertToolchainFiles(t, paths, "actionlint", []string{".github/workflows/ci.yml"})
	assertToolchainFiles(t, paths, "golangci-lint", []string{toolCatalogGoFile})
	assertToolchainFiles(
		t,
		paths,
		"yamllint",
		[]string{".github/workflows/ci.yml", "config.yaml"},
	)
	assertToolchainFiles(t, paths, "shfmt", []string{"script.sh"})
	assertToolchainFiles(t, paths, "tsc", []string{"web/app.ts"})
	assertToolchainFiles(t, paths, "eslint", []string{"web/app.js", "web/app.ts"})

	for name, want := range map[string]string{
		"bandit":        "pkg/app.py",
		"sqlfluff":      "queries/report.sql",
		"tombi":         "pyproject.toml",
		"dotenv-linter": ".env.example",
	} {
		got := toolchainFiles(name, paths)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("%s files = %#v, want %q", name, got, want)
		}
	}
}

func assertToolchainFiles(t *testing.T, paths []string, name string, want []string) {
	t.Helper()

	got := toolchainFiles(name, paths)
	if len(got) != len(want) {
		t.Fatalf("%s files = %#v, want %#v", name, got, want)
	}

	for index, path := range want {
		if got[index] != path {
			t.Fatalf("%s files = %#v, want %#v", name, got, want)
		}
	}
}

func TestToolchainCommandUsesManagedBinaryPath(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "pre-commit")
	hooksRoot := filepath.Join(bundleRoot, "hooks")
	githubBin := filepath.Join(root, "build", "toolchain", "github-bin")

	mustWriteTestFile(t, filepath.Join(hooksRoot, "pyproject.toml"), "")

	err := os.WriteFile(
		filepath.Join(hooksRoot, "managed-toolchain.tsv"),
		[]byte("tool\truntime\tversion\nshellcheck\tbinary\tv0.10.0\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write managed-toolchain.tsv: %v", err)
	}

	err = os.MkdirAll(githubBin, 0o755)
	if err != nil {
		t.Fatalf("mkdir github bin: %v", err)
	}

	managedShellcheck := filepath.Join(githubBin, "shellcheck")

	err = os.WriteFile(managedShellcheck, []byte("#!/usr/bin/env bash\n"), 0o600)
	if err != nil {
		t.Fatalf("write shellcheck: %v", err)
	}

	err = os.Chmod(managedShellcheck, 0o755)
	if err != nil {
		t.Fatalf("chmod shellcheck: %v", err)
	}

	t.Setenv(precommitRootEnv, bundleRoot)

	command := toolchainCommand("shellcheck")
	if len(command) == 0 || command[0] != managedShellcheck {
		t.Fatalf("toolchainCommand(shellcheck) = %#v, want managed binary", command)
	}

	if strings.Contains(strings.Join(command, " "), "/usr/bin/shellcheck") {
		t.Fatalf("toolchain command used host binary: %#v", command)
	}
}

func TestLoadCompiledEvidenceMapsReadsPolicyBundle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundleRoot := filepath.Join(root, "pre-commit")
	policyRoot := filepath.Join(root, "build", "policy")

	err := os.MkdirAll(policyRoot, 0o755)
	if err != nil {
		t.Fatalf("mkdir policy root: %v", err)
	}

	payload := struct {
		EvidenceMaps []diag.EvidenceMap `json:"evidence_maps"`
	}{
		EvidenceMaps: []diag.EvidenceMap{{
			Source:   "ruff",
			Codes:    []string{"PLC" + "0415"},
			PolicyID: "python.conditional_imports",
		}},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal bundle: %v", err)
	}

	bundlePath := filepath.Join(policyRoot, "policy-bundle.json")

	err = os.WriteFile(bundlePath, data, 0o600)
	if err != nil {
		t.Fatalf("write bundle: %v", err)
	}

	maps, err := loadCompiledEvidenceMaps(bundleRoot)
	if err != nil {
		t.Fatalf("loadCompiledEvidenceMaps(): %v", err)
	}

	if len(maps) != 1 || maps[0].PolicyID != "python.conditional_imports" {
		t.Fatalf("loadCompiledEvidenceMaps() = %#v", maps)
	}
}
