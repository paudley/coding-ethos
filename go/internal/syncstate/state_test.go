// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package syncstate

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestInstallSyncStatePlansRecordsAndDoctorsArtifacts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	ethosRoot := t.TempDir()
	configPath := filepath.Join(ethosRoot, "config.yaml")
	artifactPath := filepath.Join(repoRoot, "generated", "artifact.txt")

	writeTestFile(t, filepath.Join(ethosRoot, "pyproject.toml"), "version = \"9.8.7\"\n")
	writeTestFile(t, configPath, "project:\n  name: example\n")

	artifacts, err := Artifacts(repoRoot, []ArtifactInput{
		{
			RelativePath:        "generated/artifact.txt",
			Content:             "expected\n",
			Provider:            "tool-configs",
			Surface:             "generated-tool-config",
			VerificationCommand: "make check-tool-configs",
		},
	})
	if err != nil {
		t.Fatalf("artifacts: %v", err)
	}

	plan := Plan(repoRoot, "sync-tool-configs", artifacts)
	if plan.Status != artifactStatusPlanned || plan.PlannedWriteCount != 1 ||
		plan.Artifacts[0].Status != artifactStatusMissing {
		t.Fatalf("plan = %#v", plan)
	}

	writeTestFile(t, artifactPath, "expected\n")

	state, err := Upsert(UpsertOptions{
		RepoRoot:        repoRoot,
		EthosRoot:       ethosRoot,
		RequestedAction: "sync-tool-configs",
		SourcePaths:     []string{configPath},
		ProviderTargets: []ProviderTarget{{Provider: "tool-configs", Root: repoRoot}},
		Artifacts:       artifacts,
		Now:             time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	if state.SchemaVersion != SchemaVersion || state.RuntimeVersion != "9.8.7" ||
		len(state.Artifacts) != 1 || len(state.SourceHashes) != 1 {
		t.Fatalf("state = %#v", state)
	}
	if state.Artifacts[0].LastWrittenUTC != "2026-02-03T04:05:06Z" {
		t.Fatalf("last written timestamp = %s", state.Artifacts[0].LastWrittenUTC)
	}

	doctor, err := Doctor(repoRoot)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if doctor.Status != "pass" || doctor.PlannedWriteCount != 0 {
		t.Fatalf("doctor = %#v", doctor)
	}

	writeTestFile(t, artifactPath, "drifted\n")

	doctor, err = Doctor(repoRoot)
	if err != nil {
		t.Fatalf("doctor after drift: %v", err)
	}
	if doctor.Status != "fail" || doctor.Artifacts[0].Status != artifactStatusDrifted {
		t.Fatalf("doctor after drift = %#v", doctor)
	}

	repair, err := RepairPlan(repoRoot)
	if err != nil {
		t.Fatalf("repair plan: %v", err)
	}
	if repair.Status != artifactStatusPlanned || repair.PlannedWriteCount != 1 ||
		repair.Artifacts[0].Plan != "write" {
		t.Fatalf("repair = %#v", repair)
	}

	writeTestFile(t, configPath, "project:\n  name: changed\n")

	doctor, err = Doctor(repoRoot)
	if err != nil {
		t.Fatalf("doctor after source drift: %v", err)
	}
	if doctor.Sources[0].Status != sourceStatusStale {
		t.Fatalf("source reports = %#v", doctor.Sources)
	}
}

func TestRepairPlanOnlyIncludesCodingEthosOwnedArtifacts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	managedPath := filepath.Join(repoRoot, "managed.txt")
	externalPath := filepath.Join(repoRoot, "external.txt")

	artifacts, err := Artifacts(repoRoot, []ArtifactInput{
		{RelativePath: "managed.txt", Content: "expected\n", Provider: "agent-hooks"},
		{
			RelativePath: "external.txt",
			Content:      "expected\n",
			Provider:     "external",
			Ownership:    "external",
		},
	})
	if err != nil {
		t.Fatalf("artifacts: %v", err)
	}

	writeTestFile(t, managedPath, "drifted\n")
	writeTestFile(t, externalPath, "drifted\n")

	_, err = Upsert(UpsertOptions{
		RepoRoot:        repoRoot,
		RequestedAction: "agent-hooks sync",
		Artifacts:       artifacts,
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	repair, err := RepairPlan(repoRoot)
	if err != nil {
		t.Fatalf("repair plan: %v", err)
	}

	if len(repair.Artifacts) != 1 || repair.Artifacts[0].Path != "managed.txt" {
		t.Fatalf("repair artifacts = %#v", repair.Artifacts)
	}
}

func TestUpsertRebuildsCorruptedRuntimeState(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeTestFile(t, FilePath(repoRoot), "{not-json")

	state, err := Upsert(UpsertOptions{
		RepoRoot:        repoRoot,
		RequestedAction: "sync-tool-configs",
	})
	if err != nil {
		t.Fatalf("upsert corrupted state: %v", err)
	}

	if state.SchemaVersion != SchemaVersion ||
		state.RequestedAction != "sync-tool-configs" {
		t.Fatalf("state = %#v", state)
	}
}

func TestRepoRelativePathRejectsOnlyRealTraversal(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Join(t.TempDir(), "repo")

	_, err := repoRelativePath(repoRoot, "../../etc/passwd")
	if !errors.Is(err, errArtifactOutsideRepo) {
		t.Fatalf("relative traversal error = %v", err)
	}

	valid, err := repoRelativePath(repoRoot, filepath.Join(repoRoot, "..foo", "bar.txt"))
	if err != nil {
		t.Fatalf("valid path returned error: %v", err)
	}
	if valid != filepath.ToSlash(filepath.Join("..foo", "bar.txt")) {
		t.Fatalf("valid path = %q", valid)
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}

	err = os.WriteFile(path, []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
