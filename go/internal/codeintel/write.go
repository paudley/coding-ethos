// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

func deleteTraceRows(ctx context.Context, tx *sql.Tx, traceID string) error {
	for _, statement := range []string{
		"DELETE FROM code_intel_fts WHERE trace_id = ?",
		"DELETE FROM remediation_events WHERE trace_id = ?",
		"DELETE FROM remediation_occurrences WHERE trace_id = ?",
		"DELETE FROM finding_occurrences WHERE trace_id = ?",
		"DELETE FROM traces WHERE trace_id = ?",
	} {
		if _, err := tx.ExecContext(ctx, statement, traceID); err != nil {
			return fmt.Errorf("delete existing trace rows: %w", err)
		}
	}

	return nil
}

func insertTrace(ctx context.Context, tx *sql.Tx, trace Trace) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO traces(
			trace_id, trace_kind, recorded_at_utc, repo_root, cwd, provider,
			event, tool, status, source_path, raw_json
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		trace.ID,
		trace.Kind,
		trace.RecordedAtUTC,
		trace.RepoRoot,
		trace.Cwd,
		trace.Provider,
		trace.Event,
		trace.Tool,
		trace.Status,
		trace.SourcePath,
		string(trace.Raw),
	)
	if err != nil {
		return fmt.Errorf("insert trace %q: %w", trace.ID, err)
	}

	return nil
}

func insertFindings(ctx context.Context, tx *sql.Tx, trace Trace) error {
	for index, finding := range trace.Findings {
		raw, err := json.Marshal(finding)
		if err != nil {
			return fmt.Errorf("marshal finding %q: %w", finding.ID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO findings(
				finding_id, rule_id, tool, code, message, severity, policy_id,
				skill_id, path, language, symbol_kind, symbol_name, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			finding.ID,
			finding.RuleID,
			finding.Tool,
			finding.Code,
			finding.Message,
			finding.Severity,
			finding.PolicyID,
			finding.SkillID,
			finding.SourceSpan.Path,
			finding.SourceSpan.Language,
			finding.SourceSpan.SymbolKind,
			finding.SourceSpan.SymbolName,
			finding.SearchText,
			string(raw),
		); err != nil {
			return fmt.Errorf("insert finding %q: %w", finding.ID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO finding_occurrences(
				trace_id, ordinal, finding_id, policy_id, skill_id, path, recorded_at_utc
			) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			trace.ID,
			index,
			finding.ID,
			finding.PolicyID,
			finding.SkillID,
			finding.SourceSpan.Path,
			trace.RecordedAtUTC,
		); err != nil {
			return fmt.Errorf("insert finding occurrence %q: %w", finding.ID, err)
		}
		if err := insertFTS(ctx, tx, ftsRow{
			Kind:       "finding",
			RecordID:   finding.ID,
			TraceID:    trace.ID,
			PolicyID:   finding.PolicyID,
			SkillID:    finding.SkillID,
			Path:       finding.SourceSpan.Path,
			Message:    finding.Message,
			SearchText: finding.SearchText,
		}); err != nil {
			return err
		}
	}

	return nil
}

func insertRemediations(ctx context.Context, tx *sql.Tx, trace Trace) error {
	for index, remediation := range trace.AgentRemediation {
		id := remediation.ID
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("%s:remediation:%d", trace.ID, index)
		}
		search := remediationSearchText(remediation)
		raw, err := json.Marshal(remediation)
		if err != nil {
			return fmt.Errorf("marshal remediation %q: %w", id, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO remediations(
				remediation_id, policy_id, skill_id, file, path, message,
				advice, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			remediation.PolicyID,
			remediation.SkillID,
			remediation.File,
			remediation.Path,
			remediation.Message,
			remediation.Advice,
			search,
			string(raw),
		); err != nil {
			return fmt.Errorf("insert remediation %q: %w", id, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO remediation_occurrences(
				trace_id, ordinal, remediation_id, policy_id, skill_id,
				file, path, line, recorded_at_utc
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			trace.ID,
			index,
			id,
			remediation.PolicyID,
			remediation.SkillID,
			remediation.File,
			remediation.Path,
			remediation.Line,
			trace.RecordedAtUTC,
		); err != nil {
			return fmt.Errorf("insert remediation occurrence %q: %w", id, err)
		}
		if err := insertFTS(ctx, tx, ftsRow{
			Kind:       "remediation",
			RecordID:   id,
			TraceID:    trace.ID,
			PolicyID:   remediation.PolicyID,
			SkillID:    remediation.SkillID,
			Path:       firstNonEmpty(remediation.File, remediation.Path),
			Message:    remediation.Message,
			SearchText: search,
		}); err != nil {
			return err
		}
	}

	return nil
}

func insertRemediationEvents(ctx context.Context, tx *sql.Tx, trace Trace) error {
	for index, event := range trace.RemediationEvents {
		id := event.ID
		if strings.TrimSpace(id) == "" {
			id = fmt.Sprintf("%s:remediation-event:%d", trace.ID, index)
		}
		raw, err := json.Marshal(event)
		if err != nil {
			return fmt.Errorf("marshal remediation event %q: %w", id, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT OR REPLACE INTO remediation_events(
				event_id, trace_id, remediation_id, finding_id, event,
				policy_id, skill_id, search_text, raw_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id,
			trace.ID,
			event.RemediationID,
			event.FindingID,
			event.Event,
			event.PolicyID,
			event.SkillID,
			event.SearchText,
			string(raw),
		); err != nil {
			return fmt.Errorf("insert remediation event %q: %w", id, err)
		}
	}

	return nil
}

type ftsRow struct {
	Kind       string
	RecordID   string
	TraceID    string
	PolicyID   string
	SkillID    string
	Path       string
	Message    string
	SearchText string
}

func insertFTS(ctx context.Context, tx *sql.Tx, row ftsRow) error {
	_, err := tx.ExecContext(
		ctx,
		`INSERT INTO code_intel_fts(
			kind, record_id, trace_id, policy_id, skill_id, path, message, search_text
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		row.Kind,
		row.RecordID,
		row.TraceID,
		row.PolicyID,
		row.SkillID,
		row.Path,
		row.Message,
		row.SearchText,
	)
	if err != nil {
		return fmt.Errorf("insert code intelligence FTS row: %w", err)
	}

	return nil
}

func remediationSearchText(remediation agentmsg.Remediation) string {
	return strings.Join(compactStrings([]string{
		remediation.PolicyID,
		remediation.SkillID,
		remediation.Message,
		remediation.Advice,
		remediation.Command,
		remediation.File,
		remediation.Path,
		strings.Join(remediation.NextSteps, " "),
	}), "\n")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}

	return ""
}

func compactStrings(values []string) []string {
	result := []string{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}

	return result
}

var _ evidence.TraceIngestor = TraceIngester{}
