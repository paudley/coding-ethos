// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package tokeneconomy

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
	"github.com/yuin/goldmark"
)

func TestHistoricalReportLabelsGrossReductionNonCausal(t *testing.T) {
	t.Parallel()

	path := createHistoricalFixture(t)
	report, err := HistoricalReport(
		context.Background(),
		HistoricalReportOptions{
			DatabasePaths: []string{path},
			FromUTC:       "2026-08-01T00:00:00Z",
			ToUTC:         "2026-09-01T00:00:00Z",
		},
		time.Unix(1, 0),
	)
	if err != nil {
		t.Fatalf("historical report: %v", err)
	}

	if report.Causal || report.Conclusion != ConclusionObservational {
		t.Fatalf("historical report overclaimed: %#v", report)
	}
	if report.Historical == nil ||
		report.Historical.RawContextTokens != 300 ||
		report.Historical.DeliveredContextTokens != 130 ||
		report.Historical.AvoidedContextTokens != 170 {
		t.Fatalf("unexpected historical metrics: %#v", report.Historical)
	}
	if report.Historical.ReducedEvents != 1 || report.Historical.ExpandedEvents != 1 {
		t.Fatalf("unexpected transform direction counts: %#v", report.Historical)
	}
	if report.Historical.FromUTC != "2026-08-01T00:00:00Z" ||
		report.Historical.ToUTC != "2026-09-01T00:00:00Z" ||
		len(report.Historical.Sources) != 1 ||
		!report.Historical.Sources[0].SourceUnchanged ||
		report.Historical.Sources[0].SHA256Before !=
			report.Historical.Sources[0].SHA256After {
		t.Fatalf("historical provenance is incomplete: %#v", report.Historical)
	}
}

func TestHistoricalReportAggregatesSourcesDeterministicallyWithinWindow(t *testing.T) {
	t.Parallel()

	firstPath := createHistoricalFixture(t)
	secondPath := createSingleHistoricalFixture(
		t,
		"claude",
		"session-claude",
		"event-claude",
		"2026-08-20T12:00:00Z",
		100,
		40,
	)
	now := time.Unix(123, 0)
	first, err := HistoricalReport(
		context.Background(),
		HistoricalReportOptions{
			DatabasePaths: []string{secondPath, firstPath},
			FromUTC:       "2026-08-01T00:00:00-06:00",
			ToUTC:         "2026-09-01T00:00:00-06:00",
		},
		now,
	)
	if err != nil {
		t.Fatalf("aggregate historical sources: %v", err)
	}

	second, err := HistoricalReport(
		context.Background(),
		HistoricalReportOptions{
			DatabasePaths: []string{firstPath, secondPath},
			FromUTC:       "2026-08-01T06:00:00Z",
			ToUTC:         "2026-09-01T06:00:00Z",
		},
		now,
	)
	if err != nil {
		t.Fatalf("repeat aggregate historical sources: %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("historical aggregation depends on input order:\n%#v\n%#v", first, second)
	}
	if first.Historical == nil || first.Historical.RawContextTokens != 400 ||
		first.Historical.DeliveredContextTokens != 170 ||
		first.Historical.ProxySessions != 2 ||
		!slices.Equal(first.Coverage.Providers, []Provider{ProviderClaude, ProviderCodex}) {
		t.Fatalf("unexpected aggregate historical report: %#v", first)
	}
	if first.Historical.Sources[0].Path > first.Historical.Sources[1].Path {
		t.Fatalf(
			"historical sources are not canonical-path ordered: %#v",
			first.Historical.Sources,
		)
	}
}

func TestHistoricalReportRejectsDuplicateEventIDsAcrossSources(t *testing.T) {
	t.Parallel()

	firstPath := createSingleHistoricalFixture(
		t,
		"codex",
		"session-one",
		"shared-event",
		"2026-08-10T00:00:00Z",
		100,
		50,
	)
	secondPath := createSingleHistoricalFixture(
		t,
		"codex",
		"session-two",
		"shared-event",
		"2026-08-11T00:00:00Z",
		80,
		40,
	)

	_, err := HistoricalReport(
		context.Background(),
		HistoricalReportOptions{
			DatabasePaths: []string{firstPath, secondPath},
			FromUTC:       "2026-08-01T00:00:00Z",
			ToUTC:         "2026-09-01T00:00:00Z",
		},
		time.Unix(1, 0),
	)
	if err == nil || !strings.Contains(err.Error(), "shared-event") ||
		!strings.Contains(err.Error(), firstPath) ||
		!strings.Contains(err.Error(), secondPath) {
		t.Fatalf("expected duplicate event diagnostic with both sources, got %v", err)
	}
}

func TestHistoricalReportRequiresOrderedExplicitWindow(t *testing.T) {
	t.Parallel()

	path := createHistoricalFixture(t)
	for _, options := range []HistoricalReportOptions{
		{DatabasePaths: []string{path}, ToUTC: "2026-09-01T00:00:00Z"},
		{
			DatabasePaths: []string{path},
			FromUTC:       "2026-09-01T00:00:00Z",
			ToUTC:         "2026-08-01T00:00:00Z",
		},
	} {
		if _, err := HistoricalReport(
			context.Background(),
			options,
			time.Unix(1, 0),
		); err == nil {
			t.Fatalf("invalid historical window unexpectedly succeeded: %#v", options)
		}
	}
}

func TestHistoricalReportUsesHalfOpenWindow(t *testing.T) {
	t.Parallel()

	includedPath := createSingleHistoricalFixture(
		t,
		"codex",
		"included-session",
		"included-event",
		"2026-08-01T00:00:00Z",
		100,
		25,
	)
	excludedPath := createSingleHistoricalFixture(
		t,
		"codex",
		"excluded-session",
		"excluded-event",
		"2026-09-01T00:00:00Z",
		900,
		800,
	)

	report, err := HistoricalReport(
		context.Background(),
		HistoricalReportOptions{
			DatabasePaths: []string{excludedPath, includedPath},
			FromUTC:       "2026-08-01T00:00:00Z",
			ToUTC:         "2026-09-01T00:00:00Z",
		},
		time.Unix(1, 0),
	)
	if err != nil {
		t.Fatalf("report half-open historical window: %v", err)
	}
	if report.Historical == nil || report.Historical.RawContextTokens != 100 ||
		report.Historical.DeliveredContextTokens != 25 ||
		report.Historical.TransformedEvents != 1 ||
		report.Historical.ProxySessions != 1 {
		t.Fatalf("historical window did not use [from,to): %#v", report.Historical)
	}
}

func TestVerifyHistoricalSourcesRejectsChangedSource(t *testing.T) {
	t.Parallel()

	path := createHistoricalFixture(t)
	_, sources, err := prepareHistoricalReport(HistoricalReportOptions{
		DatabasePaths: []string{path},
		FromUTC:       "2026-08-01T00:00:00Z",
		ToUTC:         "2026-09-01T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("prepare historical source: %v", err)
	}
	if err = os.WriteFile(path, []byte("changed source\n"), 0o600); err != nil {
		t.Fatalf("change historical source fixture: %v", err)
	}

	_, err = verifyHistoricalSources(sources)
	if err == nil || !strings.Contains(err.Error(), path) ||
		!strings.Contains(err.Error(), "changed while reporting") {
		t.Fatalf("expected changed-source rejection, got %v", err)
	}
}

func TestExperimentReportClaimsSavingsOnlyWithQualityAndCoverage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("experiment-1")
	experiment.AnalysisBlockCheckpoints = []int{minimumCausalTaskCount, 20}
	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}

	for index := 0; index < minimumCausalTaskCount; index++ {
		taskID := "task-" + string(rune('a'+index))
		if err = store.RecordTask(
			ctx,
			testTask(experiment.ExperimentID, taskID),
		); err != nil {
			t.Fatalf("record task: %v", err)
		}

		for _, armTokens := range []struct {
			arm    Arm
			tokens int64
		}{{ArmFull, 50}, {ArmStatic, 75}, {ArmOff, 100}} {
			run := testRun(
				experiment.ExperimentID,
				taskID,
				armTokens.arm,
				1,
				armTokens.tokens,
				true,
			)
			run.RunID = taskID + "-" + string(armTokens.arm)
			if err = store.RecordRun(ctx, run); err != nil {
				t.Fatalf("record run: %v", err)
			}
		}
	}

	report, err := store.ExperimentReport(ctx, experiment.ExperimentID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("experiment report: %v", err)
	}
	if !report.Causal || report.Conclusion != ConclusionCausalSavings {
		t.Fatalf("unexpected experiment conclusion: %#v", report)
	}

	primary, found := comparisonFor(report.Comparisons, ArmFull, ArmOff)
	if !found || primary.SavingsPercent != 50 || !primary.PrecisionMet ||
		primary.ConfidenceLevelPercent != 97.5 {
		t.Fatalf("unexpected primary comparison: %#v", primary)
	}
}

func TestExperimentReportRejectsPartialTaskBlocks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("partial-blocks")
	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}
	for index := range minimumCausalTaskCount {
		taskID := "partial-task-" + string(rune('a'+index))
		if err = store.RecordTask(
			ctx,
			testTask(experiment.ExperimentID, taskID),
		); err != nil {
			t.Fatalf("record task: %v", err)
		}
		arms := []Arm{ArmFull, ArmStatic, ArmOff}
		if index == 0 {
			arms = []Arm{ArmFull, ArmOff}
		}
		for _, arm := range arms {
			run := testRun(experiment.ExperimentID, taskID, arm, 1, 100, true)
			run.RunID = taskID + "-" + string(arm)
			if err = store.RecordRun(ctx, run); err != nil {
				t.Fatalf("record run: %v", err)
			}
		}
	}

	report, err := store.ExperimentReport(ctx, experiment.ExperimentID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("experiment report: %v", err)
	}
	if report.Causal || report.Coverage.TaskCount != minimumCausalTaskCount ||
		report.Coverage.CompleteTaskCount != minimumCausalTaskCount-1 ||
		report.Coverage.CompleteBlockCount != minimumCausalTaskCount-1 {
		t.Fatalf("partial task blocks supported a causal claim: %#v", report)
	}
}

func TestExperimentReportRejectsPartialBlockAfterCheckpoint(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("partial-after-checkpoint")
	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}
	for index := 0; index <= minimumCausalTaskCount; index++ {
		taskID := "interrupted-task-" + string(rune('a'+index))
		if err = store.RecordTask(
			ctx,
			testTask(experiment.ExperimentID, taskID),
		); err != nil {
			t.Fatalf("record task: %v", err)
		}
		arms := []Arm{ArmFull, ArmStatic, ArmOff}
		if index == minimumCausalTaskCount {
			arms = []Arm{ArmFull}
		}
		for _, arm := range arms {
			run := testRun(experiment.ExperimentID, taskID, arm, 1, 100, true)
			run.RunID = taskID + "-" + string(arm)
			if err = store.RecordRun(ctx, run); err != nil {
				t.Fatalf("record run: %v", err)
			}
		}
	}

	report, err := store.ExperimentReport(ctx, experiment.ExperimentID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("experiment report: %v", err)
	}
	if report.Causal || report.Coverage.CompleteBlockCount != minimumCausalTaskCount ||
		report.Coverage.PartialBlockCount != 1 {
		t.Fatalf("partial post-checkpoint block supported a causal claim: %#v", report)
	}
}

func TestExperimentReportRequiresPredeclaredCompleteBlockLook(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("between-looks")
	experiment.AnalysisBlockCheckpoints = []int{minimumCausalTaskCount, 20}
	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}
	for index := range minimumCausalTaskCount {
		taskID := "look-task-" + string(rune('a'+index))
		if err = store.RecordTask(
			ctx,
			testTask(experiment.ExperimentID, taskID),
		); err != nil {
			t.Fatalf("record task: %v", err)
		}
		for _, arm := range []Arm{ArmFull, ArmStatic, ArmOff} {
			for replicate := 1; replicate <= 2; replicate++ {
				if index != 0 && replicate == 2 {
					continue
				}
				run := testRun(experiment.ExperimentID, taskID, arm, replicate, 100, true)
				run.RunID = fmt.Sprintf("%s-%s-%d", taskID, arm, replicate)
				if err = store.RecordRun(ctx, run); err != nil {
					t.Fatalf("record run: %v", err)
				}
			}
		}
	}

	report, err := store.ExperimentReport(ctx, experiment.ExperimentID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("experiment report: %v", err)
	}
	if report.Causal || report.Coverage.CompleteTaskCount != minimumCausalTaskCount ||
		report.Coverage.CompleteBlockCount != minimumCausalTaskCount+1 {
		t.Fatalf("between-checkpoint evidence supported a causal claim: %#v", report)
	}
}

func TestExperimentReportDoesNotTreatZeroAcceptedRunsAsPrecise(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "token-economy.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	experiment := testExperiment("zero-accepted")
	if err = store.RecordExperiment(ctx, experiment); err != nil {
		t.Fatalf("record experiment: %v", err)
	}
	for index := range minimumCausalTaskCount {
		taskID := "rejected-task-" + string(rune('a'+index))
		if err = store.RecordTask(
			ctx,
			testTask(experiment.ExperimentID, taskID),
		); err != nil {
			t.Fatalf("record task: %v", err)
		}
		for _, arm := range []Arm{ArmFull, ArmStatic, ArmOff} {
			accepted := arm != ArmFull
			run := testRun(experiment.ExperimentID, taskID, arm, 1, 100, accepted)
			run.RunID = taskID + "-" + string(arm)
			if err = store.RecordRun(ctx, run); err != nil {
				t.Fatalf("record run: %v", err)
			}
		}
	}

	report, err := store.ExperimentReport(ctx, experiment.ExperimentID, time.Unix(1, 0))
	if err != nil {
		t.Fatalf("experiment report: %v", err)
	}
	primary, found := comparisonFor(report.Comparisons, ArmFull, ArmOff)
	if !found || primary.PrecisionMet || primary.QualityNonInferior ||
		report.Conclusion != ConclusionInconclusive {
		t.Fatalf("zero accepted treatment runs appeared conclusive: %#v", report)
	}
}

func TestComparisonConclusionRequiresPrecisionForDirectionalClaims(t *testing.T) {
	t.Parallel()

	for name, interval := range map[string]Interval{
		"savings":    {Lower: 0.1, Upper: 100},
		"regression": {Lower: -100, Upper: -0.1},
	} {
		comparison := Comparison{
			SavingsPercentInterval: interval,
			QualityNonInferior:     true,
		}
		if conclusion := comparisonConclusion(
			comparison,
		); conclusion != ConclusionInconclusive {
			t.Errorf("imprecise %s conclusion = %q, want inconclusive", name, conclusion)
		}
	}
}

func TestWriteReportArtifactsIsCreateNewAndVerifiable(t *testing.T) {
	t.Parallel()

	prefix := filepath.Join(t.TempDir(), "report")
	report := Report{
		Kind:           ReportKind,
		Cohort:         "historical",
		GeneratedAtUTC: "2026-08-28T00:00:00Z",
		Conclusion:     ConclusionInconclusive,
		SchemaVersion:  SchemaVersion,
		Coverage:       Coverage{},
		Provenance:     map[string]string{},
	}

	artifacts, err := WriteReportArtifacts(report, prefix)
	if err != nil {
		t.Fatalf("write report artifacts: %v", err)
	}
	for _, path := range []string{
		artifacts.JSONPath,
		artifacts.MarkdownPath,
		artifacts.ReceiptPath,
	} {
		info, statErr := os.Stat(path)
		if statErr != nil {
			t.Fatalf("stat artifact %s: %v", path, statErr)
		}
		if info.Mode().Perm() != storeFileMode {
			t.Fatalf("artifact %s mode = %o", path, info.Mode().Perm())
		}
	}
	if len(artifacts.JSONSHA256) != 64 || len(artifacts.MarkdownSHA256) != 64 {
		t.Fatalf("artifact digests are incomplete: %#v", artifacts)
	}

	_, err = WriteReportArtifacts(report, prefix)
	if err == nil {
		t.Fatal("second create-new artifact write unexpectedly succeeded")
	}
}

func TestWriteExclusiveArtifactRemovesPartialFileAfterWriteFailure(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "partial.json")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, storeFileMode)
	if err != nil {
		t.Fatalf("create partial artifact fixture: %v", err)
	}
	if err = file.Close(); err != nil {
		t.Fatalf("close partial artifact fixture: %v", err)
	}

	err = writeExclusiveArtifactFile(path, file, []byte("payload"))
	if err == nil {
		t.Fatal("write through a closed artifact unexpectedly succeeded")
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("partial artifact survived failed write: %v", statErr)
	}
}

func TestFormatReportMarkdownIncludesVerifiedContextAndComparisonQuality(t *testing.T) {
	t.Parallel()

	report := Report{
		Cohort:         "cohort-a",
		GeneratedAtUTC: "2026-08-29T21:00:00Z",
		Conclusion:     ConclusionCausalSavings,
		Causal:         true,
		Historical: &HistoricalMetrics{
			FromUTC:                "2026-08-01T00:00:00Z",
			ToUTC:                  "2026-09-01T00:00:00Z",
			RawContextTokens:       1000,
			DeliveredContextTokens: 600,
			AvoidedContextTokens:   400,
			GrossReductionPercent:  40,
			Sources: []HistoricalSource{{
				Path:        "evidence`store.duckdb",
				SHA256After: "abc123",
			}},
		},
		Comparisons: []Comparison{
			{
				TreatmentArm:           ArmFull,
				ControlArm:             ArmOff,
				TaskCount:              2,
				SavingsPercent:         12.5,
				SavingsPercentInterval: Interval{Lower: 10, Upper: 15},
				ConfidenceLevelPercent: 95,
				QualityNonInferior:     true,
			},
			{
				TreatmentArm:           ArmStatic,
				ControlArm:             ArmOff,
				TaskCount:              1,
				SavingsPercent:         -2,
				SavingsPercentInterval: Interval{Lower: -5, Upper: 1},
				ConfidenceLevelPercent: 90,
			},
		},
		Coverage: Coverage{
			TaskCount:          3,
			CompleteTaskCount:  2,
			CompleteBlockCount: 4,
			PartialBlockCount:  1,
			RunCount:           9,
			AcceptedRunCount:   8,
			Reasons:            []string{"one incomplete\nblock"},
		},
	}

	markdown := formatReportMarkdown(report)
	for _, expected := range []string{
		"## Observational context reduction",
		"- ``evidence`store.duckdb``: `abc123` (unchanged)",
		"| full | off | 2 | 12.50% | 10.00% to 15.00% | 95.00% | non-inferior |",
		"| static | off | 1 | -2.00% | -5.00% to 1.00% | 90.00% | " +
			"not established |",
		"- one incomplete block",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("Markdown report missing %q:\n%s", expected, markdown)
		}
	}

	var rendered bytes.Buffer
	if err := goldmark.Convert([]byte(markdown), &rendered); err != nil {
		t.Fatalf("render Markdown report: %v", err)
	}
	if !strings.Contains(rendered.String(), "<code>evidence`store.duckdb</code>") {
		t.Fatalf(
			"verified source path did not round trip through Markdown: %s",
			rendered.String(),
		)
	}
}

func TestMarkdownCodeSpanRoundTripsDelimiterEdges(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"plain",
		"evidence`store.duckdb",
		"evidence``store.duckdb",
		"`boundary`",
		" leading-space",
		"trailing-space ",
	} {
		var rendered bytes.Buffer
		err := goldmark.Convert([]byte(markdownCodeSpan(value)), &rendered)
		if err != nil {
			t.Fatalf("render code span for %q: %v", value, err)
		}

		want := "<p><code>" + html.EscapeString(value) + "</code></p>\n"
		if rendered.String() != want {
			t.Fatalf("code span for %q rendered as %q, want %q", value, rendered.String(), want)
		}
	}
}

func TestReadRunMechanismsMeasuresFirstTransformsAndRepeatedAdvice(t *testing.T) {
	t.Parallel()

	missing, err := readRunMechanisms(
		context.Background(),
		filepath.Join(t.TempDir(), "missing.duckdb"),
		"session-1",
	)
	if err != nil || missing != (MechanismMetrics{}) {
		t.Fatalf("missing mechanism store = %#v, %v", missing, err)
	}

	path := filepath.Join(t.TempDir(), "mechanisms.duckdb")
	database, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open mechanism fixture: %v", err)
	}
	for _, statement := range []string{
		`CREATE TABLE proxy_events(
			event_id TEXT, session_id TEXT, event_kind TEXT,
			output_hash TEXT, output_tokens BIGINT
		)`,
		`CREATE TABLE proxy_transforms(
			event_id TEXT, ordinal INTEGER, input_tokens BIGINT
		)`,
		`INSERT INTO proxy_events VALUES
			('e1', 'session-1', 'payload_injection', 'same', 40),
			('e2', 'session-1', 'payload_injection', 'same', 20),
			('e3', 'session-1', 'response', '', 30),
			('outside', 'session-2', 'payload_injection', 'same', 999)`,
		`INSERT INTO proxy_transforms VALUES
			('e1', 0, 100), ('e1', 1, 200), ('e2', 0, 50),
			('e3', 0, 70), ('outside', 0, 5)`,
	} {
		if _, err = database.Exec(statement); err != nil {
			_ = database.Close()
			t.Fatalf("create mechanism fixture: %v", err)
		}
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close mechanism fixture: %v", err)
	}

	metrics, err := readRunMechanisms(context.Background(), path, "session-1")
	if err != nil {
		t.Fatalf("read mechanism evidence: %v", err)
	}
	want := MechanismMetrics{
		RawContextTokens:       220,
		DeliveredContextTokens: 90,
		AvoidedContextTokens:   130,
		InjectedGuidanceTokens: 60,
		RepeatedAdviceCount:    1,
		TransformEventCount:    3,
	}
	if metrics != want {
		t.Fatalf("mechanism metrics = %#v, want %#v", metrics, want)
	}
	clamped, err := readRunMechanisms(context.Background(), path, "session-2")
	if err != nil || clamped.RawContextTokens != 5 ||
		clamped.DeliveredContextTokens != 999 || clamped.AvoidedContextTokens != 0 {
		t.Fatalf(
			"negative avoided-token delta was not clamped: metrics=%#v error=%v",
			clamped,
			err,
		)
	}
}

func TestHistoricalAggregateTreatsExpansionAsZeroSavings(t *testing.T) {
	t.Parallel()

	aggregate := historicalAggregate{
		metrics: HistoricalMetrics{
			RawContextTokens:       5,
			DeliveredContextTokens: 9,
		},
	}
	metrics, _ := aggregate.finish(historicalWindow{}, nil)
	if metrics.AvoidedContextTokens != 0 || metrics.GrossReductionPercent != 0 {
		t.Fatalf("historical expansion reported negative savings: %#v", metrics)
	}
}

func createHistoricalFixture(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "code-intel.duckdb")
	database, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open historical fixture: %v", err)
	}

	statements := []string{
		`CREATE TABLE proxy_sessions(session_id TEXT, provider TEXT)`,
		`CREATE TABLE proxy_events(
			event_id TEXT, session_id TEXT, provider TEXT,
			recorded_at_utc TEXT, output_tokens BIGINT
		)`,
		`CREATE TABLE proxy_transforms(event_id TEXT, ordinal INTEGER, input_tokens BIGINT)`,
		`INSERT INTO proxy_sessions VALUES ('s1', 'codex'), ('outside', 'codex')`,
		`INSERT INTO proxy_events VALUES
			('e1', 's1', 'codex', '2026-08-10T00:00:00Z', 50),
			('e2', 's1', 'codex', '2026-08-11T00:00:00Z', 80),
			('outside', 'outside', 'codex', '2026-10-01T00:00:00Z', 1)`,
		`INSERT INTO proxy_transforms VALUES
			('e1', 0, 250), ('e1', 1, 100), ('e2', 0, 50), ('outside', 0, 999)`,
	}
	for _, statement := range statements {
		if _, err = database.Exec(statement); err != nil {
			_ = database.Close()
			t.Fatalf("create historical fixture: %v", err)
		}
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close historical fixture: %v", err)
	}

	return path
}

func createSingleHistoricalFixture(
	t *testing.T,
	provider string,
	sessionID string,
	eventID string,
	recordedAtUTC string,
	inputTokens int64,
	outputTokens int64,
) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "code-intel.duckdb")
	database, err := sql.Open("duckdb", path)
	if err != nil {
		t.Fatalf("open single historical fixture: %v", err)
	}

	for _, statement := range []string{
		`CREATE TABLE proxy_sessions(session_id TEXT, provider TEXT)`,
		`CREATE TABLE proxy_events(
			event_id TEXT, session_id TEXT, provider TEXT,
			recorded_at_utc TEXT, output_tokens BIGINT
		)`,
		`CREATE TABLE proxy_transforms(event_id TEXT, ordinal INTEGER, input_tokens BIGINT)`,
	} {
		if _, err = database.Exec(statement); err != nil {
			_ = database.Close()
			t.Fatalf("create single historical fixture schema: %v", err)
		}
	}

	if _, err = database.Exec(
		"INSERT INTO proxy_sessions VALUES (?, ?)",
		sessionID,
		provider,
	); err != nil {
		_ = database.Close()
		t.Fatalf("insert single historical session: %v", err)
	}
	if _, err = database.Exec(
		"INSERT INTO proxy_events VALUES (?, ?, ?, ?, ?)",
		eventID,
		sessionID,
		provider,
		recordedAtUTC,
		outputTokens,
	); err != nil {
		_ = database.Close()
		t.Fatalf("insert single historical event: %v", err)
	}
	if _, err = database.Exec(
		"INSERT INTO proxy_transforms VALUES (?, 0, ?)",
		eventID,
		inputTokens,
	); err != nil {
		_ = database.Close()
		t.Fatalf("insert single historical transform: %v", err)
	}
	if err = database.Close(); err != nil {
		t.Fatalf("close single historical fixture: %v", err)
	}

	return path
}
