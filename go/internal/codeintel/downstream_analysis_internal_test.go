// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func TestAnalyzeDownstreamSummarizesFrictionAndLogs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	dbPath := DefaultDBPath(root)

	store, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}

	err = seedDownstreamAnalysisStore(ctx, store)
	if err != nil {
		t.Fatalf("seed store: %v", err)
	}

	err = store.Close()
	if err != nil {
		t.Fatalf("close writable store: %v", err)
	}

	writeDownstreamHookLog(
		t,
		root,
		"20260524T000000Z-1-2",
		"error: open proxy output ledger: configure code intelligence store: database is locked (5) (SQLITE_BUSY)\n"+
			"warning: startup repo map query failed: stale code context for src/app.py\n"+
			"error: coding-ethos-sandbox: exec sandboxed command: no such file or directory\n",
	)

	readOnlyStore, err := OpenReadOnly(ctx, dbPath)
	if err != nil {
		t.Fatalf("open read-only store: %v", err)
	}
	defer readOnlyStore.Close()

	analysis, err := AnalyzeDownstream(ctx, root, readOnlyStore, 5)
	if err != nil {
		t.Fatalf("analyze downstream: %v", err)
	}

	if !analysis.SQLiteStrategy.StoreAvailable ||
		!analysis.SQLiteStrategy.ReadOnlyAnalysis ||
		!analysis.SQLiteStrategy.SingleConnectionPool {
		t.Fatalf("SQLite strategy missing read-only posture: %#v", analysis.SQLiteStrategy)
	}

	if analysis.LogSignals.SQLiteBusyCount != 1 ||
		analysis.LogSignals.StaleRepoMapCount != 1 ||
		analysis.LogSignals.SandboxMissingCount != 1 {
		t.Fatalf("log signals = %#v", analysis.LogSignals)
	}

	assertDownstreamPolicyBlocker(t, analysis, "filesystem.line_limits")
	assertDownstreamFindingHotspot(t, analysis, "src/app.py", "python.optional_returns")
	assertDownstreamFilePressure(t, analysis, "src/big.py")
	assertDownstreamToolchainFailure(t, analysis, "runtime.sandbox_denial")
}

func TestAnalyzeDownstreamWithoutStoreStillSummarizesLogs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeDownstreamHookLog(
		t,
		root,
		"20260524T000001Z-1-2",
		"Protected branch writes are forbidden.\n"+
			"Inline command environment variables are forbidden.\n",
	)

	analysis, err := AnalyzeDownstream(context.Background(), root, nil, 5)
	if err != nil {
		t.Fatalf("analyze downstream without store: %v", err)
	}

	if analysis.SQLiteStrategy.StoreAvailable {
		t.Fatalf("store should be unavailable: %#v", analysis.SQLiteStrategy)
	}

	if analysis.LogSignals.ProtectedBranchCount != 1 ||
		analysis.LogSignals.InlineEnvCount != 1 {
		t.Fatalf("log signals = %#v", analysis.LogSignals)
	}
}

func seedDownstreamAnalysisStore(ctx context.Context, store *Store) error {
	err := store.IngestTrace(ctx, Trace{
		ID:            "trace-1",
		Kind:          "hook",
		RecordedAtUTC: "2026-05-24T00:00:00Z",
		Tool:          "Bash",
		Status:        "blocked",
		Raw:           []byte(`{"trace_id":"trace-1"}`),
		HookEvent: &HookEventAnalytics{
			TraceID:       "trace-1",
			Tool:          "Bash",
			Status:        "blocked",
			OperationKind: "lint",
			TargetKind:    "unknown",
			RiskCategory:  "policy_block",
			Blocked:       true,
		},
		HookDecisions: []HookDecisionAnalytics{
			{
				TraceID:         "trace-1",
				PolicyID:        "filesystem.line_limits",
				Decision:        "block",
				Severity:        "block",
				DiagnosticCount: 1,
			},
		},
		Findings: []evidence.Finding{
			{
				ID:       "finding-1",
				Tool:     "optional_returns",
				Code:     "typing",
				PolicyID: "python.optional_returns",
				Message:  "Required values should not be optional",
				Severity: "block",
				SourceSpan: evidence.SourceSpan{
					Path: "src/app.py",
				},
			},
			{
				ID:       "finding-2",
				Tool:     "coding-ethos-sandbox",
				Code:     "SANDBOX_DENIED",
				PolicyID: "runtime.sandbox_denial",
				Message:  "Managed tool sandbox execution was denied",
				Severity: "error",
			},
		},
	})
	if err != nil {
		return err
	}

	_, err = store.database.ExecContext(
		ctx,
		`INSERT INTO code_files(
			path, language, content_hash, size_bytes, line_count, indexed_at_utc
		) VALUES (?, ?, ?, ?, ?, ?)`,
		"src/big.py",
		"python",
		"hash",
		12345,
		900,
		"2026-05-24T00:00:00Z",
	)

	return err
}

func writeDownstreamHookLog(
	t *testing.T,
	root string,
	runID string,
	stderr string,
) {
	t.Helper()

	runDir := filepath.Join(root, downstreamHookRunsSubpath, runID)
	err := os.MkdirAll(runDir, 0o700)
	if err != nil {
		t.Fatalf("create hook run dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(runDir, "stderr.log"), []byte(stderr), 0o600)
	if err != nil {
		t.Fatalf("write stderr log: %v", err)
	}

	err = os.WriteFile(filepath.Join(runDir, "event.json"), []byte("{}"), 0o600)
	if err != nil {
		t.Fatalf("write event log: %v", err)
	}
}

func assertDownstreamPolicyBlocker(
	t *testing.T,
	analysis DownstreamAnalysis,
	policyID string,
) {
	t.Helper()

	for _, blocker := range analysis.PolicyBlockers {
		if blocker.PolicyID == policyID {
			return
		}
	}

	t.Fatalf("missing policy blocker %q: %#v", policyID, analysis.PolicyBlockers)
}

func assertDownstreamFindingHotspot(
	t *testing.T,
	analysis DownstreamAnalysis,
	path string,
	policyID string,
) {
	t.Helper()

	for _, hotspot := range analysis.FindingHotspots {
		if hotspot.Path == path && hotspot.PolicyID == policyID {
			return
		}
	}

	t.Fatalf(
		"missing finding hotspot %q %q: %#v",
		path,
		policyID,
		analysis.FindingHotspots,
	)
}

func assertDownstreamFilePressure(
	t *testing.T,
	analysis DownstreamAnalysis,
	path string,
) {
	t.Helper()

	for _, pressure := range analysis.FilePressure {
		if pressure.Path == path {
			return
		}
	}

	t.Fatalf("missing file pressure %q: %#v", path, analysis.FilePressure)
}

func assertDownstreamToolchainFailure(
	t *testing.T,
	analysis DownstreamAnalysis,
	policyID string,
) {
	t.Helper()

	for _, failure := range analysis.ToolchainFailures {
		if failure.PolicyID == policyID {
			return
		}
	}

	t.Fatalf(
		"missing toolchain failure %q: %#v",
		policyID,
		analysis.ToolchainFailures,
	)
}
