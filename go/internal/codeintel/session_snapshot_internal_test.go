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

func TestSessionSnapshotSummarizesProviderBackedActivity(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, ctx)

	err := os.MkdirAll(filepath.Join(root, ".coding-ethos", "memories"), 0o700)
	if err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(root, ".coding-ethos", "memories", "MEMORY.md"),
		[]byte("# Memory\n"),
		0o600,
	); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	err = store.IngestTrace(ctx, Trace{
		ID:            "hook-trace-1",
		Kind:          "hook",
		RecordedAtUTC: "2026-05-28T00:00:00Z",
		RepoRoot:      root,
		Provider:      "codex",
		Event:         "PreToolUse",
		Tool:          "Bash",
		Status:        "blocked",
		Raw:           []byte(`{"event":"PreToolUse"}`),
		HookEvent: &HookEventAnalytics{
			TraceID:       "hook-trace-1",
			TrackingID:    "tracking-1",
			SessionID:     "session-1",
			Provider:      "codex",
			Event:         "PreToolUse",
			Tool:          "Bash",
			Status:        "blocked",
			DecisionCount: 1,
			Blocked:       true,
		},
		HookDecisions: []HookDecisionAnalytics{{
			TraceID:    "hook-trace-1",
			TrackingID: "tracking-1",
			PolicyID:   "policy.block",
			Decision:   "block",
			Severity:   "error",
			Message:    "blocked command",
		}},
	})
	if err != nil {
		t.Fatalf("ingest hook trace: %v", err)
	}

	err = store.IngestTrace(ctx, Trace{
		ID:            "memory-trace-1",
		Kind:          "memory",
		RecordedAtUTC: "2026-05-28T00:01:00Z",
		RepoRoot:      root,
		Provider:      "codex",
		Event:         "memory_import",
		Status:        "ok",
		SourcePath:    ".coding-ethos/memories/index.yaml",
		Raw:           []byte(`{"memory":"import"}`),
	})
	if err != nil {
		t.Fatalf("ingest memory trace: %v", err)
	}

	err = store.RecordProxyEvent(ctx, agentproxy.ProviderEvent{
		ID:            "proxy-event-1",
		SessionID:     "session-1",
		Kind:          agentproxy.EventKind("file_read"),
		Provider:      "codex",
		Model:         "gpt-test",
		RecordedAtUTC: time.Date(2026, 5, 28, 0, 2, 0, 0, time.UTC),
		RepoRoot:      root,
		TargetPath:    "pkg/app.go",
		TokenUsage: agentproxy.TokenUsage{
			InputTokens:  5,
			OutputTokens: 7,
			TotalTokens:  12,
		},
		Transforms: []agentproxy.TransformRecord{{
			Name:         "compression",
			PolicyID:     "proxy.token_budget",
			Decision:     "truncated",
			BytesRemoved: 42,
		}},
	})
	if err != nil {
		t.Fatalf("record proxy event: %v", err)
	}

	snapshot, err := store.SessionSnapshot(ctx, SessionSnapshotQuery{
		Root:      root,
		Worktree:  root,
		Provider:  "codex",
		SessionID: "session-1",
		Now:       time.Date(2026, 5, 28, 0, 3, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("session snapshot: %v", err)
	}

	if snapshot.Kind != SessionSnapshotKind ||
		snapshot.Session.ID != "session-1" ||
		snapshot.Session.Source != "proxy_session" ||
		snapshot.Session.Provider != "codex" ||
		snapshot.Session.Model != "gpt-test" {
		t.Fatalf("session identity = %#v", snapshot.Session)
	}
	if snapshot.Hooks.BlockedEvents != 1 ||
		len(snapshot.CurrentBlockers) != 1 ||
		snapshot.CurrentBlockers[0].PolicyID != "policy.block" {
		t.Fatalf("hook blockers = %#v / %#v", snapshot.Hooks, snapshot.CurrentBlockers)
	}
	if snapshot.Memory.ImportEvents != 1 || !snapshot.Memory.PrimaryExists {
		t.Fatalf("memory summary = %#v", snapshot.Memory)
	}
	if snapshot.Proxy.FileReads != 1 ||
		snapshot.Proxy.OutputCompression != 1 ||
		snapshot.Proxy.BytesRemoved != 42 {
		t.Fatalf("proxy summary = %#v", snapshot.Proxy)
	}
	if _, found := snapshot.Provider.Adapters["proxy"]; !found {
		t.Fatalf("provider adapters missing proxy: %#v", snapshot.Provider.Adapters)
	}
}

func TestSessionSnapshotFallbackAndTOONAreStable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, ctx)

	snapshot, err := store.SessionSnapshot(ctx, SessionSnapshotQuery{
		Root: root,
		Now:  time.Date(2026, 5, 28, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("session snapshot: %v", err)
	}
	if snapshot.Session.Source != "fallback" || snapshot.Kind != SessionSnapshotKind {
		t.Fatalf("fallback snapshot = %#v", snapshot)
	}

	rendered := FormatSessionSnapshotTOON(snapshot)
	for _, want := range []string{
		"kind: coding_ethos.session.v1",
		"session_source: fallback",
		"current_blockers[0]{trace_id,policy_id,severity,decision,message}:",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("TOON output missing %q:\n%s", want, rendered)
		}
	}
}

func TestSessionSnapshotFindsExplicitProxySessionOutsideLimit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, ctx)

	for index := range 12 {
		sessionID := "session-new"
		kind := agentproxy.EventToolCall
		if index == 0 {
			sessionID = "session-old"
			kind = agentproxy.EventPayloadTrim
		}

		err := store.RecordProxyEvent(ctx, agentproxy.ProviderEvent{
			ID:            sessionID + "-event",
			SessionID:     sessionID,
			Kind:          kind,
			Provider:      "codex",
			Model:         sessionID + "-model",
			RecordedAtUTC: time.Date(2026, 5, 28, 0, index, 0, 0, time.UTC),
			RepoRoot:      root,
		})
		if err != nil {
			t.Fatalf("record proxy event %d: %v", index, err)
		}
	}

	snapshot, err := store.SessionSnapshot(ctx, SessionSnapshotQuery{
		Root:      root,
		Provider:  "codex",
		SessionID: "session-old",
		Limit:     1,
		Now:       time.Date(2026, 5, 28, 0, 20, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("session snapshot: %v", err)
	}
	if snapshot.Session.ID != "session-old" ||
		snapshot.Session.Model != "session-old-model" {
		t.Fatalf("explicit session outside limit = %#v", snapshot.Session)
	}
	if snapshot.Proxy.Truncations != 1 {
		t.Fatalf("explicit session proxy summary = %#v", snapshot.Proxy)
	}
}

func TestSessionSnapshotScopesHookSignalsToSelectedSession(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	root := t.TempDir()
	store := openTestStore(t, ctx)

	for _, trace := range []Trace{
		{
			ID:            "trace-old",
			Kind:          "hook",
			RecordedAtUTC: "2026-05-28T00:00:00Z",
			RepoRoot:      root,
			Provider:      "codex",
			Event:         "PreToolUse",
			Tool:          "Bash",
			Status:        "ok",
			Raw:           []byte(`{"event":"PreToolUse"}`),
			HookEvent: &HookEventAnalytics{
				TraceID:   "trace-old",
				SessionID: "session-old",
				Provider:  "codex",
				Event:     "PreToolUse",
				Tool:      "Bash",
				Status:    "ok",
			},
		},
		{
			ID:            "trace-new",
			Kind:          "hook",
			RecordedAtUTC: "2026-05-28T00:10:00Z",
			RepoRoot:      root,
			Provider:      "codex",
			Event:         "PreToolUse",
			Tool:          "Bash",
			Status:        "ok",
			Raw:           []byte(`{"event":"PreToolUse"}`),
			HookEvent: &HookEventAnalytics{
				TraceID:   "trace-new",
				SessionID: "session-new",
				Provider:  "codex",
				Event:     "PreToolUse",
				Tool:      "Bash",
				Status:    "ok",
			},
		},
	} {
		err := store.IngestTrace(ctx, trace)
		if err != nil {
			t.Fatalf("ingest trace %q: %v", trace.ID, err)
		}
	}

	for _, review := range []HookReview{
		{
			ID:            "review-old",
			TraceID:       "trace-old",
			Disposition:   "accepted",
			Reviewer:      "admin",
			RecordedAtUTC: "2026-05-28T00:01:00Z",
		},
		{
			ID:            "review-new",
			TraceID:       "trace-new",
			Disposition:   "accepted",
			Reviewer:      "admin",
			RecordedAtUTC: "2026-05-28T00:11:00Z",
		},
	} {
		err := store.RecordHookReview(ctx, review)
		if err != nil {
			t.Fatalf("record hook review %q: %v", review.ID, err)
		}
	}

	latest, err := store.SessionSnapshot(ctx, SessionSnapshotQuery{
		Root:     root,
		Provider: "codex",
		Now:      time.Date(2026, 5, 28, 0, 20, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("latest session snapshot: %v", err)
	}
	if latest.Session.ID != "session-new" {
		t.Fatalf("latest hook session = %#v", latest.Session)
	}

	scoped, err := store.SessionSnapshot(ctx, SessionSnapshotQuery{
		Root:      root,
		Provider:  "codex",
		SessionID: "session-old",
		Now:       time.Date(2026, 5, 28, 0, 20, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("scoped session snapshot: %v", err)
	}
	if scoped.Hooks.RecentReviewCount != 1 {
		t.Fatalf("scoped review count = %d", scoped.Hooks.RecentReviewCount)
	}
	if len(scoped.LinkedTraceIDs) != 1 || scoped.LinkedTraceIDs[0] != "trace-old" {
		t.Fatalf("scoped linked traces = %#v", scoped.LinkedTraceIDs)
	}
}
