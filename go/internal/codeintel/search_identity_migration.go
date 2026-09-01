// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	errFTSIdentityConflict = errors.New(
		"conflicting code intelligence FTS rows share identity",
	)
	errFTSIdentityMissing = errors.New(
		"code intelligence FTS row has no durable identity",
	)
)

type ftsIdentityContent struct {
	kind       sql.NullString
	recordID   sql.NullString
	traceID    sql.NullString
	policyID   sql.NullString
	skillID    sql.NullString
	path       sql.NullString
	message    sql.NullString
	searchText sql.NullString
}

// deduplicateSearchIdentity upgrades the v1 logical keys before unique
// indexes are created. Equal crash/reindex replays collapse to one row;
// conflicting rows for the same identity fail closed because choosing either
// would silently change search evidence.
func deduplicateSearchIdentity(ctx context.Context, database *sql.DB) error {
	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin search identity migration: %w", err)
	}
	defer rollbackUnlessCommitted(transaction)

	ftsDuplicates, err := inspectFTSIdentityDuplicates(ctx, transaction)
	if err != nil {
		return err
	}

	termDuplicates, err := inspectSearchTermDuplicates(ctx, transaction)
	if err != nil {
		return err
	}

	if ftsDuplicates > 0 {
		for _, statement := range []string{
			`CREATE OR REPLACE TEMP TABLE code_intel_fts_deduplicated AS
				SELECT DISTINCT * FROM code_intel_fts`,
			"DELETE FROM code_intel_fts",
			"INSERT INTO code_intel_fts SELECT * FROM code_intel_fts_deduplicated",
			"DROP TABLE code_intel_fts_deduplicated",
		} {
			_, execErr := transaction.ExecContext(ctx, statement)
			if execErr != nil {
				return fmt.Errorf("deduplicate code intelligence FTS rows: %w", execErr)
			}
		}
	}

	if termDuplicates > 0 {
		for _, statement := range []string{
			`CREATE OR REPLACE TEMP TABLE code_intel_terms_deduplicated AS
				SELECT DISTINCT term, fts_id FROM code_intel_search_terms`,
			"DELETE FROM code_intel_search_terms",
			`INSERT INTO code_intel_search_terms(term, fts_id)
				SELECT term, fts_id FROM code_intel_terms_deduplicated`,
			"DROP TABLE code_intel_terms_deduplicated",
		} {
			_, execErr := transaction.ExecContext(ctx, statement)
			if execErr != nil {
				return fmt.Errorf("deduplicate code intelligence search terms: %w", execErr)
			}
		}
	}

	commitErr := transaction.Commit()
	if commitErr != nil {
		return fmt.Errorf("commit search identity migration: %w", commitErr)
	}

	return nil
}

func inspectFTSIdentityDuplicates(
	ctx context.Context,
	transaction *sql.Tx,
) (int, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT fts_id, kind, record_id, trace_id, policy_id, skill_id,
			path, message, search_text
		FROM code_intel_fts`,
	)
	if err != nil {
		return 0, fmt.Errorf("inspect code intelligence FTS identities: %w", err)
	}
	defer rows.Close()

	seen := map[string]ftsIdentityContent{}
	duplicates := 0

	for rows.Next() {
		var (
			identity sql.NullString
			content  ftsIdentityContent
		)

		scanErr := rows.Scan(
			&identity,
			&content.kind,
			&content.recordID,
			&content.traceID,
			&content.policyID,
			&content.skillID,
			&content.path,
			&content.message,
			&content.searchText,
		)
		if scanErr != nil {
			return 0, fmt.Errorf("scan code intelligence FTS identity: %w", scanErr)
		}

		if !identity.Valid || identity.String == "" {
			return 0, errFTSIdentityMissing
		}

		if previous, ok := seen[identity.String]; ok {
			if previous != content {
				return 0, fmt.Errorf("%w: %q", errFTSIdentityConflict, identity.String)
			}

			duplicates++

			continue
		}

		seen[identity.String] = content
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return 0, fmt.Errorf("iterate code intelligence FTS identities: %w", rowsErr)
	}

	return duplicates, nil
}

func inspectSearchTermDuplicates(
	ctx context.Context,
	transaction *sql.Tx,
) (int, error) {
	rows, err := transaction.QueryContext(
		ctx,
		"SELECT term, fts_id FROM code_intel_search_terms",
	)
	if err != nil {
		return 0, fmt.Errorf("inspect code intelligence search term identities: %w", err)
	}
	defer rows.Close()

	seen := map[[2]string]struct{}{}
	duplicates := 0

	for rows.Next() {
		var term, identity string

		scanErr := rows.Scan(&term, &identity)
		if scanErr != nil {
			return 0, fmt.Errorf("scan code intelligence search term identity: %w", scanErr)
		}

		key := [2]string{term, identity}

		if _, ok := seen[key]; ok {
			duplicates++

			continue
		}

		seen[key] = struct{}{}
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return 0, fmt.Errorf("iterate code intelligence search term identities: %w", rowsErr)
	}

	return duplicates, nil
}
