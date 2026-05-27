// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"unicode"
)

func searchTerms(text string) []string {
	seen := map[string]bool{}
	terms := []string{}

	for _, field := range strings.FieldsFunc(text, searchTermSeparator) {
		term := strings.ToLower(strings.TrimSpace(field))
		if term == "" || seen[term] {
			continue
		}

		seen[term] = true
		terms = append(terms, term)
	}

	return terms
}

func searchTermSeparator(value rune) bool {
	return !unicode.IsLetter(value) && !unicode.IsDigit(value)
}

func backfillSearchTerms(ctx context.Context, database *sql.DB) error {
	rows, err := database.QueryContext(
		ctx,
		`SELECT fts_id, search_text
		FROM code_intel_fts
		WHERE COALESCE(fts_id, '') != ''
			AND fts_id NOT IN (SELECT DISTINCT fts_id FROM code_intel_search_terms)`,
	)
	if err != nil {
		return fmt.Errorf("query missing code intelligence search terms: %w", err)
	}
	defer rows.Close()

	pending := map[string]string{}

	for rows.Next() {
		var rowID, text string

		err = rows.Scan(&rowID, &text)
		if err != nil {
			return fmt.Errorf("scan missing code intelligence search terms: %w", err)
		}

		pending[rowID] = text
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate missing code intelligence search terms: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	transaction, err := database.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin code intelligence search-term backfill: %w", err)
	}

	defer rollbackSearchTransaction(transaction)

	for rowID, text := range pending {
		err = insertSearchTerms(ctx, transaction, rowID, text)
		if err != nil {
			return err
		}
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit code intelligence search-term backfill: %w", err)
	}

	return nil
}
