// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"

	"blackcat.ca/coding-ethos/go/internal/apperror"
)

const (
	duckDBStoreMode    = 0o700
	duckDBLockFileMode = 0o600
	duckDBStaleLockAge = 30 * time.Minute
)

// DuckDBStore is the code-intel analytical query store.
type DuckDBStore struct {
	database *sql.DB
	path     string
}

// RebuildIndexSummary reports a DuckDB rebuild/import run.
type RebuildIndexSummary struct {
	Backend                  string   `json:"backend"`
	Path                     string   `json:"path"`
	RemovedObsoleteArtifacts []string `json:"removed_obsolete_artifacts,omitempty"`
	EventCount               int      `json:"event_count"`
	ImportedEventCount       int      `json:"imported_event_count"`
	Stats                    Stats    `json:"stats"`
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

type StorageUpgradeSummary struct {
	ObsoleteArtifactPaths []string            `json:"obsolete_artifact_paths,omitempty"`
	DuckDBPath            string              `json:"duckdb_path,omitempty"`
	RebuildSummary        RebuildIndexSummary `json:"rebuild_summary,omitzero"`
	Needed                bool                `json:"needed"`
	Completed             bool                `json:"completed"`
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

func RebuildDuckDBIndex(
	ctx context.Context,
	root string,
	duckDBPath string,
	_ string,
) (RebuildIndexSummary, error) {
	stateRoot := ResolveStateRoot(root)
	if strings.TrimSpace(duckDBPath) == "" {
		duckDBPath = DefaultDuckDBPath(stateRoot)
	}

	release, err := acquireDuckDBRebuildLock(stateRoot)
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
		NewEventLog(DefaultEventLogDir(ResolveStateRoot(root))),
	)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	removedObsolete, err := RemoveObsoleteCodeIntelArtifacts(stateRoot)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return RebuildIndexSummary{}, err
	}

	return RebuildIndexSummary{
		Backend:                  "duckdb",
		Path:                     duckDBPath,
		RemovedObsoleteArtifacts: removedObsolete,
		EventCount:               eventCount,
		ImportedEventCount:       importedEventCount,
		Stats:                    stats,
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

func ObsoleteCodeIntelArtifactPaths(root string) []string {
	path := filepath.Join(root, downstreamStateDir, "code-intel."+"db")

	return []string{path, path + "-wal", path + "-shm"}
}

func RemoveObsoleteCodeIntelArtifacts(root string) ([]string, error) {
	removed := make([]string, 0, len(ObsoleteCodeIntelArtifactPaths(root)))

	for _, candidate := range ObsoleteCodeIntelArtifactPaths(root) {
		err := os.Remove(filepath.Clean(candidate))
		if err == nil {
			removed = append(removed, candidate)

			continue
		}

		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("remove obsolete code-intel artifact %q: %w", candidate, err)
		}
	}

	return removed, nil
}

func UpgradeStorageIfNeeded(
	ctx context.Context,
	root string,
) (StorageUpgradeSummary, error) {
	stateRoot := ResolveStateRoot(root)
	duckDBPath := DefaultDuckDBPath(stateRoot)
	obsoletePaths := ObsoleteCodeIntelArtifactPaths(stateRoot)
	needed := false

	for _, path := range obsoletePaths {
		_, err := os.Stat(path)
		if err == nil {
			needed = true

			break
		}

		if !os.IsNotExist(err) {
			return StorageUpgradeSummary{}, fmt.Errorf(
				"stat obsolete code-intel artifact: %w",
				err,
			)
		}
	}

	if !needed {
		return StorageUpgradeSummary{
			Needed:     false,
			Completed:  true,
			DuckDBPath: duckDBPath,
		}, nil
	}

	rebuild, err := RebuildDuckDBIndex(ctx, root, duckDBPath, "")
	if err != nil {
		return StorageUpgradeSummary{}, err
	}

	return StorageUpgradeSummary{
		Needed:                true,
		Completed:             len(rebuild.RemovedObsoleteArtifacts) > 0,
		ObsoleteArtifactPaths: obsoletePaths,
		DuckDBPath:            duckDBPath,
		RebuildSummary:        rebuild,
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

func duckDBSchemaStatements() []string {
	baseStatements := schemaStatements()
	statements := make([]string, 0, len(baseStatements)+1)
	statements = append(statements, duckDBEventSchemaStatement)

	for _, statement := range baseStatements {
		statements = append(
			statements,
			duckDBSchemaStatement(statement),
		)
	}

	return statements
}

func duckDBSchemaStatement(statement string) string {
	return duckDBForeignKeyLinePattern.ReplaceAllString(statement, "")
}

var duckDBForeignKeyLinePattern = regexp.MustCompile(`(?m),?\n\s*FOREIGN KEY\([^\n]+`)

const duckDBEventSchemaStatement = `CREATE TABLE IF NOT EXISTS code_intel_events (
	event_id TEXT PRIMARY KEY,
	event_kind TEXT NOT NULL,
	recorded_at_utc TEXT NOT NULL,
	source_run_id TEXT,
	trace_id TEXT,
	provider TEXT,
	tool TEXT,
	command_shape_sha256 TEXT,
	policy_id TEXT,
	skill_id TEXT,
	path TEXT,
	payload_json TEXT
)`

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
			name:   "decisions",
			query:  "SELECT COUNT(*) FROM decisions",
			target: &stats.Decisions,
		},
		{
			name:   "decision_links",
			query:  "SELECT COUNT(*) FROM decision_links",
			target: &stats.DecisionLinks,
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
