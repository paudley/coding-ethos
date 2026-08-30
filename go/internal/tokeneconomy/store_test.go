// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package tokeneconomy

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestStoreRecordsImmutableExperimentTaskAndRun(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("experiment-1")
	task := testTask(experiment.ExperimentID, "task-1")
	run := testRun(experiment.ExperimentID, task.TaskID, ArmFull, 1, 100, true)

	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}
	if err = store.RecordTask(ctx, task); err != nil {
		t.Fatalf("record task: %v", err)
	}
	if err = store.RecordRun(ctx, run); err != nil {
		t.Fatalf("record run: %v", err)
	}
	if err = store.RecordRun(ctx, run); err != nil {
		t.Fatalf("idempotent run record: %v", err)
	}

	runs, err := store.Runs(ctx, experiment.ExperimentID)
	if err != nil {
		t.Fatalf("query runs: %v", err)
	}
	if len(runs) != 1 || runs[0].Usage.TotalTokens != 100 {
		t.Fatalf("unexpected stored runs: %#v", runs)
	}

	run.Usage.TotalTokens = 101
	err = store.RecordRun(ctx, run)
	if err == nil || !strings.Contains(err.Error(), "conflicts with stored evidence") {
		t.Fatalf("expected immutable run conflict, got %v", err)
	}
}

func TestStoreRejectsRunWithoutRegisteredTask(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("experiment-1")
	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}

	err = store.RecordRun(
		ctx,
		testRun(experiment.ExperimentID, "missing", ArmFull, 1, 100, true),
	)
	if err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("expected missing task failure, got %v", err)
	}
}

func testExperiment(id string) Experiment {
	return Experiment{
		ExperimentID:             id,
		ManifestSHA256:           strings.Repeat("a", 64),
		ProtocolSHA256:           strings.Repeat("b", 64),
		Provider:                 ProviderCodex,
		Model:                    "gpt-test",
		RuntimeVersion:           "codex-test",
		CreatedAtUTC:             "2026-08-28T00:00:00Z",
		RandomizationSeed:        "seed-1",
		Status:                   "registered",
		AnalysisBlockCheckpoints: []int{minimumCausalTaskCount},
		Randomized:               true,
		ArmIsolationVerified:     true,
	}
}

func testTask(experimentID, taskID string) Task {
	return Task{
		ExperimentID:    experimentID,
		TaskID:          taskID,
		Kind:            "diagnostic",
		SourceSHA256:    strings.Repeat("c", 64),
		PromptSHA256:    strings.Repeat("d", 64),
		ValidatorSHA256: strings.Repeat("e", 64),
	}
}

func testRun(
	experimentID string,
	taskID string,
	arm Arm,
	replicate int,
	totalTokens int64,
	accepted bool,
) Run {
	return Run{
		RunID:                   string(arm) + "-" + taskID,
		ExperimentID:            experimentID,
		TaskID:                  taskID,
		Arm:                     arm,
		Provider:                ProviderCodex,
		Model:                   "gpt-test",
		ProviderSessionID:       "session-1",
		LedgerSHA256:            strings.Repeat("f", 64),
		ValidationReceiptSHA256: strings.Repeat("1", 64),
		StartedAtUTC:            "2026-08-28T00:00:00Z",
		CompletedAtUTC:          "2026-08-28T00:01:00Z",
		Status:                  "completed",
		Replicate:               replicate,
		DurationMilliseconds:    60_000,
		Accepted:                accepted,
		Usage:                   TokenUsage{TotalTokens: totalTokens},
		UsageEvents: []UsageEvent{{
			Ordinal:   0,
			UsageKind: "cumulative",
			Usage:     TokenUsage{TotalTokens: totalTokens},
		}},
	}
}
