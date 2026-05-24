// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
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
	writeDownstreamLintRun(
		t,
		root,
		"20260524T000000Z-tool_pyupgrade.json",
		`{"tool":"pyupgrade","sandbox":{"reason":"sandbox backend unavailable"}}`,
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
		analysis.LogSignals.SandboxMissingCount != 1 ||
		analysis.LogSignals.LintRunCount != 1 ||
		analysis.LogSignals.ToolchainFailureCount == 0 {
		t.Fatalf("log signals = %#v", analysis.LogSignals)
	}

	assertDownstreamPolicyBlocker(t, analysis, "filesystem.line_limits")
	assertDownstreamAffectedCommand(t, analysis, "filesystem.line_limits", "Bash")
	assertDownstreamRemediationLoop(t, analysis, "filesystem.line_limits")
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
	writeDownstreamHookLog(
		t,
		root,
		"20260524T000002Z-1-2",
		strings.Repeat("large unbounded log line ", 6000),
	)

	analysis, err := AnalyzeDownstream(context.Background(), root, nil, 5)
	if err != nil {
		t.Fatalf("analyze downstream without store: %v", err)
	}

	if analysis.SQLiteStrategy.StoreAvailable {
		t.Fatalf("store should be unavailable: %#v", analysis.SQLiteStrategy)
	}

	if analysis.LogSignals.ProtectedBranchCount != 1 ||
		analysis.LogSignals.InlineEnvCount != 1 ||
		analysis.LogSignals.LargeLogCount != 1 {
		t.Fatalf("log signals = %#v", analysis.LogSignals)
	}
}

func TestDownstreamQueriesGroupByDisplayedValues(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "code-intel.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	_, err = store.database.ExecContext(
		ctx,
		`INSERT INTO traces(trace_id, trace_kind, raw_json)
		VALUES ('trace-null', 'hook', '{}'), ('trace-empty', 'hook', '{}');
		INSERT INTO hook_events(trace_id, blocked)
		VALUES ('trace-null', 1), ('trace-empty', 1);
		UPDATE hook_events SET operation_kind = '' WHERE trace_id = 'trace-empty';
		INSERT INTO findings(finding_id, tool, code, message, policy_id, raw_json)
		VALUES
			('finding-null', NULL, NULL, 'sandbox failed', NULL, '{}'),
			('finding-empty', '', '', 'sandbox failed', '', '{}');
		INSERT INTO finding_occurrences(trace_id, ordinal, finding_id, path)
		VALUES
			('trace-null', 0, 'finding-null', ''),
			('trace-empty', 0, 'finding-empty', '');`,
	)
	if err != nil {
		t.Fatalf("seed grouping data: %v", err)
	}

	friction, err := downstreamHookFriction(ctx, store.database, 5)
	if err != nil {
		t.Fatalf("query hook friction: %v", err)
	}
	if len(friction) != 1 || friction[0].Count != 2 {
		t.Fatalf("hook friction did not coalesce displayed values: %#v", friction)
	}

	hotspots, err := downstreamFindingHotspots(ctx, store.database, 5)
	if err != nil {
		t.Fatalf("query finding hotspots: %v", err)
	}
	if len(hotspots) != 1 || hotspots[0].Count != 2 {
		t.Fatalf("finding hotspots did not coalesce displayed values: %#v", hotspots)
	}

	failures, err := downstreamToolchainFailures(ctx, store.database, 5)
	if err != nil {
		t.Fatalf("query toolchain failures: %v", err)
	}
	if len(failures) != 1 || failures[0].Count != 2 {
		t.Fatalf("toolchain failures did not coalesce displayed values: %#v", failures)
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
			TraceID:            "trace-1",
			Tool:               "Bash",
			Status:             "blocked",
			OperationKind:      "lint",
			TargetKind:         "unknown",
			RiskCategory:       "policy_block",
			CommandShapeSHA256: "git-add-shape",
			Blocked:            true,
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
		AgentRemediation: []agentmsg.Remediation{
			{
				ID:       "rem-line-limits",
				PolicyID: "filesystem.line_limits",
				SkillID:  "managed-toolchain",
				File:     "src/big.py",
				Message:  "Large source files must not keep growing",
			},
		},
	})
	if err != nil {
		return err
	}

	err = store.IngestTrace(ctx, Trace{
		ID:            "trace-2",
		Kind:          "hook",
		RecordedAtUTC: "2026-05-24T00:01:00Z",
		Tool:          "Bash",
		Status:        "blocked",
		Raw:           []byte(`{"trace_id":"trace-2"}`),
		AgentRemediation: []agentmsg.Remediation{
			{
				ID:       "rem-line-limits",
				PolicyID: "filesystem.line_limits",
				SkillID:  "managed-toolchain",
				File:     "src/big.py",
				Message:  "Large source files must not keep growing",
			},
		},
	})
	if err != nil {
		return err
	}

	err = store.RecordRemediationOutcome(ctx, RemediationOutcome{
		ID:            "outcome-1",
		RemediationID: "rem-line-limits",
		SourceTraceID: "trace-1",
		PolicyID:      "filesystem.line_limits",
		SkillID:       "managed-toolchain",
		File:          "src/big.py",
		Outcome:       "repeated",
		RecordedAtUTC: "2026-05-24T00:02:00Z",
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

	runDir := filepath.Join(
		root,
		downstreamStateDir,
		downstreamHookRunsDir,
		runID,
	)
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

func writeDownstreamLintRun(
	t *testing.T,
	root string,
	name string,
	content string,
) {
	t.Helper()

	runDir := filepath.Join(root, downstreamStateDir, downstreamLintRunsDir)
	err := os.MkdirAll(runDir, 0o700)
	if err != nil {
		t.Fatalf("create lint run dir: %v", err)
	}

	err = os.WriteFile(filepath.Join(runDir, name), []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write lint run log: %v", err)
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

func assertDownstreamAffectedCommand(
	t *testing.T,
	analysis DownstreamAnalysis,
	policyID string,
	tool string,
) {
	t.Helper()

	for _, blocker := range analysis.PolicyBlockers {
		if blocker.PolicyID != policyID {
			continue
		}

		for _, command := range blocker.AffectedCommands {
			if command.Tool == tool && command.CommandShapeSHA256 != "" {
				return
			}
		}
	}

	t.Fatalf(
		"missing affected command %q %q: %#v",
		policyID,
		tool,
		analysis.PolicyBlockers,
	)
}

func assertDownstreamRemediationLoop(
	t *testing.T,
	analysis DownstreamAnalysis,
	policyID string,
) {
	t.Helper()

	for _, loop := range analysis.RemediationLoops {
		if loop.PolicyID == policyID && loop.OccurrenceCount > 1 &&
			loop.RepeatedCount > 0 {
			return
		}
	}

	t.Fatalf(
		"missing remediation loop %q: %#v",
		policyID,
		analysis.RemediationLoops,
	)
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
