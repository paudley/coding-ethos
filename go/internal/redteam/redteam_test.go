// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package redteam

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
)

func TestDefaultScenariosBlockKnownBypassClasses(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	if output, err := exec.Command("git", "-C", repoRoot, "init").CombinedOutput(); err != nil {
		t.Fatalf("init isolated git repo: %v\n%s", err, output)
	}
	realGit, _ := exec.LookPath("git")
	bundle := compileRepoBundle(t)

	results, err := RunScenarios(
		bundle,
		DefaultScenarios(realGit),
		repoRoot,
	)
	if err != nil {
		t.Fatalf("run scenarios: %v", err)
	}

	if missed := Missed(results); len(missed) > 0 {
		t.Fatalf("missed red-team scenarios: %#v", missed)
	}
}

func compileRepoBundle(t *testing.T) policy.Bundle {
	t.Helper()

	root := repoRoot(t)
	bundle, _, err := policy.Compile(policy.CompileOptions{
		Primary:     filepath.Join(root, "coding_ethos.yml"),
		Config:      filepath.Join(root, "config.yaml"),
		BundleID:    "red-team-test-bundle",
		GeneratedAt: "2026-05-02T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("compile repo policy bundle: %v", err)
	}

	return bundle
}

func repoRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "coding_ethos.yml")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repository root")
		}
		dir = parent
	}
}

func TestRunScenarioRejectsUnknownSurface(t *testing.T) {
	t.Parallel()

	_, err := RunScenario(
		policy.ExampleBundle(),
		Scenario{
			ID:      "unknown",
			Surface: "elsewhere",
		},
		t.TempDir(),
	)
	if err == nil {
		t.Fatal("RunScenario() accepted unknown surface")
	}
}

func TestMissedReturnsOnlyMissedScenarios(t *testing.T) {
	t.Parallel()

	results := []Result{
		{ScenarioID: "blocked", Missed: false},
		{ScenarioID: "missed", Missed: true},
	}

	missed := Missed(results)
	if len(missed) != 1 || missed[0].ScenarioID != "missed" {
		t.Fatalf("Missed() = %#v", missed)
	}
}
