// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/realgit"
)

const (
	SessionSnapshotKind          = "coding_ethos.session.v1"
	defaultSessionDecisionLimit  = 10
	sessionSnapshotGitTimeBudget = 2 * time.Second
)

type SessionSnapshotQuery struct {
	Now       time.Time
	Root      string
	Worktree  string
	Provider  string
	SessionID string
	Limit     int
}

type SessionSnapshot struct {
	Provider          SessionProviderSummary  `json:"provider"`
	Session           SessionIdentity         `json:"session"`
	Kind              string                  `json:"kind"`
	SchemaVersion     string                  `json:"schema_version"`
	GeneratedAtUTC    string                  `json:"generated_at_utc"`
	CodeIntel         SessionCodeIntelSummary `json:"code_intel"`
	Memory            SessionMemorySummary    `json:"memory"`
	Repository        SessionRepository       `json:"repository"`
	CurrentBlockers   []SessionBlocker        `json:"current_blockers,omitempty"`
	LinkedTraceIDs    []string                `json:"linked_trace_ids,omitempty"`
	RecommendedChecks []string                `json:"recommended_checks,omitempty"`
	Risk              SessionRiskSummary      `json:"risk"`
	Proxy             SessionProxySummary     `json:"proxy"`
	Hooks             SessionHookSummary      `json:"hooks"`
}

type SessionIdentity struct {
	ID       string `json:"id,omitempty"`
	Source   string `json:"source"`
	Provider string `json:"provider,omitempty"`
	Model    string `json:"model,omitempty"`
}

type SessionRepository struct {
	Root        string `json:"root,omitempty"`
	Worktree    string `json:"worktree,omitempty"`
	Branch      string `json:"branch,omitempty"`
	HeadCommit  string `json:"head_commit,omitempty"`
	GitDetected bool   `json:"git_detected"`
}

type SessionHookSummary struct {
	Events            int `json:"events"`
	BlockedEvents     int `json:"blocked_events"`
	DecisionCount     int `json:"decision_count"`
	RewrittenEvents   int `json:"rewritten_events"`
	ContextAdditions  int `json:"context_additions"`
	RecentBlockCount  int `json:"recent_block_count"`
	RecentReviewCount int `json:"recent_review_count"`
}

type SessionMemorySummary struct {
	LastTraceUTC  string `json:"last_trace_utc,omitempty"`
	PrimaryPath   string `json:"primary_path"`
	IndexPath     string `json:"index_path"`
	TraceEvents   int    `json:"trace_events"`
	ImportEvents  int    `json:"import_events"`
	ExportEvents  int    `json:"export_events"`
	PrimaryExists bool   `json:"primary_exists"`
	IndexExists   bool   `json:"index_exists"`
}

type SessionProxySummary struct {
	LastEventUTC      string `json:"last_event_utc,omitempty"`
	LastTargetPath    string `json:"last_target_path,omitempty"`
	Sessions          int    `json:"sessions"`
	Events            int    `json:"events"`
	Transforms        int    `json:"transforms"`
	FileReads         int    `json:"file_reads"`
	FileListings      int    `json:"file_listings"`
	ToolCalls         int    `json:"tool_calls"`
	Truncations       int    `json:"truncations"`
	Denials           int    `json:"denials"`
	CacheHits         int    `json:"cache_hits"`
	InputTokens       int    `json:"input_tokens"`
	OutputTokens      int    `json:"output_tokens"`
	TotalTokens       int    `json:"total_tokens"`
	BytesRemoved      int    `json:"bytes_removed"`
	OutputCompression int    `json:"output_compression"`
}

type SessionCodeIntelSummary struct {
	Freshness           string `json:"freshness"`
	SchemaVersion       int    `json:"schema_version"`
	TraceCount          int    `json:"trace_count"`
	CodeFileCount       int    `json:"code_file_count"`
	CodeChunkCount      int    `json:"code_chunk_count"`
	ProxyEventCount     int    `json:"proxy_event_count"`
	HookEventCount      int    `json:"hook_event_count"`
	HealthSnapshotCount int    `json:"health_snapshot_count"`
	GitSignalFiles      int    `json:"git_signal_files"`
	LinkedTraceCount    int    `json:"linked_trace_count"`
	StoreReady          bool   `json:"store_ready"`
}

type SessionRiskSummary struct {
	Level             string `json:"level"`
	BlockedDecisions  int    `json:"blocked_decisions"`
	DeniedProxyEvents int    `json:"denied_proxy_events"`
	RepeatedFailures  int    `json:"repeated_failures"`
	HealthTargets     int    `json:"health_targets"`
}

type SessionBlocker struct {
	TraceID     string `json:"trace_id,omitempty"`
	TrackingID  string `json:"tracking_id,omitempty"`
	PolicyID    string `json:"policy_id,omitempty"`
	Decision    string `json:"decision,omitempty"`
	Severity    string `json:"severity,omitempty"`
	Message     string `json:"message,omitempty"`
	RecordedUTC string `json:"recorded_utc,omitempty"`
}

type SessionProviderSummary struct {
	Adapters map[string]any `json:"adapters"`
	Name     string         `json:"name,omitempty"`
}

func (store *Store) SessionSnapshot(
	ctx context.Context,
	query SessionSnapshotQuery,
) (SessionSnapshot, error) {
	query = normalizeSessionSnapshotQuery(query)

	stats, err := store.Stats(ctx)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot := newSessionSnapshotBase(query, stats)

	snapshot.Repository.Branch, snapshot.Repository.HeadCommit = sessionGitIdentity(
		ctx,
		query.Root,
	)
	snapshot.Repository.GitDetected = snapshot.Repository.Branch != "" ||
		snapshot.Repository.HeadCommit != ""

	session, err := store.sessionIdentity(ctx, query)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.Session = session
	snapshot.Provider.Name = firstNonEmptyString(query.Provider, session.Provider)

	snapshot.Hooks, err = store.sessionHookSummary(ctx, query)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.Memory, err = store.sessionMemorySummary(ctx, query.Root)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.Proxy, err = store.sessionProxySummary(ctx, query)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.CurrentBlockers, err = store.sessionCurrentBlockers(ctx, query)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.LinkedTraceIDs, err = store.sessionLinkedTraceIDs(ctx, query)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.CodeIntel.LinkedTraceCount = len(snapshot.LinkedTraceIDs)
	snapshot.CodeIntel.Freshness = sessionFreshness(stats)

	snapshot.Risk, err = store.sessionRiskSummary(ctx, &snapshot)
	if err != nil {
		return SessionSnapshot{}, err
	}

	snapshot.Provider.Adapters = sessionProviderAdapters(&snapshot)
	snapshot.RecommendedChecks = sessionRecommendedChecks(&snapshot)

	return snapshot, nil
}

func newSessionSnapshotBase(
	query SessionSnapshotQuery,
	stats Stats,
) SessionSnapshot {
	return SessionSnapshot{
		Kind:           SessionSnapshotKind,
		SchemaVersion:  "1",
		GeneratedAtUTC: query.Now.UTC().Format(time.RFC3339Nano),
		Repository: SessionRepository{
			Root:     query.Root,
			Worktree: query.Worktree,
		},
		CodeIntel: SessionCodeIntelSummary{
			StoreReady:          true,
			SchemaVersion:       stats.SchemaVersion,
			TraceCount:          stats.Traces,
			CodeFileCount:       stats.Files,
			CodeChunkCount:      stats.CodeChunks,
			ProxyEventCount:     stats.ProxyEvents,
			HookEventCount:      stats.HookEvents,
			HealthSnapshotCount: stats.CodeHealthSnapshots,
			GitSignalFiles:      stats.GitFileSignals,
		},
		Provider: SessionProviderSummary{Adapters: map[string]any{}},
	}
}

func normalizeSessionSnapshotQuery(query SessionSnapshotQuery) SessionSnapshotQuery {
	query.Root = strings.TrimSpace(query.Root)
	query.Worktree = strings.TrimSpace(query.Worktree)
	query.Provider = strings.TrimSpace(query.Provider)

	query.SessionID = strings.TrimSpace(query.SessionID)
	if query.Limit <= 0 {
		query.Limit = defaultSessionDecisionLimit
	}

	if query.Now.IsZero() {
		query.Now = time.Now().UTC()
	}

	return query
}

func (store *Store) sessionIdentity(
	ctx context.Context,
	query SessionSnapshotQuery,
) (SessionIdentity, error) {
	identity := SessionIdentity{
		ID:       query.SessionID,
		Provider: query.Provider,
		Source:   "fallback",
	}
	if query.SessionID != "" {
		identity.Source = "explicit"
	}

	session, found, err := store.latestProxySession(ctx, query)
	if err != nil {
		return SessionIdentity{}, err
	}

	if found {
		identity.ID = firstNonEmptyString(identity.ID, session.ID)
		identity.Provider = firstNonEmptyString(identity.Provider, session.Provider)
		identity.Model = session.Model
		identity.Source = "proxy_session"

		return identity, nil
	}

	hook, found, err := store.latestHookSession(ctx, query)
	if err != nil {
		return SessionIdentity{}, err
	}

	if found {
		identity.ID = firstNonEmptyString(identity.ID, hook.SessionID, hook.TraceID)
		identity.Provider = firstNonEmptyString(identity.Provider, hook.Provider)
		identity.Source = "hook_trace"
	}

	return identity, nil
}

func (store *Store) latestProxySession(
	ctx context.Context,
	query SessionSnapshotQuery,
) (ProxySession, bool, error) {
	sessions, err := store.ProxySessions(ctx, ProxySessionQuery{
		SessionID: query.SessionID,
		Provider:  query.Provider,
		Limit:     max(query.Limit, 1),
	})
	if err != nil {
		return ProxySession{}, false, err
	}

	for _, session := range sessions {
		if query.SessionID == "" || session.ID == query.SessionID {
			return session, true, nil
		}
	}

	return ProxySession{}, false, nil
}

func (store *Store) latestHookSession(
	ctx context.Context,
	query SessionSnapshotQuery,
) (HookEventAnalytics, bool, error) {
	row := store.database.QueryRowContext(
		ctx,
		`SELECT event.trace_id, COALESCE(event.session_id, ''), COALESCE(event.provider, '')
		FROM hook_events event
		JOIN traces trace ON trace.trace_id = event.trace_id
		WHERE (? = '' OR event.provider = ?)
			AND (? = '' OR event.session_id = ?)
		ORDER BY trace.recorded_at_utc DESC, event.trace_id DESC
		LIMIT 1`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
	)

	var event HookEventAnalytics

	err := row.Scan(&event.TraceID, &event.SessionID, &event.Provider)
	if err == nil {
		return event, true, nil
	}

	if errors.Is(err, sql.ErrNoRows) {
		return HookEventAnalytics{}, false, nil
	}

	return HookEventAnalytics{}, false, fmt.Errorf("query latest hook session: %w", err)
}

func (store *Store) sessionHookSummary(
	ctx context.Context,
	query SessionSnapshotQuery,
) (SessionHookSummary, error) {
	row := store.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*), COALESCE(SUM(blocked), 0),
			COALESCE(SUM(decision_count), 0), COALESCE(SUM(rewritten), 0),
			COALESCE(SUM(additional_context), 0)
		FROM hook_events
		WHERE (? = '' OR provider = ?)
			AND (? = '' OR session_id = ?)`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
	)

	var summary SessionHookSummary

	err := row.Scan(
		&summary.Events,
		&summary.BlockedEvents,
		&summary.DecisionCount,
		&summary.RewrittenEvents,
		&summary.ContextAdditions,
	)
	if err != nil {
		return SessionHookSummary{}, fmt.Errorf("query hook session summary: %w", err)
	}

	recentBlocks, err := store.sessionCurrentBlockers(ctx, query)
	if err != nil {
		return SessionHookSummary{}, err
	}

	summary.RecentBlockCount = len(recentBlocks)

	reviewCount, err := store.sessionHookReviewCount(ctx, query)
	if err != nil {
		return SessionHookSummary{}, err
	}

	summary.RecentReviewCount = reviewCount

	return summary, nil
}

func (store *Store) sessionHookReviewCount(
	ctx context.Context,
	query SessionSnapshotQuery,
) (int, error) {
	row := store.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM hook_reviews review
		JOIN hook_events event ON event.trace_id = review.trace_id
		WHERE (? = '' OR event.provider = ?)
			AND (? = '' OR event.session_id = ?)`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
	)

	var count int

	err := row.Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query hook review count: %w", err)
	}

	return count, nil
}

func (store *Store) sessionMemorySummary(
	ctx context.Context,
	root string,
) (SessionMemorySummary, error) {
	summary := SessionMemorySummary{
		PrimaryPath: ".coding-ethos/memories/MEMORY.md",
		IndexPath:   ".coding-ethos/memories/index.yaml",
	}
	row := store.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*),
			COALESCE(SUM(CASE
				WHEN lower(COALESCE(raw_json, '')) LIKE '%import%' THEN 1 ELSE 0
			END), 0),
			COALESCE(SUM(CASE
				WHEN lower(COALESCE(raw_json, '')) LIKE '%export%' THEN 1 ELSE 0
			END), 0),
			COALESCE(MAX(recorded_at_utc), '')
		FROM traces
		WHERE lower(COALESCE(event, '') || ' ' || COALESCE(tool, '') || ' ' ||
			COALESCE(source_path, '') || ' ' || COALESCE(raw_json, '')) LIKE '%memory%'`,
	)

	err := row.Scan(
		&summary.TraceEvents,
		&summary.ImportEvents,
		&summary.ExportEvents,
		&summary.LastTraceUTC,
	)
	if err != nil {
		return SessionMemorySummary{}, fmt.Errorf("query memory session summary: %w", err)
	}

	summary.PrimaryExists = fileExists(root, summary.PrimaryPath)
	summary.IndexExists = fileExists(root, summary.IndexPath)

	return summary, nil
}

func (store *Store) sessionProxySummary(
	ctx context.Context,
	query SessionSnapshotQuery,
) (SessionProxySummary, error) {
	summary, err := store.sessionProxyEventSummary(ctx, query)
	if err != nil {
		return SessionProxySummary{}, err
	}

	err = store.addSessionProxyTransformSummary(ctx, query, &summary)
	if err != nil {
		return SessionProxySummary{}, err
	}

	sessions, err := store.ProxySessions(ctx, ProxySessionQuery{
		SessionID: query.SessionID,
		Provider:  query.Provider,
		Limit:     query.Limit,
	})
	if err != nil {
		return SessionProxySummary{}, err
	}

	for _, session := range sessions {
		if query.SessionID != "" && session.ID != query.SessionID {
			continue
		}

		summary.Truncations += session.TruncationCount
		summary.CacheHits += session.CacheHitCount
	}

	return summary, nil
}

func (store *Store) sessionProxyEventSummary(
	ctx context.Context,
	query SessionSnapshotQuery,
) (SessionProxySummary, error) {
	row := store.database.QueryRowContext(
		ctx,
		`SELECT COUNT(DISTINCT session_id), COUNT(*),
			COALESCE(SUM(CASE WHEN event_kind = 'file_read' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_kind = 'file_listing' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN event_kind = 'tool_call' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE
				WHEN decision = 'deny' OR decision = 'blocked' THEN 1
				ELSE 0
			END), 0),
			COALESCE(SUM(input_tokens), 0), COALESCE(SUM(output_tokens), 0),
			COALESCE(SUM(total_tokens), 0), COALESCE(MAX(recorded_at_utc), ''),
			COALESCE(MAX(target_path), '')
		FROM proxy_events
		WHERE (? = '' OR provider = ?)
			AND (? = '' OR session_id = ?)`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
	)

	var summary SessionProxySummary

	err := row.Scan(
		&summary.Sessions,
		&summary.Events,
		&summary.FileReads,
		&summary.FileListings,
		&summary.ToolCalls,
		&summary.Denials,
		&summary.InputTokens,
		&summary.OutputTokens,
		&summary.TotalTokens,
		&summary.LastEventUTC,
		&summary.LastTargetPath,
	)
	if err != nil {
		return SessionProxySummary{}, fmt.Errorf("query proxy session summary: %w", err)
	}

	return summary, nil
}

func (store *Store) addSessionProxyTransformSummary(
	ctx context.Context,
	query SessionSnapshotQuery,
	summary *SessionProxySummary,
) error {
	transformRow := store.database.QueryRowContext(
		ctx,
		`SELECT COUNT(*), COALESCE(SUM(proxy_transforms.bytes_removed), 0),
			COALESCE(SUM(CASE
				WHEN proxy_transforms.policy_id = 'proxy.token_budget'
					OR proxy_transforms.policy_id = 'proxy.saved_output_notice'
					OR proxy_transforms.name LIKE '%compression%'
				THEN 1 ELSE 0 END), 0)
		FROM proxy_transforms
		JOIN proxy_events ON proxy_events.event_id = proxy_transforms.event_id
		WHERE (? = '' OR proxy_events.provider = ?)
			AND (? = '' OR proxy_events.session_id = ?)`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
	)

	err := transformRow.Scan(
		&summary.Transforms,
		&summary.BytesRemoved,
		&summary.OutputCompression,
	)
	if err != nil {
		return fmt.Errorf("query proxy transform summary: %w", err)
	}

	return nil
}

func (store *Store) sessionCurrentBlockers(
	ctx context.Context,
	query SessionSnapshotQuery,
) ([]SessionBlocker, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT decision.trace_id, COALESCE(decision.tracking_id, ''),
			COALESCE(decision.policy_id, ''), COALESCE(decision.decision, ''),
			COALESCE(decision.severity, ''), COALESCE(decision.message, ''),
			COALESCE(trace.recorded_at_utc, '')
		FROM hook_decisions decision
		JOIN traces trace ON trace.trace_id = decision.trace_id
		LEFT JOIN hook_events event ON event.trace_id = decision.trace_id
		WHERE (? = '' OR event.provider = ?)
			AND (? = '' OR event.session_id = ?)
			AND (
				lower(COALESCE(decision.decision, '')) IN ('block', 'blocked', 'deny')
				OR lower(COALESCE(decision.severity, '')) IN ('error', 'critical')
			)
		ORDER BY trace.recorded_at_utc DESC, decision.trace_id DESC
		LIMIT ?`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query session blockers: %w", err)
	}
	defer rows.Close()

	blockers := []SessionBlocker{}

	for rows.Next() {
		var blocker SessionBlocker

		err = rows.Scan(
			&blocker.TraceID,
			&blocker.TrackingID,
			&blocker.PolicyID,
			&blocker.Decision,
			&blocker.Severity,
			&blocker.Message,
			&blocker.RecordedUTC,
		)
		if err != nil {
			return nil, fmt.Errorf("scan session blocker: %w", err)
		}

		blockers = append(blockers, blocker)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate session blockers: %w", err)
	}

	return blockers, nil
}

func (store *Store) sessionLinkedTraceIDs(
	ctx context.Context,
	query SessionSnapshotQuery,
) ([]string, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT trace.trace_id
		FROM traces trace
		WHERE (? = '' OR trace.provider = ?)
			AND (
				? = ''
				OR EXISTS (
					SELECT 1 FROM hook_events event
					WHERE event.trace_id = trace.trace_id
						AND event.session_id = ?
				)
				OR EXISTS (
					SELECT 1 FROM proxy_events event
					WHERE event.trace_id = trace.trace_id
						AND event.session_id = ?
				)
			)
		ORDER BY trace.recorded_at_utc DESC, trace.trace_id DESC
		LIMIT ?`,
		query.Provider,
		query.Provider,
		query.SessionID,
		query.SessionID,
		query.SessionID,
		query.Limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query linked session traces: %w", err)
	}
	defer rows.Close()

	traceIDs := []string{}

	for rows.Next() {
		var traceID string

		err = rows.Scan(&traceID)
		if err != nil {
			return nil, fmt.Errorf("scan linked session trace: %w", err)
		}

		traceIDs = append(traceIDs, traceID)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate linked session traces: %w", err)
	}

	return traceIDs, nil
}

func (store *Store) sessionRiskSummary(
	ctx context.Context,
	snapshot *SessionSnapshot,
) (SessionRiskSummary, error) {
	repeated, err := store.RepeatedFailures(ctx, RepeatedFailureQuery{
		Limit: defaultSessionDecisionLimit,
	})
	if err != nil {
		return SessionRiskSummary{}, err
	}

	risk := SessionRiskSummary{
		BlockedDecisions:  snapshot.Hooks.BlockedEvents,
		DeniedProxyEvents: snapshot.Proxy.Denials,
		RepeatedFailures:  len(repeated),
		HealthTargets:     snapshot.CodeIntel.HealthSnapshotCount,
		Level:             "normal",
	}
	if len(snapshot.CurrentBlockers) > 0 ||
		risk.BlockedDecisions > 0 ||
		risk.DeniedProxyEvents > 0 {
		risk.Level = "blocked"
	} else if risk.RepeatedFailures > 0 || snapshot.Proxy.Truncations > 0 {
		risk.Level = "watch"
	}

	return risk, nil
}

func sessionGitIdentity(ctx context.Context, root string) (string, string) {
	if strings.TrimSpace(root) == "" {
		return "", ""
	}

	gitCtx, cancel := context.WithTimeout(ctx, sessionSnapshotGitTimeBudget)
	defer cancel()

	branch := sessionGitOutput(gitCtx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if branch == "" {
		return "", ""
	}

	head := sessionGitOutput(gitCtx, root, "rev-parse", "--verify", "HEAD")

	return branch, head
}

func sessionGitOutput(ctx context.Context, root string, args ...string) string {
	command := realgit.Command(ctx, false, append([]string{"-C", root}, args...)...)

	output, err := command.Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}

func sessionFreshness(stats Stats) string {
	if stats.Files == 0 && stats.CodeChunks == 0 {
		return "missing_index"
	}

	if stats.GitFileSignals == 0 || stats.CodeHealthSnapshots == 0 {
		return "partial"
	}

	return "ready"
}

func sessionProviderAdapters(snapshot *SessionSnapshot) map[string]any {
	adapters := map[string]any{
		"hook_traces": map[string]any{
			"events":           snapshot.Hooks.Events,
			"blocked_events":   snapshot.Hooks.BlockedEvents,
			"decision_count":   snapshot.Hooks.DecisionCount,
			"linked_trace_ids": snapshot.LinkedTraceIDs,
		},
		"proxy": map[string]any{
			"sessions":     snapshot.Proxy.Sessions,
			"events":       snapshot.Proxy.Events,
			"file_reads":   snapshot.Proxy.FileReads,
			"truncations":  snapshot.Proxy.Truncations,
			"total_tokens": snapshot.Proxy.TotalTokens,
		},
		"memory": map[string]any{
			"trace_events":   snapshot.Memory.TraceEvents,
			"primary_exists": snapshot.Memory.PrimaryExists,
			"index_exists":   snapshot.Memory.IndexExists,
		},
	}
	if snapshot.Session.Model != "" {
		adapters["model"] = map[string]any{"name": snapshot.Session.Model}
	}

	return adapters
}

func sessionRecommendedChecks(snapshot *SessionSnapshot) []string {
	checks := []string{}
	if len(snapshot.CurrentBlockers) > 0 {
		checks = append(checks, "Review current_blockers before editing.")
	}

	if snapshot.CodeIntel.Freshness != "ready" {
		checks = append(checks, "Refresh code intelligence before broad code work.")
	}

	if snapshot.Proxy.Truncations > 0 || snapshot.Proxy.OutputCompression > 0 {
		checks = append(
			checks,
			"Inspect compressed proxy outputs before relying on summaries.",
		)
	}

	if len(checks) == 0 {
		checks = append(checks, "Proceed with focused checks for the intended change.")
	}

	return checks
}

func fileExists(root, relativePath string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(relativePath) == "" {
		return false
	}

	_, err := os.Stat(filepath.Join(root, filepath.FromSlash(relativePath)))

	return err == nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	return ""
}
