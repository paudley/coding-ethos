// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintelcli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"blackcat.ca/coding-ethos/go/internal/codeintel"
	"blackcat.ca/coding-ethos/go/internal/tokeneconomy"
)

func TestTokenEconomyLedgerPrintsSanitizedProviderUsage(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout.jsonl")
	payload := strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"session-1"}}`,
		`{"type":"turn_context","payload":{"model":"gpt-test"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":5,"total_tokens":15}}}}`,
	}, "\n")
	if err := os.WriteFile(path, []byte(payload+"\n"), 0o600); err != nil {
		t.Fatalf("write ledger: %v", err)
	}

	var runErr error
	output := captureStdout(t, func() {
		runErr = run(context.Background(), []string{
			"token-economy",
			"ledger",
			"--provider",
			"codex",
			"--path",
			path,
		})
	})
	if runErr != nil {
		t.Fatalf("token-economy ledger: %v", runErr)
	}
	for _, expected := range []string{
		`"session_id": "session-1"`,
		`"model": "gpt-test"`,
		`"total_tokens": 15`,
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("ledger output missing %s:\n%s", expected, output)
		}
	}
}

func TestTokenEconomyReportRequiresExactlyOneCohort(t *testing.T) {
	err := runCapturingStdout(t, context.Background(), []string{
		"token-economy",
		"report",
		"--output-prefix",
		filepath.Join(t.TempDir(), "report"),
	})
	if err == nil || !strings.Contains(err.Error(), "choose exactly one") {
		t.Fatalf("expected cohort validation failure, got %v", err)
	}
}

func TestTokenEconomyHistoricalReportRequiresSourcesAndWindow(t *testing.T) {
	err := runCapturingStdout(t, context.Background(), []string{
		"token-economy",
		"report",
		"--historical",
		"--output-prefix",
		filepath.Join(t.TempDir(), "report"),
	})
	if err == nil || !strings.Contains(err.Error(), "repeatable --db") {
		t.Fatalf("expected explicit historical input failure, got %v", err)
	}
}

func TestTokenEconomyHistoricalReportAcceptsRepeatableDatabases(t *testing.T) {
	ctx := context.Background()
	first := createCLIHistoricalStore(
		t,
		ctx,
		"codex",
		"session-codex",
		"event-codex",
		100,
		50,
	)
	second := createCLIHistoricalStore(
		t,
		ctx,
		"claude",
		"session-claude",
		"event-claude",
		200,
		80,
	)
	prefix := filepath.Join(t.TempDir(), "historical")

	err := runCapturingStdout(t, ctx, []string{
		"token-economy",
		"report",
		"--historical",
		"--db", second,
		"--db", first,
		"--from", "2026-08-01T00:00:00Z",
		"--to", "2026-09-01T00:00:00Z",
		"--output-prefix", prefix,
	})
	if err != nil {
		t.Fatalf("write historical report from repeated databases: %v", err)
	}

	payload, err := os.ReadFile(prefix + ".json")
	if err != nil {
		t.Fatalf("read historical report artifact: %v", err)
	}
	var report tokeneconomy.Report
	if err = json.Unmarshal(payload, &report); err != nil {
		t.Fatalf("decode historical report artifact: %v", err)
	}
	if report.Historical == nil || len(report.Historical.Sources) != 2 ||
		report.Historical.RawContextTokens != 300 ||
		report.Historical.DeliveredContextTokens != 130 ||
		report.Historical.ProxySessions != 2 {
		t.Fatalf("unexpected repeated-database report: %#v", report)
	}
}

func TestTokenEconomyBenchmarkValidateIsReadOnlyDispatch(t *testing.T) {
	err := runCapturingStdout(t, context.Background(), []string{
		"token-economy",
		"benchmark",
		"validate",
		"--manifest",
		filepath.Join(t.TempDir(), "missing.yaml"),
	})
	if err == nil || !strings.Contains(err.Error(), "validate token-economy benchmark") {
		t.Fatalf("expected benchmark validation failure, got %v", err)
	}
}

func createCLIHistoricalStore(
	t *testing.T,
	ctx context.Context,
	provider string,
	sessionID string,
	eventID string,
	inputTokens int64,
	outputTokens int64,
) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "code-intel.duckdb")
	store, err := codeintel.Open(ctx, path)
	if err != nil {
		t.Fatalf("open CLI historical store: %v", err)
	}

	statements := []struct {
		query string
		args  []any
	}{
		{
			query: `INSERT INTO proxy_sessions(
				session_id, provider, started_at_utc, last_seen_utc, raw_json
			) VALUES (?, ?, '2026-08-10T00:00:00Z', '2026-08-10T00:00:00Z', '{}')`,
			args: []any{sessionID, provider},
		},
		{
			query: `INSERT INTO proxy_events(
				event_id, session_id, event_kind, provider, recorded_at_utc,
				output_tokens, metadata_json, raw_json
			) VALUES (?, ?, 'file_read', ?, '2026-08-10T00:00:00Z', ?, '{}', '{}')`,
			args: []any{eventID, sessionID, provider, outputTokens},
		},
		{
			query: `INSERT INTO proxy_transforms(
				event_id, ordinal, name, input_tokens, output_tokens
			) VALUES (?, 0, 'compact', ?, ?)`,
			args: []any{eventID, inputTokens, outputTokens},
		},
	}
	for _, statement := range statements {
		if _, err = store.Database().ExecContext(
			ctx,
			statement.query,
			statement.args...,
		); err != nil {
			_ = store.Close()
			t.Fatalf("populate CLI historical store: %v", err)
		}
	}
	if err = store.Close(); err != nil {
		t.Fatalf("close CLI historical store: %v", err)
	}

	return path
}
