// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"blackcat.ca/coding-ethos/go/internal/agentmsg"
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
}

func (store *Store) IngestTrace(ctx context.Context, trace Trace) error {
	if strings.TrimSpace(trace.ID) == "" {
		return fmt.Errorf("trace id is required")
	}

	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin trace ingest: %w", err)
	}
	defer rollbackUnlessCommitted(tx)

	if err := deleteTraceRows(ctx, tx, trace.ID); err != nil {
		return err
	}
	if err := insertTrace(ctx, tx, trace); err != nil {
		return err
	}
	if err := insertFindings(ctx, tx, trace); err != nil {
		return err
	}
	if err := insertRemediations(ctx, tx, trace); err != nil {
		return err
	}
	if err := insertRemediationEvents(ctx, tx, trace); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit trace ingest: %w", err)
	}

	return nil
}

func rollbackUnlessCommitted(tx *sql.Tx) {
	_ = tx.Rollback()
}
