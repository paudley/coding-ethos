// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	ProxySessions       int `json:"proxy_sessions"`
	ProxyEvents         int `json:"proxy_events"`
	ProxyTransforms     int `json:"proxy_transforms"`
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

type RowPruneSummary struct {
	CutoffUTC          string `json:"cutoff_utc"`
	DeletedTraces      int    `json:"deleted_traces"`
	DeletedProxyEvents int    `json:"deleted_proxy_events"`
}

// SourcePathIngestRequest asks whether a source path has trace coverage.
type SourcePathIngestRequest struct {
	Path            string
	IncludeChildren bool
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

func (store *Store) Database() *sql.DB {
	return store.database
}

func (store *Store) Close() error {
	err := store.database.Close()
	if err != nil {
		return fmt.Errorf("close code-intel store: %w", err)
	}

	return nil
}

func (store *Store) Vacuum(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, "VACUUM")
	if err != nil {
		return fmt.Errorf("vacuum code-intel store: %w", err)
	}

	return nil
}

func (store *Store) PruneRows(
	ctx context.Context,
	olderThan time.Duration,
	now time.Time,
) (RowPruneSummary, error) {
	return store.pruneRows(ctx, olderThan, now, true)
}

func (store *Store) PreviewPruneRows(
	ctx context.Context,
	olderThan time.Duration,
	now time.Time,
) (RowPruneSummary, error) {
	return store.pruneRows(ctx, olderThan, now, false)
}

func (store *Store) SourcePathIngested(
	ctx context.Context,
	path string,
	includeChildren bool,
) (bool, error) {
	results, err := store.SourcePathsIngested(ctx, []SourcePathIngestRequest{
		{
			Path:            path,
			IncludeChildren: includeChildren,
		},
	})
	if err != nil {
		return false, err
	}

	return results[cleanSourcePath(path)], nil
}

// SourcePathsIngested checks trace coverage for source paths in one DB scan.
func (store *Store) SourcePathsIngested(
	ctx context.Context,
	requests []SourcePathIngestRequest,
) (map[string]bool, error) {
	results := make(map[string]bool, len(requests))
	if len(requests) == 0 {
		return results, nil
	}

	normalized := make([]SourcePathIngestRequest, 0, len(requests))
	for _, request := range requests {
		cleanPath := cleanSourcePath(request.Path)
		results[cleanPath] = false
		normalized = append(normalized, SourcePathIngestRequest{
			Path:            cleanPath,
			IncludeChildren: request.IncludeChildren,
		})
	}

	rows, err := store.database.QueryContext(
		ctx,
		`SELECT source_path FROM traces WHERE source_path != ''`,
	)
	if err != nil {
		return nil, fmt.Errorf("check ingested source paths: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var sourcePath string

		scanErr := rows.Scan(&sourcePath)
		if scanErr != nil {
			return nil, fmt.Errorf("scan ingested source path: %w", scanErr)
		}

		markIngestedSourcePath(results, normalized, cleanSourcePath(sourcePath))
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate ingested source paths: %w", rowsErr)
	}

	return results, nil
}

func cleanSourcePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func markIngestedSourcePath(
	results map[string]bool,
	requests []SourcePathIngestRequest,
	sourcePath string,
) {
	for _, request := range requests {
		if sourcePath == request.Path {
			results[request.Path] = true

			continue
		}

		if request.IncludeChildren && strings.HasPrefix(sourcePath, request.Path+"/") {
			results[request.Path] = true
		}
	}
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

func (store *Store) pruneRows(
	ctx context.Context,
	olderThan time.Duration,
	now time.Time,
	apply bool,
) (RowPruneSummary, error) {
	if olderThan <= 0 {
		return RowPruneSummary{}, nil
	}

	if now.IsZero() {
		now = time.Now().UTC()
	}

	cutoff := now.UTC().Add(-olderThan).Format(time.RFC3339Nano)
	summary := RowPruneSummary{CutoffUTC: cutoff}

	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return RowPruneSummary{}, fmt.Errorf(
			"begin code-intel row prune transaction: %w",
			err,
		)
	}
	defer rollbackUnlessCommitted(transaction)

	traceIDs, err := traceIDsOlderThan(ctx, transaction, cutoff)
	if err != nil {
		return RowPruneSummary{}, err
	}

	summary.DeletedTraces = len(traceIDs)

	summary.DeletedProxyEvents, err = countProxyEventsOlderThan(
		ctx,
		store.database,
		cutoff,
	)
	if err != nil {
		return RowPruneSummary{}, err
	}

	if !apply {
		return summary, nil
	}

	for _, traceID := range traceIDs {
		deleteErr := deleteTraceRows(ctx, transaction, traceID)
		if deleteErr != nil {
			return RowPruneSummary{}, deleteErr
		}
	}

	result, err := transaction.ExecContext(
		ctx,
		`DELETE FROM proxy_events
		WHERE COALESCE(recorded_at_utc, '') <> ''
			AND recorded_at_utc < ?`,
		cutoff,
	)
	if err != nil {
		return RowPruneSummary{}, fmt.Errorf("delete old proxy events: %w", err)
	}

	deletedProxyEvents, err := result.RowsAffected()
	if err != nil {
		return RowPruneSummary{}, fmt.Errorf("count deleted proxy events: %w", err)
	}

	summary.DeletedProxyEvents = int(deletedProxyEvents)

	err = transaction.Commit()
	if err != nil {
		return RowPruneSummary{}, fmt.Errorf(
			"commit code-intel row prune transaction: %w",
			err,
		)
	}

	return summary, nil
}

func countProxyEventsOlderThan(
	ctx context.Context,
	queryer interface {
		QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	},
	cutoff string,
) (int, error) {
	var count int

	err := queryer.QueryRowContext(
		ctx,
		`SELECT COUNT(*)
		FROM proxy_events
		WHERE COALESCE(recorded_at_utc, '') <> ''
			AND recorded_at_utc < ?`,
		cutoff,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count old proxy events: %w", err)
	}

	return count, nil
}

func traceIDsOlderThan(
	ctx context.Context,
	transaction *sql.Tx,
	cutoff string,
) ([]string, error) {
	rows, err := transaction.QueryContext(
		ctx,
		`SELECT trace_id
		FROM traces
		WHERE COALESCE(recorded_at_utc, '') <> ''
			AND recorded_at_utc < ?
		ORDER BY recorded_at_utc, trace_id`,
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("select old trace rows: %w", err)
	}
	defer rows.Close()

	traceIDs := []string{}

	for rows.Next() {
		var traceID string

		scanErr := rows.Scan(&traceID)
		if scanErr != nil {
			return nil, fmt.Errorf("scan old trace row: %w", scanErr)
		}

		traceIDs = append(traceIDs, traceID)
	}

	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("iterate old trace rows: %w", err)
	}

	return traceIDs, nil
}

func configureStore(ctx context.Context, database *sql.DB) error {
	for _, statement := range []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = NORMAL",
		"PRAGMA busy_timeout = 30000",
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

	for _, statement := range indexSchemaStatements() {
		_, err := database.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("migrate code intelligence indexes: %w", err)
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
			name:   "proxy_sessions",
			query:  "SELECT COUNT(*) FROM proxy_sessions",
			target: &stats.ProxySessions,
		},
		{
			name:   "proxy_events",
			query:  "SELECT COUNT(*) FROM proxy_events",
			target: &stats.ProxyEvents,
		},
		{
			name:   "proxy_transforms",
			query:  "SELECT COUNT(*) FROM proxy_transforms",
			target: &stats.ProxyTransforms,
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
