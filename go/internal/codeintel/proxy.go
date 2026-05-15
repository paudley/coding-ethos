// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"blackcat.ca/coding-ethos/go/internal/agentproxy"
	"blackcat.ca/coding-ethos/go/internal/apperror"
)

func (store *Store) RecordProxyEvent(
	ctx context.Context,
	event agentproxy.ProviderEvent,
) error {
	if strings.TrimSpace(event.ID) == "" {
		return apperror.StaticError("proxy event id is required")
	}

	if strings.TrimSpace(event.SessionID) == "" {
		return apperror.StaticError("proxy session id is required")
	}

	if strings.TrimSpace(string(event.Kind)) == "" {
		return apperror.StaticError("proxy event kind is required")
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin proxy event ingest: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	err = upsertProxySession(ctx, transaction, event)
	if err != nil {
		return err
	}

	err = insertProxyEvent(ctx, transaction, event)
	if err != nil {
		return err
	}

	err = insertProxyTransforms(ctx, transaction, event)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit proxy event ingest: %w", err)
	}

	return nil
}

func upsertProxySession(
	ctx context.Context,
	transaction *sql.Tx,
	event agentproxy.ProviderEvent,
) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode proxy session raw event: %w", err)
	}

	counts := proxyEventCounts(event)
	recordedAt := proxyRecordedAt(event)

	_, err = transaction.ExecContext(
		ctx,
		`INSERT INTO proxy_sessions(
			session_id, provider, model, repo_root, started_at_utc, last_seen_utc,
			request_count, tool_call_count, file_read_count, file_listing_count,
			edit_count, cache_hit_count, injection_count, truncation_count,
			denial_count, transform_count, input_tokens, output_tokens,
			total_tokens, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(session_id) DO UPDATE SET
			provider = COALESCE(NULLIF(excluded.provider, ''), proxy_sessions.provider),
			model = COALESCE(NULLIF(excluded.model, ''), proxy_sessions.model),
			repo_root = COALESCE(NULLIF(excluded.repo_root, ''), proxy_sessions.repo_root),
			last_seen_utc = excluded.last_seen_utc,
			request_count = proxy_sessions.request_count + excluded.request_count,
			tool_call_count = proxy_sessions.tool_call_count + excluded.tool_call_count,
			file_read_count = proxy_sessions.file_read_count + excluded.file_read_count,
			file_listing_count = proxy_sessions.file_listing_count + excluded.file_listing_count,
			edit_count = proxy_sessions.edit_count + excluded.edit_count,
			cache_hit_count = proxy_sessions.cache_hit_count + excluded.cache_hit_count,
			injection_count = proxy_sessions.injection_count + excluded.injection_count,
			truncation_count = proxy_sessions.truncation_count + excluded.truncation_count,
			denial_count = proxy_sessions.denial_count + excluded.denial_count,
			transform_count = proxy_sessions.transform_count + excluded.transform_count,
			input_tokens = proxy_sessions.input_tokens + excluded.input_tokens,
			output_tokens = proxy_sessions.output_tokens + excluded.output_tokens,
			total_tokens = proxy_sessions.total_tokens + excluded.total_tokens,
			raw_json = excluded.raw_json`,
		event.SessionID,
		event.Provider,
		event.Model,
		event.RepoRoot,
		recordedAt,
		recordedAt,
		counts.requests,
		counts.toolCalls,
		counts.fileReads,
		counts.fileListings,
		counts.edits,
		counts.cacheHits,
		counts.injections,
		counts.truncations,
		counts.denials,
		len(event.Transforms),
		event.TokenUsage.InputTokens,
		event.TokenUsage.OutputTokens,
		event.TokenUsage.TotalTokens,
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("upsert proxy session %q: %w", event.SessionID, err)
	}

	return nil
}

func insertProxyEvent(
	ctx context.Context,
	transaction *sql.Tx,
	event agentproxy.ProviderEvent,
) error {
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return fmt.Errorf("encode proxy event metadata: %w", err)
	}

	policyEvidence, err := json.Marshal(event.Policy)
	if err != nil {
		return fmt.Errorf("encode proxy event policy evidence: %w", err)
	}

	dlpFacts, err := json.Marshal(event.DLPFacts)
	if err != nil {
		return fmt.Errorf("encode proxy event DLP facts: %w", err)
	}

	raw, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode proxy event raw JSON: %w", err)
	}

	_, err = transaction.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO proxy_events(
				event_id, session_id, event_kind, provider, tool, model,
				recorded_at_utc, trace_id, tracking_id, repo_root, cwd, target_path,
				direction, payload_kind, cache_key, input_hash, output_hash,
				payload_bytes, policy_id, decision, input_tokens, output_tokens,
				total_tokens, policy_evidence_json, dlp_json, metadata_json, raw_json
			) VALUES (
				?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?,
				?, ?, ?
			)`,
		event.ID,
		event.SessionID,
		string(event.Kind),
		event.Provider,
		event.Tool,
		event.Model,
		proxyRecordedAt(event),
		event.TraceID,
		event.TrackingID,
		event.RepoRoot,
		event.Cwd,
		event.TargetPath,
		string(event.Direction),
		string(event.PayloadKind),
		event.CacheKey,
		event.InputHash,
		event.OutputHash,
		event.Payload.Bytes,
		event.PolicyID,
		event.Decision,
		event.TokenUsage.InputTokens,
		event.TokenUsage.OutputTokens,
		event.TokenUsage.TotalTokens,
		string(policyEvidence),
		string(dlpFacts),
		string(metadata),
		string(raw),
	)
	if err != nil {
		return fmt.Errorf("insert proxy event %q: %w", event.ID, err)
	}

	return nil
}

func insertProxyTransforms(
	ctx context.Context,
	transaction *sql.Tx,
	event agentproxy.ProviderEvent,
) error {
	_, err := transaction.ExecContext(
		ctx,
		"DELETE FROM proxy_transforms WHERE event_id = ?",
		event.ID,
	)
	if err != nil {
		return fmt.Errorf("delete proxy transforms %q: %w", event.ID, err)
	}

	for index, transform := range event.Transforms {
		_, err = transaction.ExecContext(
			ctx,
			`INSERT INTO proxy_transforms(
				event_id, ordinal, name, reason, input_hash, output_hash,
				policy_id, decision, evidence_path, input_tokens, output_tokens,
				bytes_removed, findings_count
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			event.ID,
			index,
			transform.Name,
			transform.Reason,
			transform.InputHash,
			transform.OutputHash,
			transform.PolicyID,
			transform.Decision,
			transform.EvidencePath,
			transform.InputTokens,
			transform.OutputTokens,
			transform.BytesRemoved,
			transform.FindingsCount,
		)
		if err != nil {
			return fmt.Errorf("insert proxy transform %q:%d: %w", event.ID, index, err)
		}
	}

	return nil
}

func (store *Store) ProxySessions(
	ctx context.Context,
	query ProxySessionQuery,
) ([]ProxySession, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT session_id, COALESCE(provider, ''), COALESCE(model, ''),
			COALESCE(repo_root, ''), COALESCE(started_at_utc, ''),
			COALESCE(last_seen_utc, ''), request_count, tool_call_count,
			file_read_count, file_listing_count, edit_count, cache_hit_count,
			injection_count, truncation_count, denial_count, transform_count,
			input_tokens, output_tokens, total_tokens
		FROM proxy_sessions
		WHERE (? = '' OR provider = ?)
		ORDER BY last_seen_utc DESC, session_id
		LIMIT ?`,
		query.Provider,
		query.Provider,
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query proxy sessions: %w", err)
	}
	defer rows.Close()

	return scanProxySessions(rows)
}

func (store *Store) ProxyEvents(
	ctx context.Context,
	query ProxyEventQuery,
) ([]ProxyEvent, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT event_id, session_id, event_kind, COALESCE(provider, ''),
			COALESCE(tool, ''), COALESCE(model, ''), COALESCE(recorded_at_utc, ''),
			COALESCE(trace_id, ''), COALESCE(tracking_id, ''),
			COALESCE(repo_root, ''), COALESCE(cwd, ''), COALESCE(target_path, ''),
			COALESCE(direction, ''), COALESCE(payload_kind, ''),
			COALESCE(cache_key, ''), COALESCE(input_hash, ''),
			COALESCE(output_hash, ''), payload_bytes,
			COALESCE(policy_id, ''), COALESCE(decision, ''),
			input_tokens, output_tokens, total_tokens, metadata_json,
			policy_evidence_json, dlp_json
		FROM proxy_events
		WHERE (? = '' OR session_id = ?)
			AND (? = '' OR event_kind = ?)
			AND (? = '' OR provider = ?)
			AND (? = '' OR policy_id = ?)
			AND (? = '' OR decision = ?)
			AND (? = '' OR target_path = ?)
		ORDER BY recorded_at_utc DESC, event_id
		LIMIT ?`,
		query.SessionID,
		query.SessionID,
		query.Kind,
		query.Kind,
		query.Provider,
		query.Provider,
		query.PolicyID,
		query.PolicyID,
		query.Decision,
		query.Decision,
		query.TargetPath,
		query.TargetPath,
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query proxy events: %w", err)
	}
	defer rows.Close()

	events, err := scanProxyEvents(rows)
	if err != nil {
		return nil, err
	}

	for index := range events {
		transforms, transformErr := store.proxyTransforms(ctx, events[index].ID)
		if transformErr != nil {
			return nil, transformErr
		}

		events[index].Transforms = transforms
	}

	return events, nil
}

func scanProxySessions(rows *sql.Rows) ([]ProxySession, error) {
	results := []ProxySession{}

	for rows.Next() {
		var result ProxySession

		err := rows.Scan(
			&result.ID,
			&result.Provider,
			&result.Model,
			&result.RepoRoot,
			&result.StartedAtUTC,
			&result.LastSeenUTC,
			&result.RequestCount,
			&result.ToolCallCount,
			&result.FileReadCount,
			&result.FileListingCount,
			&result.EditCount,
			&result.CacheHitCount,
			&result.InjectionCount,
			&result.TruncationCount,
			&result.DenialCount,
			&result.TransformCount,
			&result.InputTokens,
			&result.OutputTokens,
			&result.TotalTokens,
		)
		if err != nil {
			return nil, fmt.Errorf("scan proxy session: %w", err)
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate proxy sessions: %w", err)
	}

	return results, nil
}

func scanProxyEvents(rows *sql.Rows) ([]ProxyEvent, error) {
	results := []ProxyEvent{}

	for rows.Next() {
		var (
			result         ProxyEvent
			dlpFacts       string
			metadata       string
			policyEvidence string
		)

		err := rows.Scan(
			&result.ID,
			&result.SessionID,
			&result.Kind,
			&result.Provider,
			&result.Tool,
			&result.Model,
			&result.RecordedAtUTC,
			&result.TraceID,
			&result.TrackingID,
			&result.RepoRoot,
			&result.Cwd,
			&result.TargetPath,
			&result.Direction,
			&result.PayloadKind,
			&result.CacheKey,
			&result.InputHash,
			&result.OutputHash,
			&result.PayloadBytes,
			&result.PolicyID,
			&result.Decision,
			&result.InputTokens,
			&result.OutputTokens,
			&result.TotalTokens,
			&metadata,
			&policyEvidence,
			&dlpFacts,
		)
		if err != nil {
			return nil, fmt.Errorf("scan proxy event: %w", err)
		}

		result.Metadata = map[string]string{}
		if strings.TrimSpace(metadata) != "" {
			err = json.Unmarshal([]byte(metadata), &result.Metadata)
			if err != nil {
				return nil, fmt.Errorf("decode proxy event metadata: %w", err)
			}
		}

		if strings.TrimSpace(policyEvidence) != "" {
			err = json.Unmarshal([]byte(policyEvidence), &result.Policy)
			if err != nil {
				return nil, fmt.Errorf("decode proxy event policy evidence: %w", err)
			}
		}

		if strings.TrimSpace(dlpFacts) != "" {
			err = json.Unmarshal([]byte(dlpFacts), &result.DLPFacts)
			if err != nil {
				return nil, fmt.Errorf("decode proxy event DLP facts: %w", err)
			}
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate proxy events: %w", err)
	}

	return results, nil
}

func (store *Store) proxyTransforms(
	ctx context.Context,
	eventID string,
) ([]ProxyTransform, error) {
	rows, err := store.database.QueryContext(
		ctx,
		`SELECT name, COALESCE(reason, ''), COALESCE(input_hash, ''),
			COALESCE(output_hash, ''), COALESCE(policy_id, ''),
			COALESCE(decision, ''), COALESCE(evidence_path, ''), input_tokens,
			output_tokens, bytes_removed, findings_count
		FROM proxy_transforms
		WHERE event_id = ?
		ORDER BY ordinal`,
		eventID,
	)
	if err != nil {
		return nil, fmt.Errorf("query proxy transforms: %w", err)
	}
	defer rows.Close()

	results := []ProxyTransform{}

	for rows.Next() {
		var result ProxyTransform

		err = rows.Scan(
			&result.Name,
			&result.Reason,
			&result.InputHash,
			&result.OutputHash,
			&result.PolicyID,
			&result.Decision,
			&result.EvidencePath,
			&result.InputTokens,
			&result.OutputTokens,
			&result.BytesRemoved,
			&result.FindingsCount,
		)
		if err != nil {
			return nil, fmt.Errorf("scan proxy transform: %w", err)
		}

		results = append(results, result)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate proxy transforms: %w", err)
	}

	return results, nil
}

type proxyEventCounter struct {
	requests     int
	toolCalls    int
	fileReads    int
	fileListings int
	edits        int
	cacheHits    int
	injections   int
	truncations  int
	denials      int
}

func proxyEventCounts(event agentproxy.ProviderEvent) proxyEventCounter {
	counts := proxyEventKindCounts(event.Kind)

	if strings.EqualFold(event.Decision, "deny") ||
		strings.EqualFold(event.Decision, "block") ||
		strings.EqualFold(event.Policy.Decision, "deny") ||
		strings.EqualFold(event.Policy.Decision, "block") {
		counts.denials = 1
	}

	return counts
}

func proxyEventKindCounts(kind agentproxy.EventKind) proxyEventCounter {
	switch kind {
	case agentproxy.EventSessionStarted,
		agentproxy.EventCacheMiss,
		agentproxy.EventSearchRequest,
		agentproxy.EventRemediationAction:
		return proxyEventCounter{}
	case agentproxy.EventProviderCall, agentproxy.EventProviderResponse:
		return proxyEventCounter{requests: 1}
	case agentproxy.EventToolCall, agentproxy.EventToolOutput:
		return proxyEventCounter{toolCalls: 1}
	case agentproxy.EventFileRead:
		return proxyEventCounter{fileReads: 1}
	case agentproxy.EventFileListing:
		return proxyEventCounter{fileListings: 1}
	case agentproxy.EventEditProposal, agentproxy.EventPatchOutcome:
		return proxyEventCounter{edits: 1}
	case agentproxy.EventCacheHit:
		return proxyEventCounter{cacheHits: 1}
	case agentproxy.EventPayloadInject:
		return proxyEventCounter{injections: 1}
	case agentproxy.EventPayloadTrim:
		return proxyEventCounter{truncations: 1}
	}

	return proxyEventCounter{}
}

func proxyRecordedAt(event agentproxy.ProviderEvent) string {
	if event.RecordedAtUTC.IsZero() {
		return time.Now().UTC().Format(time.RFC3339Nano)
	}

	return event.RecordedAtUTC.UTC().Format(time.RFC3339Nano)
}
