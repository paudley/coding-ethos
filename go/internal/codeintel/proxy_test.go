// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	. "blackcat.ca/coding-ethos/go/internal/codeintel"
)

func TestRecordProxyEventMaintainsSessionLedger(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	store, err := Open(ctx, filepath.Join(t.TempDir(), "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	event := agentproxy.ProviderEvent{
		ID:            "event-1",
		SessionID:     "session-1",
		Kind:          agentproxy.EventFileRead,
		Provider:      "codex",
		Model:         "gpt-test",
		RecordedAtUTC: time.Date(2026, 5, 6, 1, 2, 3, 0, time.UTC),
		RepoRoot:      "/repo",
		TargetPath:    "pkg/app.py",
		TraceID:       "trace-1",
		TrackingID:    "track-1",
		Direction:     agentproxy.DirectionLocal,
		PayloadKind:   agentproxy.PayloadFileContent,
		CacheKey:      "file:pkg/app.py",
		InputHash:     "input-hash",
		OutputHash:    "output-hash",
		PolicyID:      "proxy.read",
		Decision:      "allow",
		Policy: agentproxy.PolicyEvidence{
			PolicyID:     "proxy.read",
			SkillID:      "agent-operating-discipline",
			Decision:     "allow",
			EvidenceID:   "evidence-1",
			MCPTool:      "policy_explain",
			PrincipleIDs: []string{"security-by-design"},
		},
		DLPFacts: []agentproxy.DLPFact{{
			Type:       "credential_filename",
			Path:       ".env",
			Confidence: "high",
		}},
		TokenUsage: agentproxy.TokenUsage{
			InputTokens:  4,
			OutputTokens: 6,
			TotalTokens:  10,
		},
		Payload: agentproxy.PayloadMeasurement{Bytes: 42, Lines: 2},
		Transforms: []agentproxy.TransformRecord{{
			Name:         "token-budget",
			Reason:       "trim context",
			InputHash:    "before",
			OutputHash:   "after",
			PolicyID:     "proxy.token_budget",
			Decision:     "allow",
			InputTokens:  20,
			OutputTokens: 12,
			BytesRemoved: 128,
		}},
		Metadata: map[string]string{"source": "e2e"},
	}

	err = store.RecordProxyEvent(ctx, event)
	if err != nil {
		t.Fatalf("record proxy event: %v", err)
	}

	sessions, err := store.ProxySessions(ctx, ProxySessionQuery{Provider: "codex"})
	if err != nil {
		t.Fatalf("query proxy sessions: %v", err)
	}

	assertProxySessionLedger(t, sessions)

	events, err := store.ProxyEvents(ctx, ProxyEventQuery{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("query proxy events: %v", err)
	}

	assertProxyEventLedger(t, events)
}

func TestReadFileWithCacheSuppressesRepeatedUnchangedReads(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	err := os.MkdirAll(filepath.Join(root, "pkg"), 0o755)
	if err != nil {
		t.Fatalf("mkdir pkg: %v", err)
	}

	err = os.WriteFile(
		filepath.Join(root, "pkg", "app.py"),
		[]byte("print('hello')\n"),
		0o600,
	)
	if err != nil {
		t.Fatalf("write app.py: %v", err)
	}

	store, err := Open(ctx, filepath.Join(root, ".coding-ethos", "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	request := FileReadCacheRequest{
		SessionID:  "session-1",
		Provider:   "codex",
		Tool:       "Read",
		RepoRoot:   root,
		TargetPath: "pkg/app.py",
	}

	first, err := store.ReadFileWithCache(ctx, request)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	if first.CacheHit || first.Text != "print('hello')\n" {
		t.Fatalf("first read = %#v", first)
	}

	second, err := store.ReadFileWithCache(ctx, request)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}

	if !second.CacheHit ||
		second.Text != "[CACHED: You already have this file in your context. Do not read it again unless instructed.]" ||
		second.Decision != "cache_hit" {
		t.Fatalf("second read = %#v", second)
	}

	sessions, err := store.ProxySessions(ctx, ProxySessionQuery{Provider: "codex"})
	if err != nil {
		t.Fatalf("query proxy sessions: %v", err)
	}

	if len(sessions) != 1 ||
		sessions[0].FileReadCount != 1 ||
		sessions[0].CacheHitCount != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}
}

func TestReadFileWithCacheMissesAfterContentChanges(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "app.py")

	err := os.WriteFile(path, []byte("print('one')\n"), 0o600)
	if err != nil {
		t.Fatalf("write app.py: %v", err)
	}

	store, err := Open(ctx, filepath.Join(root, ".coding-ethos", "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	request := FileReadCacheRequest{
		SessionID:  "session-1",
		RepoRoot:   root,
		TargetPath: "app.py",
	}

	first, err := store.ReadFileWithCache(ctx, request)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	err = os.WriteFile(path, []byte("print('two')\n"), 0o600)
	if err != nil {
		t.Fatalf("rewrite app.py: %v", err)
	}

	second, err := store.ReadFileWithCache(ctx, request)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}

	if second.CacheHit || first.ContentHash == second.ContentHash {
		t.Fatalf("changed file should miss cache: first=%#v second=%#v", first, second)
	}
}

func TestReadFileWithCacheRejectsSymlinkOutsideRoot(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")

	err := os.WriteFile(outside, []byte("outside\n"), 0o600)
	if err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	err = os.Symlink(outside, filepath.Join(root, "link.txt"))
	if err != nil {
		t.Fatalf("symlink outside file: %v", err)
	}

	store, err := Open(ctx, filepath.Join(root, ".coding-ethos", "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	_, err = store.ReadFileWithCache(ctx, FileReadCacheRequest{
		SessionID:  "session-1",
		RepoRoot:   root,
		TargetPath: "link.txt",
	})
	if err == nil || !strings.Contains(err.Error(), "target outside repo root") {
		t.Fatalf("expected outside-root symlink error, got %v", err)
	}
}

func TestReadFileWithCacheRecordsOriginalTransformMeasurements(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	content := strings.Repeat("token ", 80) + "\n"

	err := os.WriteFile(filepath.Join(root, "app.py"), []byte(content), 0o600)
	if err != nil {
		t.Fatalf("write app.py: %v", err)
	}

	store, err := Open(ctx, filepath.Join(root, ".coding-ethos", "code-intel.duckdb"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer store.Close()

	request := FileReadCacheRequest{
		SessionID:  " session-1 ",
		Provider:   "codex",
		RepoRoot:   root,
		TargetPath: "app.py",
	}

	_, err = store.ReadFileWithCache(ctx, request)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}

	second, err := store.ReadFileWithCache(ctx, request)
	if err != nil {
		t.Fatalf("second read: %v", err)
	}

	if !second.CacheHit {
		t.Fatalf("expected cache hit after trimmed session normalization: %#v", second)
	}

	events, err := store.ProxyEvents(ctx, ProxyEventQuery{
		SessionID: "session-1",
		Decision:  "cache_hit",
	})
	if err != nil {
		t.Fatalf("query proxy events: %v", err)
	}

	if len(events) != 1 {
		t.Fatalf("cache-hit events = %#v", events)
	}

	cacheHit := events[0]
	if len(cacheHit.Transforms) != 1 {
		t.Fatalf("cache-hit transforms = %#v", cacheHit.Transforms)
	}

	transform := cacheHit.Transforms[0]
	if transform.InputTokens != (agentproxy.ApproximateTokenizer{}).Count(content) ||
		transform.OutputTokens >= transform.InputTokens ||
		transform.BytesRemoved == 0 {
		t.Fatalf("cache-hit transform = %#v", transform)
	}
}

func assertProxySessionLedger(t *testing.T, sessions []ProxySession) {
	t.Helper()

	if len(sessions) != 1 {
		t.Fatalf("sessions = %#v", sessions)
	}

	session := sessions[0]
	if session.ID != "session-1" ||
		session.FileReadCount != 1 ||
		session.TotalTokens != 10 {
		t.Fatalf("session = %#v", session)
	}
}

func assertProxyEventLedger(t *testing.T, events []ProxyEvent) {
	t.Helper()

	if len(events) != 1 {
		t.Fatalf("events = %#v", events)
	}

	event := events[0]
	assertProxyEventCore(t, event)

	if len(event.Transforms) != 1 ||
		event.Transforms[0].BytesRemoved != 128 ||
		event.Transforms[0].PolicyID != "proxy.token_budget" {
		t.Fatalf("event transforms = %#v", event.Transforms)
	}
}

func assertProxyEventCore(t *testing.T, event ProxyEvent) {
	t.Helper()

	if event.ID != "event-1" ||
		event.TargetPath != "pkg/app.py" ||
		event.TraceID != "trace-1" ||
		event.Direction != "local" ||
		event.PayloadKind != "file_content" ||
		event.CacheKey != "file:pkg/app.py" ||
		event.PayloadBytes != 42 ||
		event.Policy.EvidenceID != "evidence-1" ||
		len(event.DLPFacts) != 1 ||
		event.Metadata["source"] != "e2e" {
		t.Fatalf("event = %#v", event)
	}
}
