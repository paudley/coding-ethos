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
		inlineErr0 := deleteTraceRows(ctx, transaction, trace.ID)
		if inlineErr0 != nil {
			return inlineErr0
		}
	}

	inlineErr1 := insertTrace(ctx, transaction, trace)
	if inlineErr1 != nil {
		return inlineErr1
	}

	inlineErr2 := insertFindings(ctx, transaction, trace)
	if inlineErr2 != nil {
		return inlineErr2
	}

	inlineErr3 := insertRemediations(ctx, transaction, trace)
	if inlineErr3 != nil {
		return inlineErr3
	}

	inlineErr4 := insertRemediationEvents(ctx, transaction, trace)
	if inlineErr4 != nil {
		return inlineErr4
	}

	inlineErr5 := insertHookAnalytics(ctx, transaction, trace)
	if inlineErr5 != nil {
		return inlineErr5
	}

	inlineErr6 := insertDeleteIntents(ctx, transaction, trace.DeleteIntents)
	if inlineErr6 != nil {
		return inlineErr6
	}

	inlineErr7 := transaction.Commit()
	if inlineErr7 != nil {
		return fmt.Errorf("commit trace ingest: %w", inlineErr7)
	}

	return nil
}

func rollbackUnlessCommitted(transaction *sql.Tx) {
	err := transaction.Rollback()
	if err != nil {
		return
	}
}
