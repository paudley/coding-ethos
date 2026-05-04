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
	Traces            int `json:"traces"`
	Findings          int `json:"findings"`
	Remediations      int `json:"remediations"`
	RemediationEvents int `json:"remediation_events"`
	FtsRows           int `json:"fts_rows"`
	SchemaVersion     int `json:"schema_version"`
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

func (store *Store) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{SchemaVersion: schemaVersion}
	counts := map[string]*int{
		"traces":             &stats.Traces,
		"findings":           &stats.Findings,
		"remediations":       &stats.Remediations,
		"remediation_events": &stats.RemediationEvents,
		"code_intel_fts":     &stats.FtsRows,
	}
	for table, target := range counts {
		row := store.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table)
		if err := row.Scan(target); err != nil {
			return Stats{}, fmt.Errorf("count %s: %w", table, err)
		}
	}

	return stats, nil
}
