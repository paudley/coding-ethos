// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
	"blackcat.ca/coding-ethos/go/internal/apperror"
	"blackcat.ca/coding-ethos/go/internal/evidence"
)

const (
	automaticRemediationOutcomeIDPrefix  = "automatic-remediation-outcome"
	automaticRemediationOutcomeFixed     = "fixed"
	automaticRemediationOutcomeAbandoned = "abandoned"
)

type IngestSummary struct {
	FilesScanned  int `json:"files_scanned"`
	FilesIngested int `json:"files_ingested"`
}

type Trace struct {
	ID                string
	Kind              string
	RecordedAtUTC     string
	RepoRoot          string
	Cwd               string
	Provider          string
	Event             string
	Tool              string
	Status            string
	SourcePath        string
	Raw               []byte
	Findings          []evidence.Finding
	AgentRemediation  []agentmsg.Remediation
	RemediationEvents []evidence.RemediationEvent
	HookEvent         *HookEventAnalytics
	HookDecisions     []HookDecisionAnalytics
	HookTargets       []HookTargetAnalytics
	DeleteIntents     []CodeDeleteIntent
}

func (store *Store) IngestTrace(ctx context.Context, trace Trace) error {
	if strings.TrimSpace(trace.ID) == "" {
		return apperror.StaticError("trace id is required")
	}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin trace ingest: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	exists, err := traceExists(ctx, transaction, trace.ID)
	if err != nil {
		return err
	}

	if exists {
		err = deleteTraceRows(ctx, transaction, trace.ID)
		if err != nil {
			return err
		}
	}

	err = insertTraceRows(ctx, transaction, trace)
	if err != nil {
		return err
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit trace ingest: %w", err)
	}

	return nil
}

func insertTraceRows(
	ctx context.Context,
	transaction *sql.Tx,
	trace Trace,
) error {
	for _, insert := range []func(context.Context, *sql.Tx, Trace) error{
		insertTrace,
		insertFindings,
		insertRemediations,
		insertRemediationEvents,
		insertAutomaticRemediationOutcomes,
		insertHookAnalytics,
		insertTraceDeleteIntents,
	} {
		err := insert(ctx, transaction, trace)
		if err != nil {
			return err
		}
	}

	return nil
}

func insertAutomaticRemediationOutcomes(
	ctx context.Context,
	transaction *sql.Tx,
	trace Trace,
) error {
	for _, event := range trace.RemediationEvents {
		outcome := strings.ToLower(strings.TrimSpace(event.Event))
		if outcome != automaticRemediationOutcomeFixed &&
			outcome != automaticRemediationOutcomeAbandoned {
			continue
		}

		if strings.TrimSpace(event.SourceTraceID) == "" {
			continue
		}

		remediationOutcome := normalizeRemediationOutcome(RemediationOutcome{
			ID: stableID(
				automaticRemediationOutcomeIDPrefix,
				event.ID,
				event.RemediationID,
				event.FindingID,
				event.SourceTraceID,
				trace.ID,
				outcome,
			),
			RemediationID:   event.RemediationID,
			FindingID:       event.FindingID,
			SourceTraceID:   event.SourceTraceID,
			FollowupTraceID: trace.ID,
			PolicyID:        event.PolicyID,
			SkillID:         event.SkillID,
			File:            event.File,
			Path:            event.Path,
			Provider:        firstNonEmpty(event.Provider, trace.Provider),
			Tool:            firstNonEmpty(event.Tool, trace.Tool),
			Outcome:         outcome,
			RecordedAtUTC:   trace.RecordedAtUTC,
		})

		err := insertRemediationOutcome(ctx, transaction, remediationOutcome)
		if err != nil {
			return fmt.Errorf(
				"insert automatic remediation outcome %q: %w",
				remediationOutcome.ID,
				err,
			)
		}
	}

	return nil
}

func insertTraceDeleteIntents(
	ctx context.Context,
	transaction *sql.Tx,
	trace Trace,
) error {
	return insertDeleteIntents(ctx, transaction, trace.DeleteIntents)
}

func rollbackUnlessCommitted(transaction *sql.Tx) {
	err := transaction.Rollback()
	if err != nil {
		return
	}
}
