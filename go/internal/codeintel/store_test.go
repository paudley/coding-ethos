// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
	"blackcat.ca/coding-ethos/go/internal/hookoutput"
	"blackcat.ca/coding-ethos/go/internal/lint"
)

func TestStoreIngestsLintTracesAndReportsRepeatedFailures(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	first := lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z")
	second := lintTracePayload(t, "trace-b.json", "2026-01-01T00:01:00Z")

	if err := ingester.IngestLintTrace(ctx, first); err != nil {
		t.Fatalf("ingest first trace: %v", err)
	}
	if err := ingester.IngestLintTrace(ctx, second); err != nil {
		t.Fatalf("ingest second trace: %v", err)
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
	if stats.Traces != 2 || stats.Findings != 1 || stats.Remediations != 1 ||
		stats.RemediationEvents != 2 || stats.FtsRows != 4 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStoreSearchesRemediationText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	if err := ingester.IngestLintTrace(ctx, lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z")); err != nil {
		t.Fatalf("ingest trace: %v", err)
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
	writeFile(t, filepath.Join(root, ".coding-ethos", "lint-runs", "trace-a.json"), lintTracePayload(t, "trace-a.json", "2026-01-01T00:00:00Z"))
	writeFile(t, filepath.Join(root, ".coding-ethos", "hook-runs", "run-a", "event.json"), hookTracePayload(t))

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

func TestStoreIngestsSARIFResultsWithCELProvenance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	payload := sarifPayload(t)
	if err := NewTraceIngester(store).IngestSARIF(ctx, "policy.sarif", payload); err != nil {
		t.Fatalf("ingest SARIF: %v", err)
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

func TestStoreRecordsRemediationOutcomesAndEmbeddingMetadata(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	ingester := NewTraceIngester(store)
	if err := ingester.IngestLintTrace(ctx, lintTracePayload(t, "trace-a", "2026-01-01T00:00:00Z")); err != nil {
		t.Fatalf("ingest source trace: %v", err)
	}
	if err := ingester.IngestLintTrace(ctx, lintTracePayload(t, "trace-b", "2026-01-01T00:01:00Z")); err != nil {
		t.Fatalf("ingest follow-up trace: %v", err)
	}
	if err := store.RecordRemediationOutcome(ctx, RemediationOutcome{
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
	}); err != nil {
		t.Fatalf("record outcome: %v", err)
	}
	if err := store.UpsertEmbeddingRecord(ctx, EmbeddingRecord{
		Backend:      "sqlite-vec",
		Collection:   "remediations",
		ModelID:      "voyage-code-3",
		RecordKind:   "remediation_outcome",
		RecordID:     "rem-1",
		Dimension:    1024,
		PolicyID:     "python.unused_imports",
		SkillID:      "lint-remediation",
		Path:         "pkg/app.py",
		BackendRowID: "sqlite-vec-row-1",
	}); err != nil {
		t.Fatalf("record embedding: %v", err)
	}

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
	if len(effectiveness) != 1 || effectiveness[0].Fixed != 1 || effectiveness[0].Total != 1 {
		t.Fatalf("effectiveness = %#v", effectiveness)
	}
	embeddingRecords, err := store.EmbeddingRecords(ctx, EmbeddingRecordQuery{
		Backend: "sqlite-vec",
		ModelID: "voyage-code-3",
	})
	if err != nil {
		t.Fatalf("embedding records: %v", err)
	}
	if len(embeddingRecords) != 1 || embeddingRecords[0].BackendRowID != "sqlite-vec-row-1" {
		t.Fatalf("embedding records = %#v", embeddingRecords)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.RemediationOutcomes != 1 || stats.EmbeddingRecords != 1 || stats.FtsRows != 6 {
		t.Fatalf("stats = %#v", stats)
	}
}

func TestStoreReturnsEmbeddingCandidates(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	if err := NewTraceIngester(store).IngestSARIF(ctx, "policy.sarif", sarifPayload(t)); err != nil {
		t.Fatalf("ingest SARIF: %v", err)
	}
	if err := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-1",
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
	}); err != nil {
		t.Fatalf("record outcome: %v", err)
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
		if candidate.Text == "" || candidate.Metadata["policy_id"] != "python.unused_imports" {
			t.Fatalf("candidate = %#v", candidate)
		}
	}
}

func TestStoreQueriesOutcomeWithoutTraceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store := openTestStore(t, ctx)
	if err := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-no-trace",
		RemediationID: "rem-no-trace",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "attempted",
	}); err != nil {
		t.Fatalf("record outcome: %v", err)
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
	if err := NewTraceIngester(store).IngestSARIF(ctx, "multi.sarif", multiRunSARIFPayload()); err != nil {
		t.Fatalf("ingest SARIF: %v", err)
	}

	first, err := store.SARIFResults(ctx, SARIFResultQuery{PolicyID: "policy.first", Limit: 10})
	if err != nil {
		t.Fatalf("query first SARIF results: %v", err)
	}
	second, err := store.SARIFResults(ctx, SARIFResultQuery{PolicyID: "policy.second", Limit: 10})
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
	store := openTestStoreAt(t, ctx, filepath.Join(root, ".coding-ethos", "code-intel.db"))

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"})
	if err != nil {
		t.Fatalf("index code: %v", err)
	}
	if summary.FilesIndexed != 1 || summary.ChunksIndexed < 3 {
		t.Fatalf("summary = %#v", summary)
	}

	chunks, err := store.CodeChunks(ctx, CodeChunkQuery{
		Path:       "pkg/app.go",
		SymbolKind: "function",
		SymbolName: "BuildMessage",
	})
	if err != nil {
		t.Fatalf("query code chunks: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("chunks = %#v", chunks)
	}
	if chunks[0].Language != "go" || chunks[0].SearchText == "" ||
		!strings.Contains(chunks[0].RawText, "BuildMessage") {
		t.Fatalf("chunk = %#v", chunks[0])
	}

	candidates, err := store.EmbeddingCandidates(ctx, EmbeddingCandidateQuery{
		RecordKind: "code_chunk",
		Path:       "pkg/app.go",
		Limit:      10,
	})
	if err != nil {
		t.Fatalf("embedding candidates: %v", err)
	}
	if len(candidates) < 3 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Metadata["record_kind"] != "code_chunk" {
		t.Fatalf("candidate = %#v", candidates[0])
	}
	searchResults, err := store.Search(ctx, SearchQuery{Text: "BuildMessage", Limit: 5})
	if err != nil {
		t.Fatalf("search code chunks: %v", err)
	}
	if len(searchResults) == 0 || searchResults[0].Kind != "code_chunk" {
		t.Fatalf("search results = %#v", searchResults)
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Files != 1 || stats.CodeChunks < 3 || stats.FtsRows < 3 {
		t.Fatalf("stats = %#v", stats)
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
		if err := index.Close(); err != nil {
			t.Fatalf("close vector index: %v", err)
		}
	})

	for _, record := range []evidence.VectorRecord{
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
	} {
		if err := index.UpsertEmbedding(ctx, record); err != nil {
			t.Fatalf("upsert vector %q: %v", record.ID, err)
		}
	}

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
	stats, err := index.Stats(ctx)
	if err != nil {
		t.Fatalf("vector stats: %v", err)
	}
	if stats.Backend != "sqlite-vec" || stats.Rows != 2 {
		t.Fatalf("stats = %#v", stats)
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
		if err := index.Close(); err != nil {
			t.Fatalf("close vector index: %v", err)
		}
	})
	if err := store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "rem-1",
		RemediationID: "rem-1",
		FindingID:     "finding-1",
		PolicyID:      "python.unused_imports",
		SkillID:       "lint-remediation",
		Path:          "pkg/app.py",
		Outcome:       "fixed",
		SearchText:    "Remove unused import and rerun ruff.",
	}); err != nil {
		t.Fatalf("record outcome: %v", err)
	}
	if err := index.UpsertEmbedding(ctx, evidence.VectorRecord{
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
	}); err != nil {
		t.Fatalf("upsert vector: %v", err)
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
	if results[0].Source != "fts+vector" || results[0].Outcome != "fixed" || results[0].Score <= 2 {
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
	store := openTestStoreAt(t, ctx, filepath.Join(root, ".coding-ethos", "code-intel.db"))
	if _, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"pkg"}); err != nil {
		t.Fatalf("index code: %v", err)
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
		if err := index.Close(); err != nil {
			t.Fatalf("close vector index: %v", err)
		}
	})
	if err := index.UpsertEmbedding(ctx, evidence.VectorRecord{
		ID:         chunks[0].ID,
		Collection: "code_chunks",
		ModelID:    "test-model",
		Vector:     []float32{0, 1, 0},
		Dimension:  3,
		Metadata: map[string]string{
			"record_kind": "code_chunk",
			"record_id":   chunks[0].ID,
			"path":        chunks[0].Path,
			"message":     chunks[0].SymbolPath,
		},
	}); err != nil {
		t.Fatalf("upsert vector: %v", err)
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
	if len(results) == 0 || results[0].Kind != "code_chunk" || results[0].Source != "fts+vector" {
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
	store := openTestStoreAt(t, ctx, filepath.Join(root, ".coding-ethos", "code-intel.db"))

	summary, err := NewASTIndexer(store).IndexPaths(ctx, root, []string{"scripts", "config.yml"})
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
		if err := store.Close(); err != nil {
			t.Fatalf("close store: %v", err)
		}
	})

	return store
}

func lintTracePayload(t *testing.T, traceID string, recordedAt string) []byte {
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
		RemediationEvents:  evidence.RemediationEvents(remediations, findings, traceID, "suggested"),
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
	event := evidence.RemediationEventFromRemediation(remediation, finding.ID, "hook-trace-a", "suggested")

	return mustJSON(t, map[string]any{
		"schema_version":     evidence.SchemaVersion,
		"trace_id":           "hook-trace-a",
		"recorded_at_utc":    "2026-01-01T00:02:00Z",
		"provider":           "codex",
		"event":              "PreToolUse",
		"tool":               "Bash",
		"cwd":                "/repo",
		"status":             "blocked",
		"findings":           []evidence.Finding{finding},
		"agent_remediation":  []agentmsg.Remediation{remediation},
		"remediation_events": []evidence.RemediationEvent{event},
		"output_shape":       map[string]any{"blocked": true},
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
						"locations":[{"physicalLocation":{"artifactLocation":{"uri":"pkg/first.py"},"region":{"startLine":2}}}]
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
						"locations":[{"physicalLocation":{"artifactLocation":{"uri":"pkg/second.py"},"region":{"startLine":4}}}]
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

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}
