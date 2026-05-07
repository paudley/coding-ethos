// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	. "blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

const (
	codeChunkRecordKind = "code_chunk"
	vectorBackendName   = "sqlite-vec"
)

func TestStoreIngestsLintTracesAndReportsRepeatedFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	first := lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z")
	second := lintTracePayload(t, "trace-b.json", "2026-01-01T00:01:00Z")

	inlineErr0 := ingester.IngestLintTrace(ctx, first)
	if inlineErr0 != nil {
		t.Fatalf("ingest first trace: %v", inlineErr0)
	}

	inlineErr1 := ingester.IngestLintTrace(ctx, second)
	if inlineErr1 != nil {
		t.Fatalf("ingest second trace: %v", inlineErr1)
	}

	repeated, err := store.RepeatedFailures(ctx, RepeatedFailureQuery{
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Path:     "pkg/app.py",
	})
	if err != nil {
		t.Fatalf("query repeated failures: %v", err)
	}

	if len(repeated) != 1 {
		t.Fatalf("repeated failures = %#v", repeated)
	}

	if repeated[0].TraceCount != 2 || repeated[0].Count != 2 {
		t.Fatalf("repeated count = %#v", repeated[0])
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertStats(t, stats, Stats{
		Traces:            2,
		Findings:          1,
		Remediations:      1,
		RemediationEvents: 2,
		FtsRows:           4,
	})
}

func TestStoreSearchesRemediationText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	ingester := NewTraceIngester(store)

	inlineErr2 := ingester.IngestLintTrace(
		ctx,
		lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z"),
	)
	if inlineErr2 != nil {
		t.Fatalf("ingest trace: %v", inlineErr2)
	}

	results, err := store.Search(ctx, SearchQuery{Text: "unused", Limit: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}

	if len(results) == 0 {
		t.Fatalf("expected search results")
	}

	if results[0].TraceID != "trace-a.json" {
		t.Fatalf("search result = %#v", results[0])
	}
}

func TestStoreIngestTraceDirsFindsLintAndHookTraces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	ingester := NewTraceIngester(store)

	writeFile(
		t,
		filepath.Join(root, ".coding-ethos", "lint-runs", "trace-a.json"),
		lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z"),
	)
	writeFile(
		t,
		filepath.Join(root, ".coding-ethos", "hook-runs", "run-a", "event.json"),
		hookTracePayload(t),
	)

	summary, err := ingester.IngestTraceDirs(ctx, root)
	if err != nil {
		t.Fatalf("ingest trace dirs: %v", err)
	}

	if summary.FilesScanned != 2 || summary.FilesIngested != 2 {
		t.Fatalf("summary = %#v", summary)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.Traces != 2 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStoreIngestTraceDirsBackfillsMissingTraceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStoreAt(t, ctx, DefaultDBPath(root))
	ingester := NewTraceIngester(store)

	writeFile(
		t,
		filepath.Join(root, ".coding-ethos", "hook-runs", "run-a", "event.json"),
		hookTracePayloadWithIDs(t, "", "deny-hook-a", "2026-01-01T00:02:00Z"),
	)

	summary, err := ingester.IngestTraceDirs(ctx, root)
	if err != nil {
		t.Fatalf("ingest trace dirs: %v", err)
	}

	if summary.FilesScanned != 1 || summary.FilesIngested != 1 {
		t.Fatalf("summary = %#v", summary)
	}

	usage, err := store.HookUsage(ctx, HookUsageQuery{Limit: 10})
	if err != nil {
		t.Fatalf("query hook usage: %v", err)
	}

	if len(usage) != 1 ||
		!strings.HasPrefix(usage[0].LastTraceID, "source-run-a-event.json-") {
		t.Fatalf("hook usage trace fallback = %#v", usage)
	}
}

func TestStoreIngestsHookUsageAnalytics(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	ingester := NewTraceIngester(store)

	inlineErr3 := ingester.IngestHookTrace(ctx, hookTracePayload(t))
	if inlineErr3 != nil {
		t.Fatalf("ingest hook trace: %v", inlineErr3)
	}

	usage, err := store.HookUsage(ctx, HookUsageQuery{
		RiskCategory: "bypass",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("query hook usage: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("hook usage = %#v", usage)
	}

	assertHookUsageSummary(t, usage[0])

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertStats(t, stats, Stats{
		HookEvents:    1,
		HookDecisions: 1,
		HookTargets:   1,
		FtsRows:       3,
	})
}

func assertHookUsageSummary(t *testing.T, usage HookUsageSummary) {
	t.Helper()

	got := []any{
		usage.EventCount,
		usage.BlockedCount,
		usage.PolicyID,
		usage.OperationKind,
		usage.TargetKind,
		usage.LastTrackingID,
	}

	want := []any{
		1,
		1,
		"git.wrapper_required",
		"git_status",
		"source_file",
		"deny-hook-a",
	}
	if !stringAnySlicesEqual(got, want) {
		t.Fatalf("hook usage summary = %#v", usage)
	}
}

func TestHookUsageLastIDsComeFromLatestEvent(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	older := hookTracePayloadWithIDs(
		t,
		"hook-z",
		"deny-z",
		"2026-01-01T00:01:00Z",
	)

	newer := hookTracePayloadWithIDs(
		t,
		"hook-a",
		"deny-a",
		"2026-01-01T00:02:00Z",
	)

	inlineErr4 := ingester.IngestHookTrace(ctx, older)
	if inlineErr4 != nil {
		t.Fatalf("ingest older hook trace: %v", inlineErr4)
	}

	inlineErr5 := ingester.IngestHookTrace(ctx, newer)
	if inlineErr5 != nil {
		t.Fatalf("ingest newer hook trace: %v", inlineErr5)
	}

	usage, err := store.HookUsage(ctx, HookUsageQuery{
		RiskCategory: "bypass",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("query hook usage: %v", err)
	}

	if len(usage) != 1 {
		t.Fatalf("hook usage = %#v", usage)
	}

	if usage[0].EventCount != 2 ||
		usage[0].LastSeenUTC != "2026-01-01T00:02:00Z" ||
		usage[0].LastTraceID != "hook-a" ||
		usage[0].LastTrackingID != "deny-a" {
		t.Fatalf("latest IDs not tied to newest event: %#v", usage[0])
	}
}

func TestStoreRecordsHookReviews(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr6 := NewTraceIngester(
		store,
	).IngestHookTrace(ctx, hookTracePayload(t))
	if inlineErr6 != nil {
		t.Fatalf("ingest hook trace: %v", inlineErr6)
	}

	inlineErr7 := store.RecordHookReview(ctx, HookReview{
		TraceID:       "hook-trace-a",
		TrackingID:    "deny-hook-a",
		Disposition:   "false_positive",
		Reviewer:      "admin",
		Notes:         "memory path should be allowed",
		RecordedAtUTC: "2026-01-01T00:03:00Z",
	})
	if inlineErr7 != nil {
		t.Fatalf("record hook review: %v", inlineErr7)
	}

	reviews, err := store.HookReviews(ctx, HookReviewQuery{
		Disposition: "false_positive",
	})
	if err != nil {
		t.Fatalf("query hook reviews: %v", err)
	}

	if len(reviews) != 1 || reviews[0].TrackingID != "deny-hook-a" ||
		reviews[0].Notes != "memory path should be allowed" {
		t.Fatalf("reviews = %#v", reviews)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.HookReviews != 1 || stats.FtsRows != 4 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStoreIngestsSARIFResultsWithCELProvenance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	payload := sarifPayload(t)

	inlineErr8 := NewTraceIngester(
		store,
	).IngestSARIF(ctx, "policy.sarif", payload)
	if inlineErr8 != nil {
		t.Fatalf("ingest SARIF: %v", inlineErr8)
	}

	results, err := store.SARIFResults(ctx, SARIFResultQuery{
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Path:     "pkg/app.py",
	})
	if err != nil {
		t.Fatalf("query SARIF results: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("SARIF results = %#v", results)
	}

	result := results[0]
	if result.EvaluatorKind != "cel" ||
		result.CELExpression != "finding.policy_id == 'python.unused_imports'" {
		t.Fatalf("SARIF CEL provenance = %#v", result)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.SARIFRuns != 1 || stats.SARIFResults != 1 || stats.FtsRows != 1 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestDecodeSARIFRunsMergesRuleResultAndFindingMetadata(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
		"version":"2.1.0",
		"runs":[{
			"automationDetails":{"id":"policy","guid":"run-guid"},
			"baselineGuid":"base-guid",
			"properties":{"scope":"staged"},
			"tool":{"driver":{"name":"coding-ethos","rules":[{
				"id":"R1",
				"properties":{
					"policy_id":"rule.policy",
					"skill_id":"rule-skill",
					"source_tool":"ruff",
					"ethos_ids":["static-analysis"],
					"advice":"rule advice"
				}
			}]}},
			"results":[{
				"ruleId":"R1",
				"level":"error",
				"message":{"text":"result message"},
				"locations":[{
					"physicalLocation":{
						"artifactLocation":{"uri":"pkg/app.py"},
						"region":{"startLine":7,"startColumn":3}
					}
				}],
				"partialFingerprints":{"primaryLocationLineHash":"line-hash"},
				"properties":{
					"finding":{
						"id":"finding-1",
						"policy_id":"finding.policy",
						"skill_id":"finding-skill",
						"evaluator_kind":"cel",
						"search_text":"finding search",
						"principle_ids":["no-conditional-imports"]
					},
					"agent_remediation":[{
						"id":"rem-1",
						"policy_id":"finding.policy",
						"skill_id":"finding-skill",
						"message":"Move import",
						"advice":"Use module scope",
						"file":"pkg/app.py"
					}],
					"policy_id":"result.policy",
					"skill_id":"result-skill",
					"implementation":"cel",
					"cel_expression":"diagnostic.code == 'PLC0415'",
					"policy_source":"coding_ethos.yml:principles.3",
					"ast_language":"python",
					"ast_node_kind":"import_statement",
					"ast_symbol_kind":"import",
					"ast_symbol_name":"plugin",
					"ast_symbol_path":"plugin",
					"ethos_ids":["conditional-imports"]
				}
			}]
		}]
	}`)

	run, err := DecodeSARIFRun("policy.sarif", payload)
	if err != nil {
		t.Fatalf("decode SARIF run: %v", err)
	}

	assertDecodedSARIFRunMetadata(t, run)

	if len(run.Results) != 1 {
		t.Fatalf("results = %#v", run.Results)
	}

	assertDecodedSARIFResultMetadata(t, run.Results[0])
}

func assertDecodedSARIFRunMetadata(t *testing.T, run SARIFRun) {
	t.Helper()

	got := []any{
		run.SourcePath,
		run.Category,
		run.ToolName,
		run.AutomationID,
		run.RunGUID,
		run.BaselineGUID,
	}

	want := []any{
		"policy.sarif",
		"staged",
		"coding-ethos",
		"policy",
		"run-guid",
		"base-guid",
	}
	if !stringAnySlicesEqual(got, want) {
		t.Fatalf("run metadata = %#v", run)
	}
}

func assertDecodedSARIFResultMetadata(t *testing.T, result SARIFResultReference) {
	t.Helper()

	got := []any{
		result.FindingID,
		result.RemediationID,
		result.PolicyID,
		result.SkillID,
		result.EvaluatorKind,
		result.CELExpression,
		result.PolicySource,
		result.Path,
		result.StartLine,
		result.StartColumn,
		result.Fingerprint,
		result.ASTLanguage,
		result.ASTSymbolPath,
		strings.Contains(result.SearchText, "Move import"),
		strings.Contains(result.SearchText, "Use module scope"),
		containsJoined(result.PrincipleIDs, "no-conditional-imports"),
		containsJoined(result.PrincipleIDs, "conditional-imports"),
	}

	want := []any{
		"finding-1",
		"rem-1",
		"result.policy",
		"result-skill",
		"cel",
		"diagnostic.code == 'PLC0415'",
		"coding_ethos.yml:principles.3",
		"pkg/app.py",
		7,
		3,
		"line-hash",
		"python",
		"plugin",
		true,
		true,
		true,
		true,
	}
	if !stringAnySlicesEqual(got, want) {
		t.Fatalf("result metadata = %#v", result)
	}
}

func TestDecodeSARIFRunsRejectsMalformedLogs(t *testing.T) {
	t.Parallel()

	_, inlineErrAutoA := DecodeSARIFRuns("bad.sarif", []byte("{"))
	if inlineErrAutoA == nil {
		t.Fatal("expected malformed SARIF decode error")
	}

	_, inlineErrAutoB := DecodeSARIFRuns(
		"empty.sarif",
		[]byte(`{"version":"2.1.0","runs":[]}`),
	)
	if inlineErrAutoB == nil {
		t.Fatal("expected no-runs SARIF error")
	}
}

func TestStoreQueriesMigratedSARIFResultsWithNullASTColumns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "code-intel.db")
	store := openTestStoreAt(t, ctx, path)
	rawDatabase := openRawSQLite(t, path)

	_, inlineErrA := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO sarif_runs(
			sarif_run_id, source_path, category, tool_name, raw_json
		) VALUES ('old-run', 'old.sarif', 'policy', 'coding-ethos', '{}')`,
	)
	if inlineErrA != nil {
		t.Fatalf("insert old SARIF run: %v", inlineErrA)
	}

	_, inlineErrB := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO sarif_results(
			sarif_result_id, sarif_run_id, ordinal, rule_id, message,
			level, fingerprint, finding_id, remediation_id, policy_id, skill_id,
			principle_ids, path, start_line, start_column, evaluator_kind,
			cel_policy_id, cel_expression, policy_source, search_text, raw_json
		) VALUES (
			'old-result', 'old-run', 0, 'old.rule', 'old message',
			'warning', '', '', '', '', '',
			'', 'pkg/app.py', 1, 1, '',
			'', '', '', 'old message', '{}'
		)`,
	)
	if inlineErrB != nil {
		t.Fatalf("insert old SARIF result: %v", inlineErrB)
	}

	results, err := store.SARIFResults(ctx, SARIFResultQuery{RunID: "old-run"})
	if err != nil {
		t.Fatalf("query old SARIF results: %v", err)
	}

	if len(results) != 1 || results[0].ASTLanguage != "" ||
		results[0].LinkedChunkID != "" {
		t.Fatalf("SARIF results = %#v", results)
	}
}

func TestStoreRecordsRemediationOutcomesAndEmbeddingMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	recordRemediationOutcomeFixture(t, ctx, store)
	assertRecordedRemediationOutcome(t, ctx, store)
	assertRecordedEmbeddingMetadata(t, ctx, store)

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertStats(t, stats, Stats{
		RemediationOutcomes: 1,
		EmbeddingRecords:    1,
		FtsRows:             6,
	})
}

func recordRemediationOutcomeFixture(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	ingester := NewTraceIngester(store)

	err := ingester.IngestLintTrace(
		ctx,
		lintTracePayload(t, "trace-a", "2026-01-01T00:00:00Z"),
	)
	if err != nil {
		t.Fatalf("ingest source trace: %v", err)
	}

	err = ingester.IngestLintTrace(
		ctx,
		lintTracePayload(t, "trace-b", "2026-01-01T00:01:00Z"),
	)
	if err != nil {
		t.Fatalf("ingest follow-up trace: %v", err)
	}

	err = store.RecordRemediationOutcome(ctx, RemediationOutcome{
		RemediationID:   "rem-1",
		FindingID:       "finding-1",
		SourceTraceID:   "trace-a",
		FollowupTraceID: "trace-b",
		PolicyID:        "python.unused_imports",
		SkillID:         "lint-remediation",
		Path:            "pkg/app.py",
		Provider:        "codex",
		Tool:            "Edit",
		Outcome:         "fixed",
		AttemptOrdinal:  1,
	})
	if err != nil {
		t.Fatalf("record outcome: %v", err)
	}

	err = store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:      vectorBackendName,
		Collection:   "remediations",
		ModelID:      "voyage-code-3",
		RecordKind:   "remediation_outcome",
		RecordID:     "rem-1",
		Dimension:    1024,
		PolicyID:     "python.unused_imports",
		SkillID:      "lint-remediation",
		Path:         "pkg/app.py",
		BackendRowID: "sqlite-vec-row-1",
	})
	if err != nil {
		t.Fatalf("record embedding: %v", err)
	}
}

func assertRecordedRemediationOutcome(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	outcomes, err := store.RemediationOutcomes(ctx, RemediationOutcomeQuery{
		Outcome: "fixed",
	})
	if err != nil {
		t.Fatalf("query outcomes: %v", err)
	}

	if len(outcomes) != 1 || outcomes[0].RemediationID != "rem-1" {
		t.Fatalf("outcomes = %#v", outcomes)
	}

	effectiveness, err := store.RemediationEffectiveness(ctx, RemediationOutcomeQuery{})
	if err != nil {
		t.Fatalf("effectiveness: %v", err)
	}

	if len(effectiveness) != 1 || effectiveness[0].Fixed != 1 ||
		effectiveness[0].Total != 1 {
		t.Fatalf("effectiveness = %#v", effectiveness)
	}
}

func assertRecordedEmbeddingMetadata(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	embeddingRecords, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		Backend: vectorBackendName,
		ModelID: "voyage-code-3",
	})
	if err != nil {
		t.Fatalf("embedding records: %v", err)
	}

	if len(embeddingRecords) != 1 ||
		embeddingRecords[0].BackendRowID != "sqlite-vec-row-1" {
		t.Fatalf("embedding records = %#v", embeddingRecords)
	}
}

func TestStoreReturnsEmbeddingCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr13 := NewTraceIngester(
		store,
	).IngestSARIF(ctx, "policy.sarif", sarifPayload(t))
	if inlineErr13 != nil {
		t.Fatalf("ingest SARIF: %v", inlineErr13)
	}

	inlineErr14 := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-1",
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
	})
	if inlineErr14 != nil {
		t.Fatalf("record outcome: %v", inlineErr14)
	}

	candidates, err := store.EmbeddingCandidates(ctx, EmbeddingCandidateQuery{
		PolicyID: "python.unused_imports",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("embedding candidates: %v", err)
	}

	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v", candidates)
	}

	for _, candidate := range candidates {
		if candidate.Text == "" ||
			candidate.Metadata["policy_id"] != "python.unused_imports" {
			t.Fatalf("candidate = %#v", candidate)
		}
	}
}

func TestStoreQueriesOutcomeWithoutTraceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr15 := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-no-trace",
		RemediationID: "rem-no-trace",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "attempted",
	})
	if inlineErr15 != nil {
		t.Fatalf("record outcome: %v", inlineErr15)
	}

	outcomes, err := store.RemediationOutcomes(ctx, RemediationOutcomeQuery{
		PolicyID: "python.unused_imports",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("query outcomes: %v", err)
	}

	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %#v", outcomes)
	}

	if outcomes[0].SourceTraceID != "" || outcomes[0].FollowupTraceID != "" {
		t.Fatalf("trace IDs = %#v", outcomes[0])
	}
}

func TestStoreIngestsAllSARIFRuns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store := openTestStore(t, ctx)

	inlineErr16 := NewTraceIngester(
		store,
	).IngestSARIF(ctx, "multi.sarif", multiRunSARIFPayload())
	if inlineErr16 != nil {
		t.Fatalf("ingest SARIF: %v", inlineErr16)
	}

	first, err := store.SARIFResults(
		ctx,
		SARIFResultQuery{PolicyID: "policy.first", Limit: 10},
	)
	if err != nil {
		t.Fatalf("query first SARIF results: %v", err)
	}

	second, err := store.SARIFResults(
		ctx,
		SARIFResultQuery{PolicyID: "policy.second", Limit: 10},
	)
	if err != nil {
		t.Fatalf("query second SARIF results: %v", err)
	}

	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("first = %#v; second = %#v", first, second)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	if stats.SARIFRuns != 2 || stats.SARIFResults != 2 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestASTIndexerStoresCodeChunksAsSearchableEmbeddingCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "app.go")
	writeFile(t, sourcePath, []byte(`package pkg

func BuildMessage(name string) string {
	return "hello " + name
}

type Worker struct{}

func (worker Worker) Run() string {
	return BuildMessage("agent")
}
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	assertIndexedSummary(t, summary)

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/app.go",
		SymbolKind: "function",
		SymbolName: "BuildMessage",
	})
	if err != nil {
		t.Fatalf("query code chunks: %v", err)
	}

	assertBuildMessageChunk(t, chunks)

	candidates, err := store.EmbeddingCandidates(ctx, EmbeddingCandidateQuery{
		RecordKind: codeChunkRecordKind,
		Path:       "pkg/app.go",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("embedding candidates: %v", err)
	}

	if len(candidates) < 3 {
		t.Fatalf("candidates = %#v", candidates)
	}

	if candidates[0].Metadata["record_kind"] != codeChunkRecordKind {
		t.Fatalf("candidate = %#v", candidates[0])
	}

	searchResults, err := store.Search(ctx, SearchQuery{Text: "BuildMessage", Limit: 5})
	if err != nil {
		t.Fatalf("search code chunks: %v", err)
	}

	if len(searchResults) == 0 || searchResults[0].Kind != codeChunkRecordKind {
		t.Fatalf("search results = %#v", searchResults)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertMinimumStats(t, stats, Stats{
		Files:      1,
		CodeChunks: 3,
		FtsRows:    3,
	})
}

func TestASTIndexerStoresParentEdgesAndContext(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(`import os

def helper():
    return "ok"

def a():
    return "a"

def load_a_config():
    return "config"

class Worker:
    def run(self):
        return helper()
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	assertIndexedSummary(t, summary)

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/worker.py",
		SymbolPath: "Worker.run",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query run chunk: %v", err)
	}

	assertWorkerRunChunk(t, chunks)

	context, err := store.CodeContext(ctx, CodeContextQuery{
		Path:       "pkg/worker.py",
		SymbolPath: "Worker.run",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("code context: %v", err)
	}

	assertWorkerRunContext(t, context)
	assertWorkerLineAndConfigContext(t, ctx, store)
	assertWorkerImportEdge(t, ctx, store)

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}

	assertMinimumStats(t, stats, Stats{CodeEdges: 1})
}

func assertWorkerLineAndConfigContext(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	lineContext, err := store.CodeContext(ctx, CodeContextQuery{
		Path:  "pkg/worker.py",
		Line:  14,
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("line code context: %v", err)
	}

	if lineContext.Chunk.SymbolPath != "Worker.run" {
		t.Fatalf("line context = %#v", lineContext.Chunk)
	}

	configContext, err := store.CodeContext(ctx, CodeContextQuery{
		Path:       "pkg/worker.py",
		SymbolPath: "load_a_config",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("config code context: %v", err)
	}

	assertNoSubstringReferenceEdge(t, configContext)
}

func assertWorkerImportEdge(t *testing.T, ctx context.Context, store *Store) {
	t.Helper()

	edges, err := store.CodeEdges(ctx, CodeEdgeQuery{
		Path:       "pkg/worker.py",
		Kind:       "imports",
		TargetName: "os",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query import edges: %v", err)
	}

	if len(edges) != 1 {
		t.Fatalf("import edges = %#v", edges)
	}
}

func assertIndexedSummary(t *testing.T, summary CodeIndexSummary) {
	t.Helper()

	if summary.FilesIndexed != 1 || summary.ChunksIndexed < 3 {
		t.Fatalf("summary = %#v", summary)
	}
}

func assertBuildMessageChunk(t *testing.T, chunks []CodeChunk) {
	t.Helper()

	if len(chunks) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}

	if chunks[0].Language != "go" || chunks[0].SearchText == "" ||
		!strings.Contains(chunks[0].RawText, "BuildMessage") {
		t.Fatalf("chunk = %#v", chunks[0])
	}
}

func assertWorkerRunChunk(t *testing.T, chunks []CodeChunk) {
	t.Helper()

	if len(chunks) != 1 || chunks[0].ParentSymbolPath != "Worker" ||
		chunks[0].ParentChunkID == "" {
		t.Fatalf("run chunks = %#v", chunks)
	}
}

func assertWorkerRunContext(t *testing.T, context CodeContext) {
	t.Helper()

	if context.Parent == nil || context.Parent.SymbolPath != "Worker" {
		t.Fatalf("context parent = %#v", context.Parent)
	}

	if len(context.OutgoingEdges) == 0 {
		t.Fatalf("context outgoing edges = %#v", context.OutgoingEdges)
	}

	if !codeEdgesContainTarget(context.OutgoingEdges, "helper") {
		t.Fatalf(
			"context outgoing edges missing helper reference: %#v",
			context.OutgoingEdges,
		)
	}
}

func assertNoSubstringReferenceEdge(t *testing.T, context CodeContext) {
	t.Helper()

	if codeEdgesContainTarget(context.OutgoingEdges, "a") {
		t.Fatalf(
			"substring reference edge should not be emitted: %#v",
			context.OutgoingEdges,
		)
	}
}

func codeEdgesContainTarget(edges []CodeEdge, target string) bool {
	for _, edge := range edges {
		if edge.TargetName == target {
			return true
		}
	}

	return false
}

func TestStoreQueriesMigratedCodeChunksWithNullParentSymbolPath(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	path := filepath.Join(t.TempDir(), "code-intel.db")
	store := openTestStoreAt(t, ctx, path)
	rawDatabase := openRawSQLite(t, path)

	_, inlineErrC := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES ('pkg/app.py', 'python', 'hash-file', 10, 1, '2026-01-01T00:00:00Z')`,
	)
	if inlineErrC != nil {
		t.Fatalf("insert old code file: %v", inlineErrC)
	}

	_, inlineErrD := rawDatabase.ExecContext(
		ctx,
		`INSERT INTO code_chunks(
			chunk_id, path, language, node_kind, symbol_kind, symbol_name,
			symbol_path, parent_symbol_path, parent_chunk_id, start_byte,
			end_byte, start_line, end_line, content_hash, search_text, raw_text
		) VALUES (
			'chunk-old', 'pkg/app.py', 'python', 'module', 'module', 'app',
			'app', NULL, '', 0, 10, 1, 1, 'hash-chunk', 'app', 'value = 1'
		)`,
	)
	if inlineErrD != nil {
		t.Fatalf("insert old code chunk: %v", inlineErrD)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{Path: "pkg/app.py"})
	if err != nil {
		t.Fatalf("query old code chunks: %v", err)
	}

	if len(chunks) != 1 || chunks[0].ParentSymbolPath != "" {
		t.Fatalf("code chunks = %#v", chunks)
	}

	context, err := store.CodeContext(ctx, CodeContextQuery{ChunkID: "chunk-old"})
	if err != nil {
		t.Fatalf("query old code context: %v", err)
	}

	if context.Chunk.ParentSymbolPath != "" {
		t.Fatalf("code context = %#v", context)
	}
}

func TestSARIFIngestLinksASTBackedResultsToCodeChunks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "pkg", "worker.py"), []byte(`def helper():
    return "ok"
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, inlineErrE := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if inlineErrE != nil {
		t.Fatalf("index code: %v", inlineErrE)
	}

	err := store.IngestSARIFRun(ctx, SARIFRun{
		ID:       "sarif-run-1",
		ToolName: "coding-ethos",
		Results: []SARIFResultReference{{
			ID:            "sarif-result-1",
			RuleID:        "filesystem.line_limits",
			Message:       "Large source files must not keep growing.",
			PolicyID:      "filesystem.line_limits",
			SkillID:       "agent-operating-discipline",
			Path:          "pkg/worker.py",
			ASTLanguage:   "python",
			ASTNodeKind:   "function_definition",
			ASTSymbolKind: "function",
			ASTSymbolName: "helper",
			ASTSymbolPath: "helper",
			SearchText:    "helper line limit",
		}},
	})
	if err != nil {
		t.Fatalf("ingest SARIF run: %v", err)
	}

	results, err := store.SARIFResults(ctx, SARIFResultQuery{RunID: "sarif-run-1"})
	if err != nil {
		t.Fatalf("SARIF results: %v", err)
	}

	if len(results) != 1 || results[0].LinkedChunkID == "" {
		t.Fatalf("SARIF results = %#v", results)
	}

	context, err := store.CodeContext(
		ctx,
		CodeContextQuery{ChunkID: results[0].LinkedChunkID},
	)
	if err != nil {
		t.Fatalf("code context: %v", err)
	}

	if len(context.FindingLinks) != 1 ||
		context.FindingLinks[0].FindingID != "sarif-result-1" {
		t.Fatalf("finding links = %#v", context.FindingLinks)
	}
}

func TestSQLiteVectorIndexSearchesEmbeddings(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	index, err := NewSQLiteVectorIndex(ctx, filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatalf("open vector index: %v", err)
	}

	t.Cleanup(func() {
		closeErr := index.Close()
		if closeErr != nil {
			t.Fatalf("close vector index: %v", closeErr)
		}
	})

	seedSQLiteVectorIndex(t, ctx, index)
	assertSQLiteVectorSearch(t, ctx, index)
	assertSQLiteVectorMutation(t, ctx, index)
	assertSQLiteVectorValidation(t, ctx, index)
}

func seedSQLiteVectorIndex(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	for _, record := range sqliteVectorSeedRecords() {
		err := index.UpsertEmbedding(ctx, record)
		if err != nil {
			t.Fatalf("upsert vector %q: %v", record.ID, err)
		}
	}
}

func sqliteVectorSeedRecords() []evidence.VectorRecord {
	return []evidence.VectorRecord{
		{
			ID:         "near",
			Collection: "remediations",
			ModelID:    "test-model",
			Vector:     []float32{1, 0, 0},
			Dimension:  3,
			Metadata:   map[string]string{"policy_id": "python.unused_imports"},
		},
		{
			ID:         "far",
			Collection: "remediations",
			ModelID:    "test-model",
			Vector:     []float32{0, 1, 0},
			Dimension:  3,
			Metadata:   map[string]string{"policy_id": "python.unused_imports"},
		},
	}
}

func assertSQLiteVectorSearch(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	matches, err := index.Search(ctx, evidence.VectorQuery{
		Collection: "remediations",
		ModelID:    "test-model",
		Vector:     []float32{1, 0, 0},
		Filters:    map[string]string{"policy_id": "python.unused_imports"},
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("search vectors: %v", err)
	}

	if len(matches) != 1 || matches[0].ID != "near" {
		t.Fatalf("matches = %#v", matches)
	}

	assertSQLiteVectorRows(t, ctx, index, 2, "vector stats")
}

func assertSQLiteVectorMutation(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	err := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         "near",
		Collection: "remediations",
		ModelID:    "test-model",
		Vector:     []float32{0, 0, 1, 0},
		Dimension:  4,
		Metadata:   map[string]string{"policy_id": "python.unused_imports"},
	})
	if err != nil {
		t.Fatalf("replace vector with new dimension: %v", err)
	}

	err = index.DeleteEmbedding(ctx, "far", "test-model")
	if err != nil {
		t.Fatalf("delete vector: %v", err)
	}

	err = index.DeleteEmbedding(ctx, "missing", "test-model")
	if err != nil {
		t.Fatalf("delete missing vector: %v", err)
	}

	assertSQLiteVectorRows(t, ctx, index, 1, "vector stats after delete")

	err = index.Rebuild(ctx, "remediations")
	if err != nil {
		t.Fatalf("rebuild vectors: %v", err)
	}

	assertSQLiteVectorRows(t, ctx, index, 0, "vector stats after rebuild")
}

func assertSQLiteVectorRows(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
	expected int,
	label string,
) {
	t.Helper()

	stats, err := index.Stats(ctx)
	if err != nil {
		t.Fatalf("%s: %v", label, err)
	}

	if stats.Backend != vectorBackendName || stats.Rows != expected {
		t.Fatalf("%s = %#v", label, stats)
	}
}

func assertSQLiteVectorValidation(
	t *testing.T,
	ctx context.Context,
	index *SQLiteVectorIndex,
) {
	t.Helper()

	_, err := NewSQLiteVectorIndex(ctx, "")
	if err == nil {
		t.Fatal("NewSQLiteVectorIndex(empty) returned nil error")
	}

	err = index.UpsertEmbedding(ctx, evidence.VectorRecord{})
	if err == nil {
		t.Fatal("UpsertEmbedding(empty) returned nil error")
	}

	_, err = index.Search(ctx, evidence.VectorQuery{})
	if err == nil {
		t.Fatal("Search(empty) returned nil error")
	}
}

func TestHybridSearchCombinesFTSVectorAndOutcomeBoost(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)

	index, err := NewSQLiteVectorIndex(ctx, filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatalf("open vector index: %v", err)
	}

	t.Cleanup(func() {
		closeErr := index.Close()
		if closeErr != nil {
			t.Fatalf("close vector index: %v", closeErr)
		}
	})

	inlineErr22 := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-1",
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
		SearchText:    "Remove unused import and rerun ruff.",
	})
	if inlineErr22 != nil {
		t.Fatalf("record outcome: %v", inlineErr22)
	}

	inlineErr23 := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         "rem-1",
		Collection: "remediations",
		ModelID:    "test-model",
		Vector:     []float32{1, 0, 0},
		Dimension:  3,
		Metadata: map[string]string{
			"record_kind": "remediation_outcome",
			"record_id":   "rem-1",
			"policy_id":   "python.unused_imports",
			"skill_id":    "lint-remediation",
			"path":        "pkg/app.py",
			"outcome":     "fixed",
		},
	})
	if inlineErr23 != nil {
		t.Fatalf("upsert vector: %v", inlineErr23)
	}

	results, err := store.HybridSearch(ctx, index, HybridSearchQuery{
		Text:       "unused",
		Collection: "remediations",
		ModelID:    "test-model",
		PolicyID:   "python.unused_imports",
		Vector:     []float32{1, 0, 0},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("hybrid results = %#v", results)
	}

	if results[0].Source != "fts+vector" || results[0].Outcome != "fixed" ||
		results[0].Score <= 2 {
		t.Fatalf("hybrid result = %#v", results[0])
	}
}

func TestHybridSearchReturnsVectorBackedCodeChunks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	sourcePath := filepath.Join(root, "pkg", "worker.py")
	writeFile(t, sourcePath, []byte(`class Worker:
    def run(self):
        return "ok"
`))

	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	_, inlineErrF := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if inlineErrF != nil {
		t.Fatalf("index code: %v", inlineErrF)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/worker.py",
		SymbolName: "Worker",
		Limit:      1,
	})
	if err != nil {
		t.Fatalf("query chunk: %v", err)
	}

	if len(chunks) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}

	index, err := NewSQLiteVectorIndex(ctx, filepath.Join(t.TempDir(), "vectors.db"))
	if err != nil {
		t.Fatalf("open vector index: %v", err)
	}

	t.Cleanup(func() {
		closeErr := index.Close()
		if closeErr != nil {
			t.Fatalf("close vector index: %v", closeErr)
		}
	})

	inlineErr24 := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         chunks[0].ID,
		Collection: "code_chunks",
		ModelID:    "test-model",
		Vector:     []float32{0, 1, 0},
		Dimension:  3,
		Metadata: map[string]string{
			"record_kind": codeChunkRecordKind,
			"record_id":   chunks[0].ID,
			"path":        chunks[0].Path,
			"message":     chunks[0].SymbolPath,
		},
	})
	if inlineErr24 != nil {
		t.Fatalf("upsert vector: %v", inlineErr24)
	}

	results, err := store.HybridSearch(ctx, index, HybridSearchQuery{
		Text:       "Worker",
		Collection: "code_chunks",
		ModelID:    "test-model",
		Path:       "pkg/worker.py",
		Vector:     []float32{0, 1, 0},
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("hybrid search: %v", err)
	}

	if len(results) == 0 || results[0].Kind != codeChunkRecordKind ||
		results[0].Source != "fts+vector" {
		t.Fatalf("hybrid results = %#v", results)
	}
}

func TestASTIndexerSupportsShellAndYAMLChunks(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "scripts", "check.sh"), []byte(`#!/usr/bin/env bash

run_check() {
  echo ok
}
`))
	writeFile(t, filepath.Join(root, "config.yml"), []byte(`linters:
  ruff: true
`))
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	summary, err := NewASTIndexer(
		store,
	).IndexPaths(ctx, root, []string{"scripts", "config.yml"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}

	if summary.FilesIndexed != 2 || summary.ChunksIndexed < 2 {
		t.Fatalf("summary = %#v", summary)
	}

	shellChunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Language:   "shell",
		SymbolName: "run_check",
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("query shell chunks: %v", err)
	}

	if len(shellChunks) != 1 || shellChunks[0].SymbolKind != "function" {
		t.Fatalf("shell chunks = %#v", shellChunks)
	}

	yamlChunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "config.yml",
		Language:   "yaml",
		SymbolName: "linters",
		Limit:      5,
	})
	if err != nil {
		t.Fatalf("query yaml chunks: %v", err)
	}

	if len(yamlChunks) != 1 || yamlChunks[0].SymbolKind != "config_entry" {
		t.Fatalf("yaml chunks = %#v", yamlChunks)
	}
}

func TestVectorFactoryDefaultPathAndIndexStatus(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	root := t.TempDir()
	assertDefaultVectorPath(t, root)
	assertUnsupportedVectorBackendFails(t, ctx, root)

	index := openTestVectorIndex(t, ctx, root)
	store := openTestStoreAt(
		t,
		ctx,
		filepath.Join(root, ".coding-ethos", "code-intel.db"),
	)

	replaceVectorStatusCodeChunk(t, ctx, store)

	stats, err := index.Stats(ctx)
	if err != nil {
		t.Fatalf("vector stats: %v", err)
	}

	status, err := store.IndexStatus(ctx, stats, EmbeddingRecordQuery{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "test-model",
	})
	if err != nil {
		t.Fatalf("index status: %v", err)
	}

	assertVectorStatusBeforeEmbedding(t, status)

	inlineErr26 := store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "test-model",
		InputKind:  "text",
		RecordKind: codeChunkRecordKind,
		RecordID:   "chunk-1",
		Dimension:  3,
		Path:       "pkg/app.py",
	})
	if inlineErr26 != nil {
		t.Fatalf("upsert embedding record: %v", inlineErr26)
	}

	status, err = store.IndexStatus(ctx, stats, EmbeddingRecordQuery{
		Backend:    vectorBackendName,
		Collection: "code_chunks",
		ModelID:    "test-model",
	})
	if err != nil {
		t.Fatalf("index status after embedding: %v", err)
	}

	assertVectorStatusAfterEmbedding(t, status)
}

func assertDefaultVectorPath(t *testing.T, root string) {
	t.Helper()

	got := DefaultVectorPath(root)

	want := filepath.Join(root, ".coding-ethos", "code-intel-vectors.db")
	if got != want {
		t.Fatalf("DefaultVectorPath() = %q", got)
	}
}

func assertUnsupportedVectorBackendFails(
	t *testing.T,
	ctx context.Context,
	root string,
) {
	t.Helper()

	_, err := NewVectorIndex(
		ctx,
		VectorBackendConfig{Backend: "unknown", URI: filepath.Join(root, "bad.db")},
	)
	if err == nil {
		t.Fatal("unsupported vector backend should fail")
	}
}

func openTestVectorIndex(
	t *testing.T,
	ctx context.Context,
	root string,
) evidence.VectorIndex {
	t.Helper()

	index, err := NewVectorIndex(
		ctx,
		VectorBackendConfig{URI: filepath.Join(root, "vectors.db")},
	)
	if err != nil {
		t.Fatalf("new vector index: %v", err)
	}

	t.Cleanup(func() {
		if closer, ok := index.(interface{ Close() error }); ok {
			closeErr := closer.Close()
			if closeErr != nil {
				t.Fatalf("close vector index: %v", closeErr)
			}
		}
	})

	return index
}

func replaceVectorStatusCodeChunk(
	t *testing.T,
	ctx context.Context,
	store *Store,
) {
	t.Helper()

	err := store.ReplaceCodeFileChunks(ctx, CodeFile{
		Path:        "pkg/app.py",
		Language:    "python",
		ContentHash: "hash-file",
		SizeBytes:   20,
		LineCount:   3,
	}, []CodeChunk{{
		ID:          "chunk-1",
		Path:        "pkg/app.py",
		Language:    "python",
		NodeKind:    "function_definition",
		SymbolKind:  "function",
		SymbolName:  "run",
		SymbolPath:  "run",
		ContentHash: "hash-chunk",
		SearchText:  "run function",
		RawText:     "def run(): pass",
		StartLine:   1,
		EndLine:     1,
	}})
	if err != nil {
		t.Fatalf("replace chunks: %v", err)
	}
}

func assertVectorStatusBeforeEmbedding(t *testing.T, status IndexStatus) {
	t.Helper()

	if status.ReadyRecords == 0 || status.MissingVectors == 0 || status.Fresh {
		t.Fatalf("status before embedding = %#v", status)
	}
}

func assertVectorStatusAfterEmbedding(t *testing.T, status IndexStatus) {
	t.Helper()

	if status.EmbeddingRecords != 1 || status.MissingVectors != 0 || !status.Fresh {
		t.Fatalf("status after embedding = %#v", status)
	}
}

func assertStats(t *testing.T, got, want Stats) {
	t.Helper()

	for _, check := range expectedStatsFields(got, want) {
		if check.want == 0 {
			continue
		}

		if check.got != check.want {
			t.Fatalf("stats = %#v, want %s=%d", got, check.name, check.want)
		}
	}
}

type statFieldCheck struct {
	name string
	got  int
	want int
}

func expectedStatsFields(got, want Stats) []statFieldCheck {
	return []statFieldCheck{
		{"traces", got.Traces, want.Traces},
		{"hook_events", got.HookEvents, want.HookEvents},
		{"hook_decisions", got.HookDecisions, want.HookDecisions},
		{"hook_targets", got.HookTargets, want.HookTargets},
		{"findings", got.Findings, want.Findings},
		{"files", got.Files, want.Files},
		{"code_chunks", got.CodeChunks, want.CodeChunks},
		{"code_edges", got.CodeEdges, want.CodeEdges},
		{"remediations", got.Remediations, want.Remediations},
		{"remediation_events", got.RemediationEvents, want.RemediationEvents},
		{"sarif_runs", got.SARIFRuns, want.SARIFRuns},
		{"sarif_results", got.SARIFResults, want.SARIFResults},
		{"remediation_outcomes", got.RemediationOutcomes, want.RemediationOutcomes},
		{"embedding_records", got.EmbeddingRecords, want.EmbeddingRecords},
		{"fts_rows", got.FtsRows, want.FtsRows},
	}
}

func assertMinimumStats(t *testing.T, got, want Stats) {
	t.Helper()

	if got.Files < want.Files ||
		got.CodeChunks < want.CodeChunks ||
		got.CodeEdges < want.CodeEdges ||
		got.FtsRows < want.FtsRows {
		t.Fatalf("stats = %#v, want minimum %#v", got, want)
	}
}

func containsJoined(items []string, item string) bool {
	return strings.Contains(strings.Join(items, ","), item)
}

func stringAnySlicesEqual(got, want []any) bool {
	if len(got) != len(want) {
		return false
	}

	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}

func openTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()

	return openTestStoreAt(t, ctx, filepath.Join(t.TempDir(), "code-intel.db"))
}

func openTestStoreAt(t *testing.T, ctx context.Context, path string) *Store {
	t.Helper()

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	t.Cleanup(func() {
		err := store.Close()
		if err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	return store
}

func openRawSQLite(t *testing.T, path string) *sql.DB {
	t.Helper()

	database, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open raw sqlite: %v", err)
	}

	t.Cleanup(func() {
		closeErr := database.Close()
		if closeErr != nil {
			t.Fatalf("close raw sqlite: %v", closeErr)
		}
	})

	return database
}

func lintTracePayload(t *testing.T, traceID, recordedAt string) []byte {
	t.Helper()

	diagnostic := diagnostics.Diagnostic{
		Tool:     "ruff",
		Code:     "F401",
		File:     "pkg/app.py",
		Line:     4,
		Severity: "error",
		PolicyID: "python.unused_imports",
		SkillID:  "lint-remediation",
		Message:  "unused import",
		Advice:   "Remove unused imports.",
	}
	findings := evidence.FromDiagnostics([]diagnostics.Diagnostic{diagnostic})
	remediations := agentmsg.FromDiagnostics([]diagnostics.Diagnostic{diagnostic})
	record := lint.TraceRecord{
		SchemaVersion:      evidence.SchemaVersion,
		TraceID:            traceID,
		RecordedAtUTC:      recordedAt,
		RepoRoot:           "/repo",
		Result:             lint.Result{Scope: "tool:ruff", Status: "blocked"},
		Findings:           findings,
		AgentRemediation:   remediations,
		RemediationSummary: agentmsg.Summarize(remediations),
		RemediationEvents: evidence.RemediationEvents(
			remediations,
			findings,
			traceID,
			"suggested",
		),
	}

	return mustJSON(t, record)
}

func sarifPayload(t *testing.T) []byte {
	t.Helper()

	output, err := hookoutput.FormatLintResultSARIF(lint.Result{
		Scope:  "tool:ruff",
		Status: "blocked",
		Diagnostics: []diagnostics.Diagnostic{{
			Tool:     "ruff",
			Code:     "F401",
			File:     "pkg/app.py",
			Line:     4,
			Severity: "error",
			PolicyID: "python.unused_imports",
			SkillID:  "lint-remediation",
			Message:  "unused import",
			Advice:   "Remove unused imports.",
			Metadata: map[string]any{
				"implementation": "cel",
				"when":           "finding.policy_id == 'python.unused_imports'",
				"policy_source":  "coding_ethos.yml",
				"source_tool":    "ruff",
			},
		}},
	})
	if err != nil {
		t.Fatalf("format SARIF: %v", err)
	}

	return []byte(output)
}

func hookTracePayload(t *testing.T) []byte {
	t.Helper()

	return hookTracePayloadWithIDs(
		t,
		"hook-trace-a",
		"deny-hook-a",
		"2026-01-01T00:02:00Z",
	)
}

func hookTracePayloadWithIDs(
	t *testing.T,
	traceID string,
	trackingID string,
	recordedAtUTC string,
) []byte {
	t.Helper()

	finding := evidence.FromDiagnostic(diagnostics.Diagnostic{
		Tool:     "hook",
		File:     "pkg/app.py",
		PolicyID: "shell.github_admin",
		SkillID:  "safe-git-workflow",
		Message:  "admin bypass",
	})
	remediation := agentmsg.Remediation{
		ID:       "rem-hook",
		PolicyID: "shell.github_admin",
		SkillID:  "safe-git-workflow",
		Message:  "Use the normal review path.",
	}
	event := evidence.RemediationEventFromRemediation(
		remediation,
		finding.ID,
		traceID,
		"suggested",
	)

	return mustJSON(t, map[string]any{
		"schema_version":  evidence.SchemaVersion,
		"trace_id":        traceID,
		"tracking_id":     trackingID,
		"recorded_at_utc": recordedAtUTC,
		"provider":        "codex",
		"event":           "PreToolUse",
		"tool":            "Bash",
		"cwd":             "/repo",
		"command": map[string]any{
			"sha256":       strings.Repeat("a", 64),
			"shape_sha256": strings.Repeat("b", 64),
			"preview":      "git status --short",
		},
		"files":             []string{"pkg/app.py"},
		"operation_kind":    "git_status",
		"target_kind":       "source_file",
		"risk_category":     "bypass",
		"target_set_sha256": strings.Repeat("c", 64),
		"runtime_ms":        12,
		"status":            "blocked",
		"decisions": []map[string]any{
			{
				"policy_id":       "git.wrapper_required",
				"decision":        "block",
				"severity":        "block",
				"skill_id":        "safe-git-workflow",
				"implementation":  "cel",
				"message_hash":    strings.Repeat("d", 64),
				"suggestion_hash": strings.Repeat("e", 64),
			},
		},
		"findings":           []evidence.Finding{finding},
		"agent_remediation":  []agentmsg.Remediation{remediation},
		"remediation_events": []evidence.RemediationEvent{event},
		"output_shape": map[string]any{
			"blocked":           true,
			"has_updated_input": true,
		},
	})
}

func multiRunSARIFPayload() []byte {
	return []byte(`{
		"version":"2.1.0",
		"runs":[
			{
				"tool":{"driver":{"name":"first-tool","rules":[
					{"id":"R1","properties":{"policy_id":"policy.first","skill_id":"skill-a"}}
				]}},
				"results":[
					{
						"ruleId":"R1",
						"level":"error",
						"message":{"text":"first result"},
							"locations":[{
								"physicalLocation":{
									"artifactLocation":{"uri":"pkg/first.py"},
									"region":{"startLine":2}
								}
							}]
					}
				]
			},
			{
				"tool":{"driver":{"name":"second-tool","rules":[
					{"id":"R2","properties":{"policy_id":"policy.second","skill_id":"skill-b"}}
				]}},
				"results":[
					{
						"ruleId":"R2",
						"level":"warning",
						"message":{"text":"second result"},
							"locations":[{
								"physicalLocation":{
									"artifactLocation":{"uri":"pkg/second.py"},
									"region":{"startLine":4}
								}
							}]
					}
				]
			}
		]
	}`)
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()

	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}

	return payload
}

func writeFile(t *testing.T, path string, payload []byte) {
	t.Helper()

	err := os.MkdirAll(filepath.Dir(path), 0o700)
	if err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	err = os.WriteFile(path, payload, 0o600)
	if err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
