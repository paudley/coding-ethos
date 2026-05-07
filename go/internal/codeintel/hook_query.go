// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
)

const defaultQueryLimitValue = 20

func (store *Store) HookUsage(
	ctx context.Context,
	query HookUsageQuery,
) ([]HookUsageSummary, error) {
	rows, err := store.queryHookUsageRows(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanHookUsage(rows)
}

func (store *Store) queryHookUsageRows(
	ctx context.Context,
	query HookUsageQuery,
) (*sql.Rows, error) {
	rows, err := store.database.QueryContext(
		ctx,
		hookUsageSQL,
		query.Provider,
		query.Provider,
		query.Status,
		query.Status,
		query.PolicyID,
		query.PolicyID,
		query.SkillID,
		query.SkillID,
		query.OperationKind,
		query.OperationKind,
		query.TargetKind,
		query.TargetKind,
		query.RiskCategory,
		query.RiskCategory,
		defaultQueryLimit(query.Limit),
	)
	if err != nil {
		return nil, fmt.Errorf("query hook usage: %w", err)
	}

	return rows, nil
}

const hookUsageSQL = `WITH filtered AS (
			SELECT
				event.trace_id,
				event.tracking_id,
				trace.recorded_at_utc,
				event.provider,
				event.tool,
				event.operation_kind,
				event.target_kind,
				event.risk_category,
				event.status,
				decision.policy_id,
				decision.skill_id,
				event.blocked,
				event.rewritten,
				event.runtime_ms
			FROM hook_events event
			JOIN traces trace ON trace.trace_id = event.trace_id
			LEFT JOIN hook_decisions decision ON decision.trace_id = event.trace_id
			WHERE (? = '' OR event.provider = ?)
				AND (? = '' OR event.status = ?)
				AND (? = '' OR decision.policy_id = ?)
				AND (? = '' OR decision.skill_id = ?)
				AND (? = '' OR event.operation_kind = ?)
				AND (? = '' OR event.target_kind = ?)
				AND (? = '' OR event.risk_category = ?)
		),
		summary AS (
			SELECT
				COALESCE(provider, '') AS provider,
				COALESCE(tool, '') AS tool,
				COALESCE(operation_kind, '') AS operation_kind,
				COALESCE(target_kind, '') AS target_kind,
				COALESCE(risk_category, '') AS risk_category,
				COALESCE(status, '') AS status,
				COALESCE(policy_id, '') AS policy_id,
				COALESCE(skill_id, '') AS skill_id,
				COUNT(DISTINCT trace_id) AS event_count,
				COUNT(policy_id) AS decision_count,
					COUNT(DISTINCT CASE
						WHEN blocked != 0 THEN trace_id ELSE NULL
					END) AS blocked_count,
					COUNT(DISTINCT CASE
						WHEN rewritten != 0 THEN trace_id ELSE NULL
					END) AS rewrite_count,
				AVG(CASE WHEN runtime_ms > 0 THEN runtime_ms ELSE NULL END) AS avg_runtime_ms,
				COALESCE(MAX(recorded_at_utc), '') AS last_seen_utc
			FROM filtered
			GROUP BY provider, tool, operation_kind, target_kind, risk_category,
				status, policy_id, skill_id
		)
		SELECT
			summary.provider,
			summary.tool,
			summary.operation_kind,
			summary.target_kind,
			summary.risk_category,
			summary.status,
			summary.policy_id,
			summary.skill_id,
			summary.event_count,
			summary.decision_count,
			summary.blocked_count,
			summary.rewrite_count,
			summary.avg_runtime_ms,
			summary.last_seen_utc,
			COALESCE(MAX(latest.trace_id), '') AS last_trace_id,
			COALESCE(MAX(latest.tracking_id), '') AS last_tracking_id
		FROM summary
		LEFT JOIN filtered latest
			ON COALESCE(latest.provider, '') = summary.provider
			AND COALESCE(latest.tool, '') = summary.tool
			AND COALESCE(latest.operation_kind, '') = summary.operation_kind
			AND COALESCE(latest.target_kind, '') = summary.target_kind
			AND COALESCE(latest.risk_category, '') = summary.risk_category
			AND COALESCE(latest.status, '') = summary.status
			AND COALESCE(latest.policy_id, '') = summary.policy_id
			AND COALESCE(latest.skill_id, '') = summary.skill_id
			AND COALESCE(latest.recorded_at_utc, '') = summary.last_seen_utc
		GROUP BY summary.provider, summary.tool, summary.operation_kind,
			summary.target_kind, summary.risk_category, summary.status,
			summary.policy_id, summary.skill_id
			ORDER BY summary.event_count DESC, summary.blocked_count DESC,
				summary.last_seen_utc DESC
			LIMIT ?`

func defaultQueryLimit(limit int) int {
	if limit > 0 {
		return limit
	}

	return defaultQueryLimitValue
}

func (store *Store) HookReviews(
	ctx context.Context,
	query HookReviewQuery,
) ([]HookReview, error) {
	limit := query.Limit
	if limit <= 0 {
		limit = 20
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT review_id, trace_id, COALESCE(tracking_id, ''),
			disposition, COALESCE(reviewer, ''), COALESCE(notes, ''),
			COALESCE(recorded_at_utc, '')
		FROM hook_reviews
		WHERE (? = '' OR trace_id = ?)
			AND (? = '' OR tracking_id = ?)
			AND (? = '' OR disposition = ?)
		ORDER BY recorded_at_utc DESC, review_id
		LIMIT ?`,
		query.TraceID,
		query.TraceID,
		query.TrackingID,
		query.TrackingID,
		query.Disposition,
		query.Disposition,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query hook reviews: %w", err)
	}
	defer rows.Close()

	results := []HookReview{}

	for rows.Next() {
		var result HookReview

		err := rows.Scan(
			&result.ID,
			&result.TraceID,
			&result.TrackingID,
			&result.Disposition,
			&result.Reviewer,
			&result.Notes,
			&result.RecordedAtUTC,
		)
		if err != nil {
			return nil, fmt.Errorf("scan hook review: %w", err)
		}

		results = append(results, result)
	}

	inlineErr0 := rows.Err()
	if inlineErr0 != nil {
		return nil, fmt.Errorf("iterate hook reviews: %w", inlineErr0)
	}

	return results, nil
}

func scanHookUsage(rows *sql.Rows) ([]HookUsageSummary, error) {
	results := []HookUsageSummary{}

	for rows.Next() {
		var (
			result     HookUsageSummary
			avgRuntime sql.NullFloat64
		)

		err := rows.Scan(
			&result.Provider,
			&result.Tool,
			&result.OperationKind,
			&result.TargetKind,
			&result.RiskCategory,
			&result.Status,
			&result.PolicyID,
			&result.SkillID,
			&result.EventCount,
			&result.DecisionCount,
			&result.BlockedCount,
			&result.RewriteCount,
			&avgRuntime,
			&result.LastSeenUTC,
			&result.LastTraceID,
			&result.LastTrackingID,
		)
		if err != nil {
			return nil, fmt.Errorf("scan hook usage: %w", err)
		}

		if avgRuntime.Valid {
			result.AvgRuntimeMS = avgRuntime.Float64
		}

		results = append(results, result)
	}

	err := rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate hook usage: %w", err)
	}

	return results, nil
}
