// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

//nolint:lll,noinlineerr,wsl_v5 // SQL evidence projection stays adjacent to its scan contract.
package tokeneconomy

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

func readRunMechanisms(
	ctx context.Context,
	codeIntelDBPath string,
	sessionID string,
) (MechanismMetrics, error) {
	if _, err := os.Stat(codeIntelDBPath); errors.Is(err, os.ErrNotExist) {
		return MechanismMetrics{}, nil
	} else if err != nil {
		return MechanismMetrics{}, fmt.Errorf("inspect run mechanism store: %w", err)
	}

	database, err := sql.Open("duckdb", codeIntelDBPath+"?access_mode=READ_ONLY")
	if err != nil {
		return MechanismMetrics{}, fmt.Errorf("open run mechanism store: %w", err)
	}
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	metrics, queryErr := queryRunMechanisms(ctx, database, sessionID)
	closeErr := database.Close()
	if err = errors.Join(queryErr, closeErr); err != nil {
		return MechanismMetrics{}, err
	}

	return metrics, nil
}

func queryRunMechanisms(
	ctx context.Context,
	database *sql.DB,
	sessionID string,
) (MechanismMetrics, error) {
	metrics := MechanismMetrics{}
	err := database.QueryRowContext(
		ctx,
		`WITH first_transform AS (
			SELECT transform.event_id, transform.input_tokens,
				ROW_NUMBER() OVER (
					PARTITION BY transform.event_id ORDER BY transform.ordinal
				) AS rank
			FROM proxy_transforms transform
			JOIN proxy_events event USING(event_id)
			WHERE event.session_id = ?
		), repeated_injections AS (
			SELECT COALESCE(SUM(repetitions), 0) AS repeated
			FROM (
				SELECT GREATEST(COUNT(*) - 1, 0) AS repetitions
				FROM proxy_events
				WHERE session_id = ? AND event_kind = 'payload_injection'
					AND COALESCE(output_hash, '') != ''
				GROUP BY output_hash
			)
		)
		SELECT
			COALESCE(SUM(first_transform.input_tokens), 0),
			COALESCE(SUM(event.output_tokens), 0),
			COALESCE(SUM(CASE WHEN event.event_kind = 'payload_injection'
				THEN event.output_tokens ELSE 0 END), 0),
			COUNT(*),
			(SELECT repeated FROM repeated_injections)
		FROM first_transform
		JOIN proxy_events event USING(event_id)
		WHERE first_transform.rank = 1`,
		sessionID,
		sessionID,
	).Scan(
		&metrics.RawContextTokens,
		&metrics.DeliveredContextTokens,
		&metrics.InjectedGuidanceTokens,
		&metrics.TransformEventCount,
		&metrics.RepeatedAdviceCount,
	)
	if err != nil {
		return MechanismMetrics{}, fmt.Errorf("query run mechanism evidence: %w", err)
	}

	metrics.AvoidedContextTokens = metrics.RawContextTokens - metrics.DeliveredContextTokens

	return metrics, nil
}
