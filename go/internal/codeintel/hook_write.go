// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

func insertHookAnalytics(ctx context.Context, tx *sql.Tx, trace Trace) error {
	if trace.HookEvent == nil {
		return nil
	}
	if err := insertHookEvent(ctx, tx, *trace.HookEvent); err != nil {
		return err
	}
	for _, decision := range trace.HookDecisions {
		if err := insertHookDecision(ctx, tx, decision); err != nil {
			return err
		}
	}
	for _, target := range trace.HookTargets {
		if err := insertHookTarget(ctx, tx, target); err != nil {
			return err
		}
	}

	return insertFTS(ctx, tx, ftsRow{
		Kind:       "hook_event",
		RecordID:   trace.HookEvent.TraceID,
		TraceID:    trace.HookEvent.TraceID,
		PolicyID:   firstHookPolicy(trace.HookDecisions),
		SkillID:    firstHookSkill(trace.HookDecisions),
		Path:       firstHookTarget(trace.HookTargets),
		Message:    trace.HookEvent.RiskCategory,
		SearchText: hookEventSearchText(*trace.HookEvent, trace.HookDecisions, trace.HookTargets),
	})
}

func insertHookEvent(ctx context.Context, tx *sql.Tx, event HookEventAnalytics) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO hook_events(
			trace_id, tracking_id, session_id, provider, event, tool, status,
			operation_kind, target_kind, risk_category, command_sha256,
			command_shape_sha256, target_set_sha256, cwd, source, matcher,
			transcript_path, runtime_ms, decision_count, blocked, rewritten,
			additional_context
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.TraceID,
		event.TrackingID,
		event.SessionID,
		event.Provider,
		event.Event,
		event.Tool,
		event.Status,
		event.OperationKind,
		event.TargetKind,
		event.RiskCategory,
		event.CommandSHA256,
		event.CommandShapeSHA256,
		event.TargetSetSHA256,
		event.Cwd,
		event.Source,
		event.Matcher,
		event.TranscriptPath,
		event.RuntimeMS,
		event.DecisionCount,
		boolInt(event.Blocked),
		boolInt(event.Rewritten),
		boolInt(event.AdditionalContext),
	)
	if err != nil {
		return fmt.Errorf("insert hook event %q: %w", event.TraceID, err)
	}

	return nil
}

func insertHookDecision(ctx context.Context, tx *sql.Tx, decision HookDecisionAnalytics) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO hook_decisions(
			trace_id, ordinal, tracking_id, policy_id, decision, severity,
			skill_id, implementation, principle_ids, diagnostic_count,
			message_hash, suggestion_hash, message, suggestion
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		decision.TraceID,
		decision.DecisionOrdinal,
		decision.TrackingID,
		decision.PolicyID,
		decision.Decision,
		decision.Severity,
		decision.SkillID,
		decision.Implementation,
		strings.Join(decision.PrincipleIDs, ","),
		decision.DiagnosticCount,
		decision.MessageHash,
		decision.SuggestionHash,
		decision.Message,
		decision.Suggestion,
	)
	if err != nil {
		return fmt.Errorf("insert hook decision %q:%d: %w", decision.TraceID, decision.DecisionOrdinal, err)
	}

	return nil
}

func insertHookTarget(ctx context.Context, tx *sql.Tx, target HookTargetAnalytics) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO hook_targets(
			trace_id, ordinal, target_path, target_kind
		) VALUES (?, ?, ?, ?)`,
		target.TraceID,
		target.TargetIndex,
		target.TargetPath,
		target.TargetKind,
	)
	if err != nil {
		return fmt.Errorf("insert hook target %q:%d: %w", target.TraceID, target.TargetIndex, err)
	}

	return nil
}

func insertHookReview(ctx context.Context, tx *sql.Tx, review HookReview) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT OR REPLACE INTO hook_reviews(
			review_id, trace_id, tracking_id, disposition, reviewer, notes,
			recorded_at_utc
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		review.ID,
		review.TraceID,
		review.TrackingID,
		review.Disposition,
		review.Reviewer,
		review.Notes,
		review.RecordedAtUTC,
	)
	if err != nil {
		return fmt.Errorf("insert hook review %q: %w", review.ID, err)
	}

	return insertFTS(ctx, tx, ftsRow{
		Kind:       "hook_review",
		RecordID:   review.ID,
		TraceID:    review.TraceID,
		Message:    review.Disposition,
		SearchText: strings.Join(compactStrings([]string{review.Disposition, review.Reviewer, review.Notes}), "\n"),
	})
}

func hookEventSearchText(
	event HookEventAnalytics,
	decisions []HookDecisionAnalytics,
	targets []HookTargetAnalytics,
) string {
	values := []string{
		event.Provider,
		event.Event,
		event.Tool,
		event.Status,
		event.OperationKind,
		event.TargetKind,
		event.RiskCategory,
		event.TrackingID,
	}
	for _, decision := range decisions {
		values = append(
			values,
			decision.PolicyID,
			decision.SkillID,
			decision.Decision,
			decision.Severity,
			decision.Implementation,
			decision.Message,
			decision.Suggestion,
			strings.Join(decision.PrincipleIDs, " "),
		)
	}
	for _, target := range targets {
		values = append(values, target.TargetPath, target.TargetKind)
	}

	return strings.Join(compactStrings(values), "\n")
}

func firstHookPolicy(decisions []HookDecisionAnalytics) string {
	for _, decision := range decisions {
		if strings.TrimSpace(decision.PolicyID) != "" {
			return decision.PolicyID
		}
	}

	return ""
}

func firstHookSkill(decisions []HookDecisionAnalytics) string {
	for _, decision := range decisions {
		if strings.TrimSpace(decision.SkillID) != "" {
			return decision.SkillID
		}
	}

	return ""
}

func firstHookTarget(targets []HookTargetAnalytics) string {
	for _, target := range targets {
		if strings.TrimSpace(target.TargetPath) != "" {
			return target.TargetPath
		}
	}

	return ""
}
