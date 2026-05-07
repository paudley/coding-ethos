// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: MIT

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

const (
	schemaVersion = 1
	storeDirMode  = 0o700
)

type Store struct {
	database *sql.DB
}

type Stats struct {
	Traces              int `json:"traces"`
	HookEvents          int `json:"hook_events"`
	HookDecisions       int `json:"hook_decisions"`
	HookTargets         int `json:"hook_targets"`
	HookReviews         int `json:"hook_reviews"`
	Findings            int `json:"findings"`
	Files               int `json:"files"`
	CodeChunks          int `json:"code_chunks"`
	CodeEdges           int `json:"code_edges"`
	ASTFindingLinks     int `json:"ast_finding_links"`
	Remediations        int `json:"remediations"`
	RemediationEvents   int `json:"remediation_events"`
	SARIFRuns           int `json:"sarif_runs"`
	SARIFResults        int `json:"sarif_results"`
	RemediationOutcomes int `json:"remediation_outcomes"`
	EmbeddingRecords    int `json:"embedding_records"`
	FtsRows             int `json:"fts_rows"`
	SchemaVersion       int `json:"schema_version"`
}

func DefaultDBPath(root string) string {
	return filepath.Join(root, ".coding-ethos", "code-intel.db")
}

func Open(ctx context.Context, path string) (*Store, error) {
	inlineErr0 := os.MkdirAll(filepath.Dir(path), storeDirMode)
	if inlineErr0 != nil {
		return nil, fmt.Errorf("create code intelligence store dir: %w", inlineErr0)
	}

	database, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}

	store := &Store{database: database}

	inlineErr1 := configureStore(ctx, database)
	if inlineErr1 != nil {
		_ = database.Close()

		return nil, inlineErr1
	}

	inlineErr2 := migrateStore(ctx, database)
	if inlineErr2 != nil {
		_ = database.Close()

		return nil, inlineErr2
	}

	return store, nil
}

func (store *Store) Close() error {
	err := store.database.Close()
	if err != nil {
		return fmt.Errorf("close code-intel store: %w", err)
	}

	return nil
}

func configureStore(ctx context.Context, database *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	} {
		_, inlineErrA := database.ExecContext(ctx, statement)
		if inlineErrA != nil {
			return fmt.Errorf("configure code intelligence store: %w", inlineErrA)
		}
	}

	return nil
}

func migrateStore(ctx context.Context, database *sql.DB) error {
	for _, statement := range schemaStatements() {
		_, inlineErrB := database.ExecContext(ctx, statement)
		if inlineErrB != nil {
			return fmt.Errorf("migrate code intelligence store: %w", inlineErrB)
		}
	}

	for table, columns := range migrationColumns() {
		for _, column := range columns {
			err := ensureColumn(ctx, database, table, column)
			if err != nil {
				return err
			}
		}
	}

	_, err := database.ExecContext(
		ctx,
		"INSERT OR REPLACE INTO schema_metadata(key, value) VALUES('schema_version', ?)",
		schemaVersion,
	)
	if err != nil {
		return fmt.Errorf("record code intelligence schema version: %w", err)
	}

	return nil
}

func ensureColumn(
	ctx context.Context,
	database *sql.DB,
	table string,
	column migrationColumn,
) error {
	exists, err := columnExists(ctx, database, table, column.Name)
	if err != nil {
		return err
	}

	if exists {
		return nil
	}

	_, inlineErrC := database.ExecContext(
		ctx,
		fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column.Name, column.Type),
	)
	if inlineErrC != nil {
		return fmt.Errorf("add %s.%s column: %w", table, column.Name, inlineErrC)
	}

	return nil
}

func columnExists(
	ctx context.Context,
	database *sql.DB,
	table, name string,
) (bool, error) {
	rows, err := database.QueryContext(
		ctx,
		fmt.Sprintf("PRAGMA table_info(%s)", table),
	)
	if err != nil {
		return false, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			columnName string
			columnType string
			notNull    int
			defaultVal any
			pk         int
		)

		err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &pk)
		if err != nil {
			return false, fmt.Errorf("scan %s column info: %w", table, err)
		}

		if columnName == name {
			return true, nil
		}
	}

	inlineErr3 := rows.Err()
	if inlineErr3 != nil {
		return false, fmt.Errorf("iterate %s column info: %w", table, inlineErr3)
	}

	return false, nil
}

func (store *Store) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{SchemaVersion: schemaVersion}

	for _, count := range statCountQueries(&stats) {
		row := store.database.QueryRowContext(ctx, count.query)

		err := row.Scan(count.target)
		if err != nil {
			return Stats{}, fmt.Errorf("count %s: %w", count.name, err)
		}
	}

	return stats, nil
}

type statCountQuery struct {
	target *int
	name   string
	query  string
}

func statCountQueries(stats *Stats) []statCountQuery {
	return append(
		coreStatCountQueries(stats),
		derivedStatCountQueries(stats)...,
	)
}

func coreStatCountQueries(stats *Stats) []statCountQuery {
	return []statCountQuery{
		{name: "traces", query: "SELECT COUNT(*) FROM traces", target: &stats.Traces},
		{
			name:   "hook_events",
			query:  "SELECT COUNT(*) FROM hook_events",
			target: &stats.HookEvents,
		},
		{
			name:   "hook_decisions",
			query:  "SELECT COUNT(*) FROM hook_decisions",
			target: &stats.HookDecisions,
		},
		{
			name:   "hook_targets",
			query:  "SELECT COUNT(*) FROM hook_targets",
			target: &stats.HookTargets,
		},
		{
			name:   "hook_reviews",
			query:  "SELECT COUNT(*) FROM hook_reviews",
			target: &stats.HookReviews,
		},
		{
			name:   "findings",
			query:  "SELECT COUNT(*) FROM findings",
			target: &stats.Findings,
		},
		{
			name:   "code_files",
			query:  "SELECT COUNT(*) FROM code_files",
			target: &stats.Files,
		},
		{
			name:   "code_chunks",
			query:  "SELECT COUNT(*) FROM code_chunks",
			target: &stats.CodeChunks,
		},
	}
}

func derivedStatCountQueries(stats *Stats) []statCountQuery {
	return []statCountQuery{
		{
			name:   "code_edges",
			query:  "SELECT COUNT(*) FROM code_edges",
			target: &stats.CodeEdges,
		},
		{
			name:   "ast_finding_links",
			query:  "SELECT COUNT(*) FROM ast_finding_links",
			target: &stats.ASTFindingLinks,
		},
		{
			name:   "remediations",
			query:  "SELECT COUNT(*) FROM remediations",
			target: &stats.Remediations,
		},
		{
			name:   "remediation_events",
			query:  "SELECT COUNT(*) FROM remediation_events",
			target: &stats.RemediationEvents,
		},
		{
			name:   "sarif_runs",
			query:  "SELECT COUNT(*) FROM sarif_runs",
			target: &stats.SARIFRuns,
		},
		{
			name:   "sarif_results",
			query:  "SELECT COUNT(*) FROM sarif_results",
			target: &stats.SARIFResults,
		},
		{
			name:   "remediation_outcomes",
			query:  "SELECT COUNT(*) FROM remediation_outcomes",
			target: &stats.RemediationOutcomes,
		},
		{
			name:   "embedding_records",
			query:  "SELECT COUNT(*) FROM embedding_records",
			target: &stats.EmbeddingRecords,
		},
		{
			name:   "code_intel_fts",
			query:  "SELECT COUNT(*) FROM code_intel_fts",
			target: &stats.FtsRows,
		},
	}
}
