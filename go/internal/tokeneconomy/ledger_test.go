// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package tokeneconomy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCodexLedgerUsesFinalCumulativeUsage(t *testing.T) {
	t.Parallel()

	path := writeLedgerFixture(t, strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"session-1"}}`,
		`{"type":"turn_context","payload":{"model":"gpt-test"}}`,
		`{"timestamp":"2026-08-28T01:00:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":80,"output_tokens":20,"reasoning_output_tokens":5,"total_tokens":120}}}}`,
		`{"timestamp":"2026-08-28T01:01:00Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":250,"cached_input_tokens":200,"output_tokens":40,"reasoning_output_tokens":9,"total_tokens":290}}}}`,
	}, "\n"))

	ledger, err := ParseLedger(ProviderCodex, path)
	if err != nil {
		t.Fatalf("parse Codex ledger: %v", err)
	}

	if ledger.SessionID != "session-1" || ledger.Model != "gpt-test" {
		t.Fatalf("unexpected Codex identity: %#v", ledger)
	}
	if ledger.Usage.TotalTokens != 290 || ledger.Usage.CachedInputTokens != 200 {
		t.Fatalf("unexpected final Codex usage: %#v", ledger.Usage)
	}
	if len(ledger.Events) != 2 || ledger.Events[0].UsageKind != "cumulative" {
		t.Fatalf("unexpected Codex events: %#v", ledger.Events)
	}
	if len(ledger.SourceSHA256) != 64 {
		t.Fatalf("unexpected source digest: %q", ledger.SourceSHA256)
	}
}

func TestParseCodexLedgerRejectsDecreasingCumulativeUsage(t *testing.T) {
	t.Parallel()

	path := writeLedgerFixture(t, strings.Join([]string{
		`{"type":"session_meta","payload":{"id":"session-1"}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20,"total_tokens":120}}}}`,
		`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":90,"output_tokens":20,"total_tokens":110}}}}`,
	}, "\n"))

	_, err := ParseLedger(ProviderCodex, path)
	if err == nil || !strings.Contains(err.Error(), "cumulative usage decreased") {
		t.Fatalf("expected decreasing usage failure, got %v", err)
	}
}

func TestUsageDecreasedCoversCumulativeCacheCounters(t *testing.T) {
	t.Parallel()

	previous := TokenUsage{
		CacheCreationInputTokens: 20,
		CacheReadInputTokens:     30,
	}
	for name, current := range map[string]TokenUsage{
		"cache creation": {CacheCreationInputTokens: 19, CacheReadInputTokens: 30},
		"cache read":     {CacheCreationInputTokens: 20, CacheReadInputTokens: 29},
	} {
		if !usageDecreased(previous, current) {
			t.Errorf(
				"%s decrease was accepted: previous=%#v current=%#v",
				name,
				previous,
				current,
			)
		}
	}
}

func TestParseClaudeLedgerDeduplicatesMessagesAndExcludesSyntheticUsage(t *testing.T) {
	t.Parallel()

	realMessage := `{"type":"assistant","sessionId":"session-2","timestamp":"2026-08-28T02:00:00Z","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":2,"cache_creation_input_tokens":20,"cache_read_input_tokens":30,"output_tokens":4}}}`
	path := writeLedgerFixture(t, strings.Join([]string{
		`{"type":"assistant","sessionId":"session-2","message":{"id":"synthetic","model":"<synthetic>","usage":{"input_tokens":999}}}`,
		realMessage,
		realMessage,
		`{"type":"assistant","sessionId":"session-2","message":{"id":"msg-2","model":"claude-test","usage":{"input_tokens":3,"cache_creation_input_tokens":10,"cache_read_input_tokens":5,"output_tokens":6}}}`,
	}, "\n"))

	ledger, err := ParseLedger(ProviderClaude, path)
	if err != nil {
		t.Fatalf("parse Claude ledger: %v", err)
	}

	if ledger.SessionID != "session-2" || ledger.Model != "claude-test" {
		t.Fatalf("unexpected Claude identity: %#v", ledger)
	}
	if len(ledger.Events) != 2 {
		t.Fatalf("duplicate Claude messages were retained: %#v", ledger.Events)
	}
	if ledger.Usage.TotalTokens != 80 || ledger.Usage.CacheReadInputTokens != 35 {
		t.Fatalf("unexpected Claude usage: %#v", ledger.Usage)
	}
}

func TestParseClaudeLedgerRejectsConflictingDuplicateMessages(t *testing.T) {
	t.Parallel()

	path := writeLedgerFixture(t, strings.Join([]string{
		`{"type":"assistant","sessionId":"session-2","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":2,"output_tokens":4}}}`,
		`{"type":"assistant","sessionId":"session-2","message":{"id":"msg-1","model":"claude-test","usage":{"input_tokens":3,"output_tokens":4}}}`,
	}, "\n"))

	_, err := ParseLedger(ProviderClaude, path)
	if err == nil || !strings.Contains(err.Error(), "conflicting usage") {
		t.Fatalf("expected conflicting duplicate failure, got %v", err)
	}
}

func TestParseLedgerRequiresAbsolutePath(t *testing.T) {
	t.Parallel()

	_, err := ParseLedger(ProviderCodex, "relative.jsonl")
	if err == nil || !strings.Contains(err.Error(), "must be absolute") {
		t.Fatalf("expected absolute path failure, got %v", err)
	}
}

func writeLedgerFixture(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "ledger.jsonl")
	err := os.WriteFile(path, []byte(content+"\n"), 0o600)
	if err != nil {
		t.Fatalf("write ledger fixture: %v", err)
	}

	return path
}
