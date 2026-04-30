// SPDX-FileCopyrightText: 2026 Blackcat Informatics® Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const toolCatalogGoFile = "pkg/app.go"

func TestParseCatalogFindingsEnrichesDiagnostics(t *testing.T) {
	t.Parallel()

	findings := parseCatalogFindings(
		"shellcheck",
		`{"comments":[{"file":"script.sh","line":3,"column":7,`+
			`"level":"warning","code":2086,`+
			`"message":"Double quote to prevent globbing and word splitting."}]}`,
	)
	if len(findings) != 1 {
		t.Fatalf("parseCatalogFindings() = %#v, want one finding", findings)
	}

	if findings[0].PolicyID != "shell.static_analysis" {
		t.Fatalf("policy id = %q", findings[0].PolicyID)
	}

	if findings[0].Advice == "" {
		t.Fatalf("advice missing from enriched finding: %#v", findings[0])
	}
}

func TestToolchainFilesUsesCatalogMetadata(t *testing.T) {
	t.Parallel()

	paths := []string{
		"Dockerfile",
		"README.md",
		".github/workflows/ci.yml",
		toolCatalogGoFile,
		"config.yaml",
		"script.sh",
	}

	if got := toolchainFiles("hadolint", paths); len(got) != 1 || got[0] != "Dockerfile" {
		t.Fatalf("hadolint files = %#v", got)
	}

	if got := toolchainFiles("actionlint", paths); len(got) != 1 ||
		got[0] != ".github/workflows/ci.yml" {
		t.Fatalf("actionlint files = %#v", got)
	}

	if got := toolchainFiles("golangci-lint", paths); len(got) != 1 ||
		got[0] != toolCatalogGoFile {
		t.Fatalf("golangci-lint files = %#v", got)
	}

	if got := toolchainFiles("yamllint", paths); len(got) != 2 {
		t.Fatalf("yamllint files = %#v", got)
	}

	if got := toolchainFiles("shfmt", paths); len(got) != 1 || got[0] != "script.sh" {
		t.Fatalf("shfmt files = %#v", got)
	}
}

func TestToolchainCommandUsesManagedBinaryPath(t *testing.T) {
	root := t.TempDir()
	bundleRoot := filepath.Join(root, "pre-commit")
	hooksRoot := filepath.Join(bundleRoot, "hooks")
	goHooksRoot := filepath.Join(hooksRoot, "go-hooks")
	githubBin := filepath.Join(root, "build", "toolchain", "github-bin")

	err := os.MkdirAll(goHooksRoot, 0o755)
	if err != nil {
		t.Fatalf("mkdir hook root: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(hooksRoot, "run-go-hook.sh"),
		[]byte("#!/usr/bin/env bash\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write run-go-hook.sh: %v", err)
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
