// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package redteam_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/policy"
	"blackcat.ca/coding-ethos/go/internal/redteam"
)

func TestDefaultScenariosBlockKnownBypassClasses(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()

	output, inlineErrA := exec.CommandContext(
		context.Background(),
		"git",
		"-C",
		repoRoot,
		"init",
	).
		CombinedOutput()
	if inlineErrA != nil {
		t.Fatalf("init isolated git repo: %v\n%s", inlineErrA, output)
	}

	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git executable: %v", err)
	}

	bundle := compileRepoBundle(t)

	results, err := redteam.RunScenarios(
		bundle,
		redteam.DefaultScenarios(realGit),
		repoRoot,
	)
	if err != nil {
		t.Fatalf("run scenarios: %v", err)
	}

	if missed := redteam.Missed(results); len(missed) > 0 {
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
		_, inlineErrAutoA := os.Stat(filepath.Join(dir, "coding_ethos.yml"))
		if inlineErrAutoA == nil {
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

	_, err := redteam.RunScenario(
		policy.ExampleBundle(),
		redteam.Scenario{
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

	results := []redteam.Result{
		{ScenarioID: "blocked", Missed: false},
		{ScenarioID: "missed", Missed: true},
	}

	missed := redteam.Missed(results)
	if len(missed) != 1 || missed[0].ScenarioID != "missed" {
		t.Fatalf("Missed() = %#v", missed)
	}
}
