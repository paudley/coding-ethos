// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	duckDBStoreMode                  = 0o700
	duckDBLockFileMode               = 0o600
	duckDBStaleLockAge               = 30 * time.Minute
	maxLegacySQLiteImportBytes int64 = 1 << 30
)

// DuckDBStore is the code-intel analytical query store.
type DuckDBStore struct {
	database *sql.DB
	path     string
}

// RebuildIndexSummary reports a DuckDB rebuild/import run.
type RebuildIndexSummary struct {
	Backend                string `json:"backend"`
	Path                   string `json:"path"`
	LegacySQLitePath       string `json:"legacy_sqlite_path,omitempty"`
	LegacySQLiteSkipReason string `json:"legacy_sqlite_skip_reason,omitempty"`
	EventCount             int    `json:"event_count"`
	ImportedEventCount     int    `json:"imported_event_count"`
	ImportedLegacySQLite   bool   `json:"imported_legacy_sqlite"`
	SkippedLegacySQLite    bool   `json:"skipped_legacy_sqlite"`
	RemovedLegacySQLite    bool   `json:"removed_legacy_sqlite"`
	Stats                  Stats  `json:"stats"`
}

// RebuildLockMaintenance reports inspection and optional cleanup of the
// repo-local code-intel rebuild lock.
type RebuildLockMaintenance struct {
	Path       string `json:"path"`
	Reason     string `json:"reason,omitempty"`
	PID        int    `json:"pid,omitempty"`
	AgeSeconds int64  `json:"age_seconds,omitempty"`
	Exists     bool   `json:"exists"`
	Stale      bool   `json:"stale"`
	Removed    bool   `json:"removed"`
}

type legacySQLiteImportResult struct {
	skipReason string
	imported   bool
	skipped    bool
}

type StorageUpgradeSummary struct {
	LegacyPath     string              `json:"legacy_path,omitempty"`
	DuckDBPath     string              `json:"duckdb_path,omitempty"`
	RebuildSummary RebuildIndexSummary `json:"rebuild_summary,omitzero"`
	Needed         bool                `json:"needed"`
	Completed      bool                `json:"completed"`
}

func DefaultDuckDBPath(root string) string {
	return filepath.Join(root, downstreamStateDir, "code-intel.duckdb")
}

func OpenDuckDB(ctx context.Context, path string) (*DuckDBStore, error) {
	err := os.MkdirAll(filepath.Dir(path), duckDBStoreMode)
	if err != nil {
		return nil, fmt.Errorf("create DuckDB code-intel store dir: %w", err)
	}

	database, err := sql.Open("duckdb", path)
	if err != nil {
		return nil, fmt.Errorf("open DuckDB code-intel store: %w", err)
	}

	store := &DuckDBStore{database: database, path: path}

	err = store.migrate(ctx)
	if err != nil {
		_ = database.Close()

		return nil, err
	}

	return store, nil
}

func OpenDuckDBReadOnly(ctx context.Context, path string) (*DuckDBStore, error) {
	_, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat DuckDB code-intel store: %w", err)
	}

	database, err := sql.Open("duckdb", path+"?access_mode=READ_ONLY")
	if err != nil {
		return nil, fmt.Errorf("open read-only DuckDB code-intel store: %w", err)
	}

	store := &DuckDBStore{database: database, path: path}

	err = store.ping(ctx)
	if err != nil {
		_ = database.Close()

		return nil, err
	}

	return store, nil
}

func (store *DuckDBStore) Close() error {
	err := store.database.Close()
	if err != nil {
		return fmt.Errorf("close DuckDB code-intel store: %w", err)
	}

	return nil
}

func (store *DuckDBStore) Database() *sql.DB {
	return store.database
}

func (store *DuckDBStore) Stats(ctx context.Context) (Stats, error) {
	stats := Stats{SchemaVersion: schemaVersion}
	for _, count := range duckDBStatsQueries(&stats) {
		row := store.database.QueryRowContext(ctx, count.query)

		err := row.Scan(count.target)
		if err != nil {
			return Stats{}, fmt.Errorf("count DuckDB %s: %w", count.name, err)
		}
	}

	return stats, nil
}

func (store *DuckDBStore) Checkpoint(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, "CHECKPOINT")
	if err != nil {
		return fmt.Errorf("checkpoint DuckDB code-intel store: %w", err)
	}

	return nil
}

func (store *DuckDBStore) Compact(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, "VACUUM")
	if err != nil {
		return fmt.Errorf("compact DuckDB code-intel store: %w", err)
	}

	return nil
}

func (store *DuckDBStore) RebuildFromLegacySQLite(
	ctx context.Context,
	legacy *Store,
) error {
	err := clearDuckDBTables(ctx, store.database)
	if err != nil {
		return err
	}

	for _, table := range legacyImportTables() {
		err := copyTableRows(ctx, legacy.database, store.database, table)
		if err != nil {
			return err
		}
	}

	return nil
}

func RebuildDuckDBIndex(
	ctx context.Context,
	root string,
	duckDBPath string,
	legacySQLitePath string,
) (RebuildIndexSummary, error) {
	if strings.TrimSpace(duckDBPath) == "" {
		duckDBPath = DefaultDuckDBPath(root)
	}

	if strings.TrimSpace(legacySQLitePath) == "" {
		legacySQLitePath = DefaultDBPath(root)
	}

	release, err := acquireDuckDBRebuildLock(root)
	if err != nil {
		return RebuildIndexSummary{}, err
	}
	defer release()

	store, err := OpenDuckDB(ctx, duckDBPath)
	if err != nil {
		return RebuildIndexSummary{}, err
	}
	defer store.Close()

	eventCount, err := EventLogStats(root)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	importedEventCount, err := store.ImportEventLog(
		ctx,
		NewEventLog(DefaultEventLogDir(root)),
	)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	legacyImport, err := importLegacySQLiteIntoDuckDB(ctx, store, legacySQLitePath)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	removedLegacy := false
	if legacyImport.imported {
		removedLegacy, err = RemoveLegacySQLiteStore(legacySQLitePath)
		if err != nil {
			return RebuildIndexSummary{}, err
		}
	}

	return RebuildIndexSummary{
		Backend:                "duckdb",
		Path:                   duckDBPath,
		LegacySQLitePath:       legacySQLitePath,
		LegacySQLiteSkipReason: legacyImport.skipReason,
		EventCount:             eventCount,
		ImportedEventCount:     importedEventCount,
		ImportedLegacySQLite:   legacyImport.imported,
		SkippedLegacySQLite:    legacyImport.skipped,
		RemovedLegacySQLite:    removedLegacy,
		Stats:                  stats,
	}, nil
}

func acquireDuckDBRebuildLock(root string) (func(), error) {
	lockPath := DuckDBRebuildLockPath(root)

	err := os.MkdirAll(filepath.Dir(lockPath), duckDBStoreMode)
	if err != nil {
		return nil, fmt.Errorf("create code-intel rebuild lock dir: %w", err)
	}

	file, err := openDuckDBRebuildLock(lockPath)
	if err != nil {
		return nil, err
	}

	_, err = file.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	if err != nil {
		_ = file.Close()

		return nil, fmt.Errorf("write code-intel rebuild lock: %w", err)
	}

	err = file.Close()
	if err != nil {
		return nil, fmt.Errorf("close code-intel rebuild lock: %w", err)
	}

	return func() {
		_ = os.Remove(filepath.Clean(lockPath))
	}, nil
}

func openDuckDBRebuildLock(lockPath string) (*os.File, error) {
	file, err := os.OpenFile(
		filepath.Clean(lockPath),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		duckDBLockFileMode,
	)
	if err == nil {
		return file, nil
	}

	if !os.IsExist(err) {
		return nil, fmt.Errorf("acquire code-intel rebuild lock: %w", err)
	}

	stale, staleErr := duckDBRebuildLockStale(lockPath)
	if staleErr != nil {
		return nil, staleErr
	}

	if !stale {
		return nil, fmt.Errorf(
			"%w: %s",
			apperror.StaticError("active code-intel rebuild lock exists"),
			lockPath,
		)
	}

	removeErr := os.Remove(filepath.Clean(lockPath))
	if removeErr != nil && !os.IsNotExist(removeErr) {
		return nil, fmt.Errorf("remove stale code-intel rebuild lock: %w", removeErr)
	}

	file, err = os.OpenFile(
		filepath.Clean(lockPath),
		os.O_CREATE|os.O_EXCL|os.O_WRONLY,
		duckDBLockFileMode,
	)
	if err != nil {
		return nil, fmt.Errorf(
			"acquire code-intel rebuild lock after stale cleanup: %w",
			err,
		)
	}

	return file, nil
}

func duckDBRebuildLockStale(lockPath string) (bool, error) {
	maintenance, err := inspectDuckDBRebuildLock(lockPath, time.Now().UTC())
	if err != nil {
		return false, err
	}

	return maintenance.Stale, nil
}

// DuckDBRebuildLockPath returns the repo-local code-intel rebuild lock path.
func DuckDBRebuildLockPath(root string) string {
	return filepath.Join(root, downstreamStateDir, "code-intel-rebuild.lock")
}

// InspectDuckDBRebuildLock reports whether the repo-local rebuild lock exists
// and whether its owner process is stale.
func InspectDuckDBRebuildLock(
	root string,
	now time.Time,
) (RebuildLockMaintenance, error) {
	return inspectDuckDBRebuildLock(DuckDBRebuildLockPath(root), now)
}

// CleanupStaleDuckDBRebuildLock removes a stale repo-local rebuild lock after
// validating that its owner process is gone or the lock is beyond the fallback
// stale age on hosts without /proc.
func CleanupStaleDuckDBRebuildLock(
	root string,
	now time.Time,
) (RebuildLockMaintenance, error) {
	maintenance, err := InspectDuckDBRebuildLock(root, now)
	if err != nil {
		return RebuildLockMaintenance{}, err
	}

	if !maintenance.Stale {
		return maintenance, nil
	}

	err = os.Remove(filepath.Clean(maintenance.Path))
	if err != nil && !os.IsNotExist(err) {
		return RebuildLockMaintenance{}, fmt.Errorf(
			"remove stale code-intel rebuild lock: %w",
			err,
		)
	}

	maintenance.Removed = true

	return maintenance, nil
}

func inspectDuckDBRebuildLock(
	lockPath string,
	now time.Time,
) (RebuildLockMaintenance, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}

	maintenance := RebuildLockMaintenance{
		Path: filepath.Clean(lockPath),
	}

	info, err := os.Stat(filepath.Clean(lockPath))
	if err != nil {
		if os.IsNotExist(err) {
			return maintenance, nil
		}

		return RebuildLockMaintenance{}, fmt.Errorf(
			"stat code-intel rebuild lock: %w",
			err,
		)
	}

	maintenance.Exists = true
	maintenance.AgeSeconds = max(int64(now.Sub(info.ModTime()).Seconds()), 0)

	content, err := os.ReadFile(filepath.Clean(lockPath))
	if err != nil {
		return RebuildLockMaintenance{},
			fmt.Errorf("read code-intel rebuild lock: %w", err)
	}

	pid, stalePID := parseDuckDBRebuildLockPID(strings.TrimSpace(string(content)))
	maintenance.PID = pid

	if stalePID {
		maintenance.Stale = true
		maintenance.Reason = "invalid pid"

		return maintenance, nil
	}

	stale, err := duckDBRebuildLockPIDStale(pid)
	if err == nil {
		maintenance.Stale = stale
		if stale {
			maintenance.Reason = "owner process missing"
		}

		return maintenance, nil
	}

	maintenance.Stale = now.Sub(info.ModTime()) > duckDBStaleLockAge
	if maintenance.Stale {
		maintenance.Reason = "lock age exceeded stale threshold"
	}

	return maintenance, nil
}

func parseDuckDBRebuildLockPID(pidText string) (int, bool) {
	pid, err := strconv.Atoi(pidText)

	return pid, err != nil || pid <= 0
}

func duckDBRebuildLockPIDStale(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return false, nil
	}

	if errors.Is(err, syscall.ESRCH) {
		return true, nil
	}

	return false, fmt.Errorf("inspect code-intel rebuild lock pid %d: %w", pid, err)
}

func importLegacySQLiteIntoDuckDB(
	ctx context.Context,
	store *DuckDBStore,
	legacySQLitePath string,
) (legacySQLiteImportResult, error) {
	info, err := os.Stat(legacySQLitePath)
	if err != nil {
		if os.IsNotExist(err) {
			return legacySQLiteImportResult{}, nil
		}

		return legacySQLiteImportResult{},
			errors.Join(apperror.StaticError("stat legacy SQLite store"), err)
	}

	if info.Size() > maxLegacySQLiteImportBytes {
		return legacySQLiteImportResult{
			skipped: true,
			skipReason: "legacy SQLite store is " +
				strconv.FormatInt(info.Size(), 10) +
				" bytes; maximum automatic import is " +
				strconv.FormatInt(maxLegacySQLiteImportBytes, 10) +
				" bytes",
		}, nil
	}

	legacy, err := OpenReadOnly(ctx, legacySQLitePath)
	if err != nil {
		return legacySQLiteImportResult{},
			errors.Join(apperror.StaticError("open legacy SQLite for import"), err)
	}

	err = store.RebuildFromLegacySQLite(ctx, legacy)
	if err != nil {
		_ = legacy.Close()

		return legacySQLiteImportResult{}, err
	}

	err = legacy.Close()
	if err != nil {
		return legacySQLiteImportResult{}, err
	}

	return legacySQLiteImportResult{imported: true}, nil
}

func RemoveLegacySQLiteStore(path string) (bool, error) {
	removed := false

	for _, candidate := range []string{path, path + "-wal", path + "-shm"} {
		err := os.Remove(filepath.Clean(candidate))
		if err == nil {
			removed = true

			continue
		}

		if !os.IsNotExist(err) {
			return false, fmt.Errorf("remove legacy SQLite store %q: %w", candidate, err)
		}
	}

	return removed, nil
}

func UpgradeStorageIfNeeded(
	ctx context.Context,
	root string,
) (StorageUpgradeSummary, error) {
	legacyPath := DefaultDBPath(root)
	duckDBPath := DefaultDuckDBPath(root)

	_, err := os.Stat(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return StorageUpgradeSummary{
				Needed:     false,
				Completed:  true,
				LegacyPath: legacyPath,
				DuckDBPath: duckDBPath,
			}, nil
		}

		return StorageUpgradeSummary{}, fmt.Errorf("stat legacy SQLite store: %w", err)
	}

	rebuild, err := RebuildDuckDBIndex(ctx, root, duckDBPath, legacyPath)
	if err != nil {
		return StorageUpgradeSummary{}, err
	}

	return StorageUpgradeSummary{
		Needed:         true,
		Completed:      rebuild.RemovedLegacySQLite,
		LegacyPath:     legacyPath,
		DuckDBPath:     duckDBPath,
		RebuildSummary: rebuild,
	}, nil
}

func (store *DuckDBStore) ImportEventLog(
	ctx context.Context,
	log EventLog,
) (int, error) {
	transaction, err := store.database.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin DuckDB event import transaction: %w", err)
	}

	defer rollbackUnlessCommitted(transaction)

	_, err = transaction.ExecContext(
		ctx,
		"DELETE FROM code_intel_events",
	)
	if err != nil {
		return 0, fmt.Errorf("clear DuckDB event table: %w", err)
	}

	imported := 0

	for record, readErr := range log.Records() {
		if readErr != nil {
			return 0, readErr
		}

		payload := string(record.Payload)
		if payload == "" {
			payload = "{}"
		}

		_, err = transaction.ExecContext(
			ctx,
			`INSERT INTO code_intel_events(
				event_id, event_kind, recorded_at_utc, source_run_id, trace_id,
				provider, tool, command_shape_sha256, policy_id, skill_id, path,
				payload_json
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			record.ID,
			record.Kind,
			record.RecordedAtUTC,
			record.SourceRunID,
			record.TraceID,
			record.Provider,
			record.Tool,
			record.CommandShape,
			record.PolicyID,
			record.SkillID,
			record.Path,
			payload,
		)
		if err != nil {
			return 0, fmt.Errorf("insert DuckDB event %q: %w", record.ID, err)
		}

		imported++
	}

	err = transaction.Commit()
	if err != nil {
		return 0, fmt.Errorf("commit DuckDB event import transaction: %w", err)
	}

	return imported, nil
}

func (store *DuckDBStore) migrate(ctx context.Context) error {
	err := store.ping(ctx)
	if err != nil {
		return err
	}

	for _, statement := range duckDBSchemaStatements() {
		_, err := store.database.ExecContext(ctx, statement)
		if err != nil {
			return fmt.Errorf("migrate DuckDB code-intel store: %w", err)
		}
	}

	return nil
}

func (store *DuckDBStore) ping(ctx context.Context) error {
	err := store.database.PingContext(ctx)
	if err != nil {
		return fmt.Errorf("ping DuckDB code-intel store: %w", err)
	}

	return nil
}

//nolint:gochecknoglobals // Fixed DuckDB schema metadata is static process data.
var duckDBSchemaStatementList = []string{
	`CREATE TABLE IF NOT EXISTS code_intel_events (
			event_id VARCHAR PRIMARY KEY,
			event_kind VARCHAR NOT NULL,
			recorded_at_utc VARCHAR NOT NULL,
			source_run_id VARCHAR,
			trace_id VARCHAR,
			provider VARCHAR,
			tool VARCHAR,
			command_shape_sha256 VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			path VARCHAR,
			payload_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS traces (
			trace_id VARCHAR PRIMARY KEY,
			trace_kind VARCHAR,
			recorded_at_utc VARCHAR,
			repo_root VARCHAR,
			cwd VARCHAR,
			provider VARCHAR,
			event VARCHAR,
			tool VARCHAR,
			status VARCHAR,
			source_path VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS hook_events (
			trace_id VARCHAR PRIMARY KEY,
			tracking_id VARCHAR,
			session_id VARCHAR,
			provider VARCHAR,
			event VARCHAR,
			tool VARCHAR,
			status VARCHAR,
			operation_kind VARCHAR,
			target_kind VARCHAR,
			risk_category VARCHAR,
			command_sha256 VARCHAR,
			command_shape_sha256 VARCHAR,
			target_set_sha256 VARCHAR,
			cwd VARCHAR,
			source VARCHAR,
			matcher VARCHAR,
			transcript_path VARCHAR,
			runtime_ms BIGINT,
			decision_count BIGINT,
			blocked BIGINT,
			rewritten BIGINT,
			additional_context BIGINT
		)`,
	`CREATE TABLE IF NOT EXISTS hook_decisions (
			trace_id VARCHAR,
			ordinal BIGINT,
			tracking_id VARCHAR,
			policy_id VARCHAR,
			decision VARCHAR,
			severity VARCHAR,
			skill_id VARCHAR,
			implementation VARCHAR,
			principle_ids VARCHAR,
			diagnostic_count BIGINT,
			message_hash VARCHAR,
			suggestion_hash VARCHAR,
			message VARCHAR,
			suggestion VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS hook_targets (
			trace_id VARCHAR,
			ordinal BIGINT,
			target_path VARCHAR,
			target_kind VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS hook_reviews (
			review_id VARCHAR PRIMARY KEY,
			trace_id VARCHAR,
			tracking_id VARCHAR,
			disposition VARCHAR,
			reviewer VARCHAR,
			notes VARCHAR,
			recorded_at_utc VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS proxy_sessions (
			session_id VARCHAR PRIMARY KEY,
			provider VARCHAR,
			model VARCHAR,
			repo_root VARCHAR,
			started_at_utc VARCHAR,
			last_seen_utc VARCHAR,
			request_count BIGINT,
			tool_call_count BIGINT,
			file_read_count BIGINT,
			file_listing_count BIGINT,
			edit_count BIGINT,
			cache_hit_count BIGINT,
			injection_count BIGINT,
			truncation_count BIGINT,
			denial_count BIGINT,
			transform_count BIGINT,
			input_tokens BIGINT,
			output_tokens BIGINT,
			total_tokens BIGINT,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS proxy_events (
			event_id VARCHAR PRIMARY KEY,
			session_id VARCHAR,
			event_kind VARCHAR,
			provider VARCHAR,
			tool VARCHAR,
			model VARCHAR,
			recorded_at_utc VARCHAR,
			trace_id VARCHAR,
			tracking_id VARCHAR,
			repo_root VARCHAR,
			cwd VARCHAR,
			target_path VARCHAR,
			direction VARCHAR,
			payload_kind VARCHAR,
			cache_key VARCHAR,
			input_hash VARCHAR,
			output_hash VARCHAR,
			payload_bytes BIGINT,
			policy_id VARCHAR,
			decision VARCHAR,
			input_tokens BIGINT,
			output_tokens BIGINT,
			total_tokens BIGINT,
			policy_evidence_json VARCHAR,
			dlp_json VARCHAR,
			metadata_json VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS proxy_transforms (
			event_id VARCHAR,
			ordinal BIGINT,
			name VARCHAR,
			reason VARCHAR,
			input_hash VARCHAR,
			output_hash VARCHAR,
			policy_id VARCHAR,
			decision VARCHAR,
			evidence_path VARCHAR,
			input_tokens BIGINT,
			output_tokens BIGINT,
			bytes_removed BIGINT,
			findings_count BIGINT
		)`,
	`CREATE TABLE IF NOT EXISTS findings (
			finding_id VARCHAR PRIMARY KEY,
			rule_id VARCHAR,
			tool VARCHAR,
			code VARCHAR,
			message VARCHAR,
			severity VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			evaluator_kind VARCHAR,
			cel_policy_id VARCHAR,
			cel_expression VARCHAR,
			policy_source VARCHAR,
			path VARCHAR,
			language VARCHAR,
			symbol_kind VARCHAR,
			symbol_name VARCHAR,
			search_text VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS finding_occurrences (
			trace_id VARCHAR,
			ordinal BIGINT,
			finding_id VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			path VARCHAR,
			recorded_at_utc VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS code_files (
			path VARCHAR PRIMARY KEY,
			language VARCHAR,
			content_hash VARCHAR,
			parser_name VARCHAR,
			parser_version VARCHAR,
			source_mtime_utc VARCHAR,
			deleted_at_utc VARCHAR,
			size_bytes BIGINT,
			line_count BIGINT,
			indexed_at_utc VARCHAR,
			stale_reason VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS code_delete_intents (
			intent_id VARCHAR PRIMARY KEY,
			path VARCHAR,
			intent_kind VARCHAR,
			trace_id VARCHAR,
			recorded_at_utc VARCHAR,
			provider VARCHAR,
			event VARCHAR,
			tool VARCHAR,
			status VARCHAR,
			cwd VARCHAR,
			command_sha256 VARCHAR,
			command_preview VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS code_chunks (
			chunk_id VARCHAR PRIMARY KEY,
			path VARCHAR,
			language VARCHAR,
			node_kind VARCHAR,
			symbol_kind VARCHAR,
			symbol_name VARCHAR,
			symbol_path VARCHAR,
			parent_symbol_path VARCHAR,
			parent_chunk_id VARCHAR,
			start_byte BIGINT,
			end_byte BIGINT,
			start_line BIGINT,
			end_line BIGINT,
			content_hash VARCHAR,
			normalized_hash VARCHAR,
			minhash_sig BLOB,
			search_text VARCHAR,
			raw_text VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS code_edges (
			edge_id VARCHAR PRIMARY KEY,
			edge_kind VARCHAR,
			path VARCHAR,
			source_chunk_id VARCHAR,
			target_path VARCHAR,
			target_chunk_id VARCHAR,
			target_symbol_path VARCHAR,
			target_name VARCHAR,
			raw_text VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS diff_edit_patterns (
			pattern_hash VARCHAR PRIMARY KEY,
			diff_source VARCHAR,
			first_git_head VARCHAR,
			last_git_head VARCHAR,
			target_path VARCHAR,
			hunk_header VARCHAR,
			removed_sha256 VARCHAR,
			added_sha256 VARCHAR,
			old_start BIGINT,
			old_lines BIGINT,
			new_start BIGINT,
			new_lines BIGINT,
			ast_chunk_id VARCHAR,
			ast_language VARCHAR,
			ast_node_kind VARCHAR,
			ast_symbol_kind VARCHAR,
			ast_symbol_name VARCHAR,
			ast_symbol_path VARCHAR,
			first_seen_utc VARCHAR,
			last_seen_utc VARCHAR,
			seen_count BIGINT
		)`,
	`CREATE TABLE IF NOT EXISTS ast_finding_links (
			link_id VARCHAR PRIMARY KEY,
			finding_kind VARCHAR,
			finding_id VARCHAR,
			chunk_id VARCHAR,
			path VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			symbol_path VARCHAR,
			content_hash VARCHAR,
			stale BIGINT
		)`,
	`CREATE TABLE IF NOT EXISTS lsh_bands (
			band_hash VARCHAR,
			band_index BIGINT,
			chunk_id VARCHAR,
			path VARCHAR,
			symbol_name VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS sarif_runs (
			sarif_run_id VARCHAR PRIMARY KEY,
			trace_id VARCHAR,
			source_path VARCHAR,
			category VARCHAR,
			tool_name VARCHAR,
			automation_id VARCHAR,
			run_guid VARCHAR,
			baseline_guid VARCHAR,
			produced_at_utc VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS sarif_results (
			sarif_result_id VARCHAR PRIMARY KEY,
			sarif_run_id VARCHAR,
			ordinal BIGINT,
			rule_id VARCHAR,
			level VARCHAR,
			message VARCHAR,
			fingerprint VARCHAR,
			proxy_event_id VARCHAR,
			proxy_session_id VARCHAR,
			proxy_event_kind VARCHAR,
			proxy_direction VARCHAR,
			proxy_payload_kind VARCHAR,
			proxy_trace_id VARCHAR,
			proxy_tracking_id VARCHAR,
			proxy_transform VARCHAR,
			finding_id VARCHAR,
			remediation_id VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			principle_ids VARCHAR,
			path VARCHAR,
			ast_language VARCHAR,
			ast_node_kind VARCHAR,
			ast_symbol_kind VARCHAR,
			ast_symbol_name VARCHAR,
			ast_symbol_path VARCHAR,
			linked_chunk_id VARCHAR,
			start_line BIGINT,
			start_column BIGINT,
			evaluator_kind VARCHAR,
			cel_policy_id VARCHAR,
			cel_expression VARCHAR,
			policy_source VARCHAR,
			search_text VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS remediations (
			remediation_id VARCHAR PRIMARY KEY,
			policy_id VARCHAR,
			skill_id VARCHAR,
			file VARCHAR,
			path VARCHAR,
			message VARCHAR,
			advice VARCHAR,
			search_text VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS remediation_occurrences (
			trace_id VARCHAR,
			ordinal BIGINT,
			remediation_id VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			file VARCHAR,
			path VARCHAR,
			line BIGINT,
			recorded_at_utc VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS remediation_events (
			event_id VARCHAR PRIMARY KEY,
			trace_id VARCHAR,
			remediation_id VARCHAR,
			finding_id VARCHAR,
			event VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			search_text VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS remediation_outcomes (
			outcome_id VARCHAR PRIMARY KEY,
			remediation_id VARCHAR,
			finding_id VARCHAR,
			source_trace_id VARCHAR,
			followup_trace_id VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			file VARCHAR,
			path VARCHAR,
			provider VARCHAR,
			tool VARCHAR,
			outcome VARCHAR,
			attempt_ordinal BIGINT,
			recorded_at_utc VARCHAR,
			search_text VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS embedding_records (
			embedding_id VARCHAR PRIMARY KEY,
			backend VARCHAR,
			collection VARCHAR,
			model_id VARCHAR,
			dimension BIGINT,
			input_kind VARCHAR,
			record_kind VARCHAR,
			record_id VARCHAR,
			trace_id VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			path VARCHAR,
			content_hash VARCHAR,
			provider VARCHAR,
			backend_row_id VARCHAR,
			created_at_utc VARCHAR,
			raw_json VARCHAR
		)`,
	`CREATE TABLE IF NOT EXISTS code_intel_search (
			kind VARCHAR,
			policy_id VARCHAR,
			skill_id VARCHAR,
			path VARCHAR,
			message VARCHAR,
			search_text VARCHAR,
			record_id VARCHAR,
			trace_id VARCHAR
		)`,
}

func duckDBSchemaStatements() []string {
	return duckDBSchemaStatementList
}

//nolint:gochecknoglobals // Fixed table-copy metadata is static process data.
var legacyImportTableSpecs = []tableCopySpec{
	{name: "traces", columns: []string{
		"trace_id", "trace_kind", "recorded_at_utc", "repo_root", "cwd",
		"provider", "event", "tool", "status", "source_path", "raw_json",
	}},
	{name: "hook_events", columns: []string{
		"trace_id", "tracking_id", "session_id", "provider", "event", "tool",
		"status", "operation_kind", "target_kind", "risk_category",
		"command_sha256", "command_shape_sha256", "target_set_sha256", "cwd",
		"source", "matcher", "transcript_path", "runtime_ms", "decision_count",
		"blocked", "rewritten", "additional_context",
	}},
	{name: "hook_decisions", columns: []string{
		"trace_id", "ordinal", "tracking_id", "policy_id", "decision",
		"severity", "skill_id", "implementation", "principle_ids",
		"diagnostic_count", "message_hash", "suggestion_hash", "message",
		"suggestion",
	}},
	{name: "hook_targets", columns: []string{
		"trace_id", "ordinal", "target_path", "target_kind",
	}},
	{name: "hook_reviews", columns: []string{
		"review_id", "trace_id", "tracking_id", "disposition", "reviewer",
		"notes", "recorded_at_utc",
	}},
	{name: "proxy_sessions", columns: []string{
		"session_id", "provider", "model", "repo_root", "started_at_utc",
		"last_seen_utc", "request_count", "tool_call_count",
		"file_read_count", "file_listing_count", "edit_count",
		"cache_hit_count", "injection_count", "truncation_count",
		"denial_count", "transform_count", "input_tokens", "output_tokens",
		"total_tokens", "raw_json",
	}},
	{name: "proxy_events", columns: []string{
		"event_id", "session_id", "event_kind", "provider", "tool", "model",
		"recorded_at_utc", "trace_id", "tracking_id", "repo_root", "cwd",
		"target_path", "direction", "payload_kind", "cache_key", "input_hash",
		"output_hash", "payload_bytes", "policy_id", "decision",
		"input_tokens", "output_tokens", "total_tokens", "policy_evidence_json",
		"dlp_json", "metadata_json", "raw_json",
	}},
	{name: "proxy_transforms", columns: []string{
		"event_id", "ordinal", "name", "reason", "input_hash", "output_hash",
		"policy_id", "decision", "evidence_path", "input_tokens",
		"output_tokens", "bytes_removed", "findings_count",
	}},
	{name: "findings", columns: []string{
		"finding_id", "rule_id", "tool", "code", "message", "severity",
		"policy_id", "skill_id", "evaluator_kind", "cel_policy_id",
		"cel_expression", "policy_source", "path", "language", "symbol_kind",
		"symbol_name", "search_text", "raw_json",
	}},
	{name: "finding_occurrences", columns: []string{
		"trace_id", "ordinal", "finding_id", "policy_id", "skill_id", "path",
		"recorded_at_utc",
	}},
	{name: "code_files", columns: []string{
		"path", "language", "content_hash", "parser_name", "parser_version",
		"source_mtime_utc", "deleted_at_utc", "size_bytes", "line_count",
		"indexed_at_utc", "stale_reason",
	}},
	{name: "code_delete_intents", columns: []string{
		"intent_id", "path", "intent_kind", "trace_id", "recorded_at_utc",
		"provider", "event", "tool", "status", "cwd", "command_sha256",
		"command_preview", "raw_json",
	}},
	{name: "code_chunks", columns: []string{
		"chunk_id", "path", "language", "node_kind", "symbol_kind",
		"symbol_name", "symbol_path", "parent_symbol_path", "parent_chunk_id",
		"start_byte", "end_byte", "start_line", "end_line", "content_hash",
		"normalized_hash", "minhash_sig", "search_text", "raw_text",
	}},
	{name: "code_edges", columns: []string{
		"edge_id", "edge_kind", "path", "source_chunk_id", "target_path",
		"target_chunk_id", "target_symbol_path", "target_name", "raw_text",
	}},
	{name: "diff_edit_patterns", columns: []string{
		"pattern_hash", "diff_source", "first_git_head", "last_git_head",
		"target_path", "hunk_header", "removed_sha256", "added_sha256",
		"old_start", "old_lines", "new_start", "new_lines", "ast_chunk_id",
		"ast_language", "ast_node_kind", "ast_symbol_kind", "ast_symbol_name",
		"ast_symbol_path", "first_seen_utc", "last_seen_utc", "seen_count",
	}},
	{name: "ast_finding_links", columns: []string{
		"link_id", "finding_kind", "finding_id", "chunk_id", "path",
		"policy_id", "skill_id", "symbol_path", "content_hash", "stale",
	}},
	{name: "lsh_bands", columns: []string{
		"band_hash", "band_index", "chunk_id", "path", "symbol_name",
	}},
	{name: "sarif_runs", columns: []string{
		"sarif_run_id", "trace_id", "source_path", "category", "tool_name",
		"automation_id", "run_guid", "baseline_guid", "produced_at_utc",
		"raw_json",
	}},
	{name: "sarif_results", columns: []string{
		"sarif_result_id", "sarif_run_id", "ordinal", "rule_id", "level",
		"message", "fingerprint", "proxy_event_id", "proxy_session_id",
		"proxy_event_kind", "proxy_direction", "proxy_payload_kind",
		"proxy_trace_id", "proxy_tracking_id", "proxy_transform",
		"finding_id", "remediation_id", "policy_id", "skill_id",
		"principle_ids", "path", "ast_language", "ast_node_kind",
		"ast_symbol_kind", "ast_symbol_name", "ast_symbol_path",
		"linked_chunk_id", "start_line", "start_column", "evaluator_kind",
		"cel_policy_id", "cel_expression", "policy_source", "search_text",
		"raw_json",
	}},
	{name: "remediations", columns: []string{
		"remediation_id", "policy_id", "skill_id", "file", "path", "message",
		"advice", "search_text", "raw_json",
	}},
	{name: "remediation_occurrences", columns: []string{
		"trace_id", "ordinal", "remediation_id", "policy_id", "skill_id",
		"file", "path", "line", "recorded_at_utc",
	}},
	{name: "remediation_events", columns: []string{
		"event_id", "trace_id", "remediation_id", "finding_id", "event",
		"policy_id", "skill_id", "search_text", "raw_json",
	}},
	{name: "remediation_outcomes", columns: []string{
		"outcome_id", "remediation_id", "finding_id", "source_trace_id",
		"followup_trace_id", "policy_id", "skill_id", "file", "path",
		"provider", "tool", "outcome", "attempt_ordinal", "recorded_at_utc",
		"search_text", "raw_json",
	}},
	{name: "embedding_records", columns: []string{
		"embedding_id", "backend", "collection", "model_id", "dimension",
		"input_kind", "record_kind", "record_id", "trace_id", "policy_id",
		"skill_id", "path", "content_hash", "provider", "backend_row_id",
		"created_at_utc", "raw_json",
	}},
	{
		name:        "code_intel_fts",
		destination: "code_intel_search",
		columns: []string{
			"kind", "policy_id", "skill_id", "path", "message", "search_text",
			"record_id", "trace_id",
		},
	},
}

func legacyImportTables() []tableCopySpec {
	return legacyImportTableSpecs
}

type tableCopySpec struct {
	name        string
	destination string
	columns     []string
}

func clearDuckDBTables(ctx context.Context, database *sql.DB) error {
	tables := legacyImportTables()
	for index := len(tables) - 1; index >= 0; index-- {
		destination := tables[index].target()
		// #nosec G202 -- destination comes from fixed tableCopySpec values.
		_, err := database.ExecContext(ctx, "DELETE FROM "+destination)
		if err != nil {
			return fmt.Errorf("clear DuckDB table %s: %w", destination, err)
		}
	}

	return nil
}

func copyTableRows(
	ctx context.Context,
	source *sql.DB,
	destination *sql.DB,
	table tableCopySpec,
) error {
	// #nosec G202 -- table metadata is fixed in legacyImportTableSpecs.
	query := "SELECT " + strings.Join(table.columns, ", ") + " FROM " + table.name

	rows, err := source.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("query legacy SQLite table %s: %w", table.name, err)
	}
	defer rows.Close()

	destinationTable := table.target()

	transaction, err := destination.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin DuckDB table %s transaction: %w", destinationTable, err)
	}

	defer rollbackUnlessCommitted(transaction)

	// #nosec G202 -- table metadata is fixed in legacyImportTableSpecs.
	insert := "INSERT INTO " + destinationTable + " (" +
		strings.Join(table.columns, ", ") + ") VALUES (" +
		placeholders(len(table.columns)) + ")"
	for rows.Next() {
		values := make([]any, len(table.columns))

		targets := make([]any, len(table.columns))
		for index := range values {
			targets[index] = &values[index]
		}

		err = rows.Scan(targets...)
		if err != nil {
			return fmt.Errorf("scan legacy SQLite table %s: %w", table.name, err)
		}

		_, err = transaction.ExecContext(ctx, insert, values...)
		if err != nil {
			return fmt.Errorf("insert DuckDB table %s: %w", destinationTable, err)
		}
	}

	err = rows.Err()
	if err != nil {
		return fmt.Errorf("iterate legacy SQLite table %s: %w", table.name, err)
	}

	err = transaction.Commit()
	if err != nil {
		return fmt.Errorf("commit DuckDB table %s transaction: %w", destinationTable, err)
	}

	return nil
}

func (table tableCopySpec) target() string {
	if table.destination != "" {
		return table.destination
	}

	return table.name
}

func placeholders(count int) string {
	values := make([]string, count)
	for index := range values {
		values[index] = "?"
	}

	return strings.Join(values, ", ")
}

func duckDBStatsQueries(stats *Stats) []statCountQuery {
	queries := duckDBCoreStatsQueries(stats)

	return append(queries, duckDBExtendedStatsQueries(stats)...)
}

func duckDBCoreStatsQueries(stats *Stats) []statCountQuery {
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
		{name: "findings", query: "SELECT COUNT(*) FROM findings", target: &stats.Findings},
		{name: "files", query: "SELECT COUNT(*) FROM code_files", target: &stats.Files},
	}
}

func duckDBExtendedStatsQueries(stats *Stats) []statCountQuery {
	return []statCountQuery{
		{
			name:   "code_chunks",
			query:  "SELECT COUNT(*) FROM code_chunks",
			target: &stats.CodeChunks,
		},
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
			name:   "code_intel_search",
			query:  "SELECT COUNT(*) FROM code_intel_search",
			target: &stats.FtsRows,
		},
	}
}
