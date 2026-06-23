// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package syncstate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInstallSyncStatePlansRecordsAndDoctorsArtifacts(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	ethosRoot := t.TempDir()
	configPath := filepath.Join(ethosRoot, "config.yaml")
	artifactPath := filepath.Join(repoRoot, "generated", "artifact.txt")

	writeTestFile(
		t,
		filepath.Join(ethosRoot, "pyproject.toml"),
		"[project]\nversion = \"9.8.7\"\n",
	)
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

func TestReportRendersAllFeedbackFormats(t *testing.T) {
	t.Parallel()

	report := Report{
		Tool:              "install-sync-doctor",
		Status:            "planned",
		StatePath:         ".coding-ethos/state/install-sync.json",
		TargetRepoRoot:    "/repo",
		RequestedAction:   "agent-hooks sync",
		RuntimeVersion:    "1.2.3",
		RuntimeCommit:     "abc1234",
		LastValidationUTC: "2026-02-03T04:05:06Z",
		ProviderTargets: []ProviderTarget{
			{Provider: "agent-hooks", Root: "/repo"},
		},
		Sources: []SourceReport{
			{
				Path:           "coding_ethos.yml",
				Status:         sourceStatusCurrent,
				ExpectedSHA256: "sha256:source",
				ActualSHA256:   "sha256:source",
			},
		},
		Artifacts: []ArtifactReport{
			{
				Path:                ".codex/config.toml",
				Provider:            "agent-hooks",
				Surface:             "codex-config",
				Ownership:           DefaultOwnership,
				Status:              artifactStatusMissing,
				Plan:                "write",
				ExpectedSHA256:      "sha256:expected",
				VerificationCommand: "bin/coding-ethos-run agent-hooks doctor",
			},
		},
		PlannedWriteCount: 1,
	}

	if got, ok := report.MarshalFeedbackJSON().(Report); !ok || got.Tool != report.Tool {
		t.Fatalf("json feedback = %#v", got)
	}

	toon := report.MarshalFeedbackTOON()
	for _, want := range []string{
		"tool: install-sync-doctor",
		"provider_targets[1]{provider,root}:",
		"sources[1]{path,status,expected_sha256,actual_sha256}:",
		"artifacts[1]{path,provider,surface,status,ownership,plan,expected_sha256,actual_sha256,verification_command}:",
		"planned_write_count: 1",
	} {
		if !strings.Contains(toon, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, toon)
		}
	}

	if human := report.MarshalFeedbackHuman(); human != toon {
		t.Fatalf("human feedback differs from TOON:\n%s", human)
	}

	sarif := report.MarshalFeedbackSARIF()
	if sarif.Version == "" || len(sarif.Runs) != 1 ||
		len(sarif.Runs[0].Results) != 1 {
		t.Fatalf("SARIF feedback = %#v", sarif)
	}

	fields := report.FeedbackLogFields()
	if fields["tool"] != report.Tool ||
		fields["status"] != report.Status ||
		fields["planned_write_count"] != report.PlannedWriteCount {
		t.Fatalf("log fields = %#v", fields)
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

func TestRuntimeVersionUsesProjectTable(t *testing.T) {
	t.Parallel()

	ethosRoot := t.TempDir()
	writeTestFile(
		t,
		filepath.Join(ethosRoot, "pyproject.toml"),
		"[tool.example]\nversion = \"0.0.1\"\n\n[project]\nversion = \"2.3.4\"\n",
	)

	if got := runtimeVersion(ethosRoot); got != "2.3.4" {
		t.Fatalf("runtime version = %q", got)
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
