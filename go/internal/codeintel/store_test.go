// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/diagnostics"
	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
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
