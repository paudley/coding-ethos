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
	db *sql.DB
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
	if err := os.MkdirAll(filepath.Dir(path), storeDirMode); err != nil {
		return nil, fmt.Errorf("create code intelligence store dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open code intelligence store: %w", err)
	}
	store := &Store{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}

	return store, nil
}

func (store *Store) Close() error {
	return store.db.Close()
}

func (store *Store) configure(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure code intelligence store: %w", err)
		}
	}

	return nil
}

func (store *Store) migrate(ctx context.Context) error {
	for _, statement := range schemaStatements {
		if _, err := store.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate code intelligence store: %w", err)
		}
	}
	for table, columns := range migrationColumns {
		for _, column := range columns {
			if err := store.ensureColumn(ctx, table, column); err != nil {
				return err
			}
		}
	}
	_, err := store.db.ExecContext(
		ctx,
		"INSERT OR REPLACE INTO schema_metadata(key, value) VALUES('schema_version', ?)",
		schemaVersion,
	)
	if err != nil {
		return fmt.Errorf("record code intelligence schema version: %w", err)
	}

	return nil
}

func (store *Store) ensureColumn(ctx context.Context, table string, column migrationColumn) error {
	exists, err := store.columnExists(ctx, table, column.Name)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := store.db.ExecContext(
		ctx,
		fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, column.Name, column.Type),
	); err != nil {
		return fmt.Errorf("add %s.%s column: %w", table, column.Name, err)
	}

	return nil
}

func (store *Store) columnExists(ctx context.Context, table string, name string) (bool, error) {
	rows, err := store.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
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
		if err := rows.Scan(&cid, &columnName, &columnType, &notNull, &defaultVal, &pk); err != nil {
			return false, fmt.Errorf("scan %s column info: %w", table, err)
		}
		if columnName == name {
			return true, nil
		}
	}
	if err := rows.Err(); err != nil {
		return false, fmt.Errorf("iterate %s column info: %w", table, err)
	}

	return false, nil
}

func (store *Store) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{SchemaVersion: schemaVersion}
	counts := map[string]*int{
		"traces":               &stats.Traces,
		"hook_events":          &stats.HookEvents,
		"hook_decisions":       &stats.HookDecisions,
		"hook_targets":         &stats.HookTargets,
		"hook_reviews":         &stats.HookReviews,
		"findings":             &stats.Findings,
		"code_files":           &stats.Files,
		"code_chunks":          &stats.CodeChunks,
		"code_edges":           &stats.CodeEdges,
		"ast_finding_links":    &stats.ASTFindingLinks,
		"remediations":         &stats.Remediations,
		"remediation_events":   &stats.RemediationEvents,
		"sarif_runs":           &stats.SARIFRuns,
		"sarif_results":        &stats.SARIFResults,
		"remediation_outcomes": &stats.RemediationOutcomes,
		"embedding_records":    &stats.EmbeddingRecords,
		"code_intel_fts":       &stats.FtsRows,
	}
	for table, target := range counts {
		row := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
		if err := row.Scan(target); err != nil {
			return Stats{}, fmt.Errorf("count %s: %w", table, err)
		}
	}

	return stats, nil
}
