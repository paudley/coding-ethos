// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	downstreamDefaultLimit    = 10
	downstreamLargeLogBytes   = 100_000
	downstreamHookRunsSubpath = ".coding-ethos/hook-runs"
	downstreamAppendOnlyHint  = "observed SQLITE_BUSY; consider append-only " +
		"per-run traces with later ingestion"
)

type DownstreamAnalysis struct {
	HookFriction      []DownstreamHookFriction     `json:"hook_friction,omitempty"`
	PolicyBlockers    []DownstreamPolicyBlocker    `json:"policy_blockers,omitempty"`
	FindingHotspots   []DownstreamFindingHotspot   `json:"finding_hotspots,omitempty"`
	FilePressure      []DownstreamFilePressure     `json:"file_pressure,omitempty"`
	ToolchainFailures []DownstreamToolchainFailure `json:"toolchain_failures,omitempty"`
	SQLiteStrategy    DownstreamSQLiteStrategy     `json:"sqlite_strategy"`
	Stats             Stats                        `json:"stats,omitzero"`
	LogSignals        DownstreamLogSignals         `json:"log_signals"`
}

type DownstreamHookFriction struct {
	OperationKind string `json:"operation_kind,omitempty"`
	TargetKind    string `json:"target_kind,omitempty"`
	RiskCategory  string `json:"risk_category,omitempty"`
	Status        string `json:"status,omitempty"`
	Blocked       bool   `json:"blocked"`
	Count         int    `json:"count"`
}

type DownstreamPolicyBlocker struct {
	PolicyID        string `json:"policy_id,omitempty"`
	Decision        string `json:"decision,omitempty"`
	Severity        string `json:"severity,omitempty"`
	DiagnosticCount int    `json:"diagnostic_count,omitempty"`
	Count           int    `json:"count"`
}

type DownstreamFindingHotspot struct {
	Path     string `json:"path,omitempty"`
	Tool     string `json:"tool,omitempty"`
	Code     string `json:"code,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	Count    int    `json:"count"`
}

type DownstreamFilePressure struct {
	Path        string `json:"path"`
	Language    string `json:"language,omitempty"`
	StaleReason string `json:"stale_reason,omitempty"`
	LineCount   int    `json:"line_count"`
	SizeBytes   int    `json:"size_bytes"`
}

type DownstreamToolchainFailure struct {
	Tool     string `json:"tool,omitempty"`
	Code     string `json:"code,omitempty"`
	PolicyID string `json:"policy_id,omitempty"`
	Message  string `json:"message,omitempty"`
	Count    int    `json:"count"`
}

type DownstreamLogSignals struct {
	HookRunCount           int `json:"hook_run_count"`
	EventJSONCount         int `json:"event_json_count"`
	NonEmptyStdoutLogs     int `json:"non_empty_stdout_logs"`
	NonEmptyStderrLogs     int `json:"non_empty_stderr_logs"`
	LargeLogCount          int `json:"large_log_count"`
	SQLiteBusyCount        int `json:"sqlite_busy_count"`
	StaleRepoMapCount      int `json:"stale_repo_map_count"`
	SandboxMissingCount    int `json:"sandbox_missing_count"`
	DirectGitHookCount     int `json:"direct_git_hook_count"`
	ProtectedBranchCount   int `json:"protected_branch_count"`
	ProviderRequiredCount  int `json:"provider_required_count"`
	InlineEnvCount         int `json:"inline_env_count"`
	LineLimitCount         int `json:"line_limit_count"`
	UnparsedDiagnosticLogs int `json:"unparsed_diagnostic_logs"`
}

type DownstreamSQLiteStrategy struct {
	OpenError              string `json:"open_error,omitempty"`
	JournalMode            string `json:"journal_mode,omitempty"`
	AppendOnlyFallbackHint string `json:"append_only_fallback_hint,omitempty"`
	BusyTimeoutMS          int    `json:"busy_timeout_ms,omitempty"`
	SQLiteBusyLogCount     int    `json:"sqlite_busy_log_count"`
	StoreAvailable         bool   `json:"store_available"`
	ReadOnlyAnalysis       bool   `json:"read_only_analysis"`
	SingleConnectionPool   bool   `json:"single_connection_pool"`
}

func AnalyzeDownstream(
	ctx context.Context,
	root string,
	store *Store,
	limit int,
) (DownstreamAnalysis, error) {
	if limit <= 0 {
		limit = downstreamDefaultLimit
	}

	logSignals, err := scanDownstreamHookLogs(root)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis := DownstreamAnalysis{
		LogSignals: logSignals,
		SQLiteStrategy: DownstreamSQLiteStrategy{
			StoreAvailable:       store != nil,
			SQLiteBusyLogCount:   logSignals.SQLiteBusyCount,
			ReadOnlyAnalysis:     true,
			SingleConnectionPool: store != nil,
		},
	}

	if logSignals.SQLiteBusyCount > 0 {
		analysis.SQLiteStrategy.AppendOnlyFallbackHint = downstreamAppendOnlyHint
	}

	if store == nil {
		return analysis, nil
	}

	return populateDownstreamAnalysisFromStore(ctx, store, limit, analysis)
}

func populateDownstreamAnalysisFromStore(
	ctx context.Context,
	store *Store,
	limit int,
	analysis DownstreamAnalysis,
) (DownstreamAnalysis, error) {
	stats, err := store.Stats(ctx)
	if err != nil {
		return DownstreamAnalysis{}, fmt.Errorf("read downstream stats: %w", err)
	}

	analysis.Stats = stats

	journalMode, busyTimeout, err := sqliteRuntimeSettings(ctx, store.database)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.SQLiteStrategy.JournalMode = journalMode
	analysis.SQLiteStrategy.BusyTimeoutMS = busyTimeout

	analysis.HookFriction, err = downstreamHookFriction(ctx, store.database, limit)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.PolicyBlockers, err = downstreamPolicyBlockers(
		ctx,
		store.database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.FindingHotspots, err = downstreamFindingHotspots(
		ctx,
		store.database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.FilePressure, err = downstreamFilePressure(ctx, store.database, limit)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.ToolchainFailures, err = downstreamToolchainFailures(
		ctx,
		store.database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	return analysis, nil
}

func sqliteRuntimeSettings(
	ctx context.Context,
	database *sql.DB,
) (string, int, error) {
	var journalMode string

	err := database.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(
		&journalMode,
	)
	if err != nil {
		return "", 0, fmt.Errorf("read SQLite journal mode: %w", err)
	}

	var busyTimeout int

	err = database.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(
		&busyTimeout,
	)
	if err != nil {
		return "", 0, fmt.Errorf("read SQLite busy timeout: %w", err)
	}

	return journalMode, busyTimeout, nil
}

func downstreamHookFriction(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamHookFriction, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT
			COALESCE(operation_kind, ''),
			COALESCE(target_kind, ''),
			COALESCE(risk_category, ''),
			COALESCE(status, ''),
			blocked,
			COUNT(*) AS count
		FROM hook_events
		GROUP BY operation_kind, target_kind, risk_category, status, blocked
		ORDER BY count DESC, blocked DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream hook friction: %w", err)
	}
	defer rows.Close()

	results := []DownstreamHookFriction{}

	for rows.Next() {
		var result DownstreamHookFriction

		var blocked int

		scanErr := rows.Scan(
			&result.OperationKind,
			&result.TargetKind,
			&result.RiskCategory,
			&result.Status,
			&blocked,
			&result.Count,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan downstream hook friction: %w", scanErr)
		}

		result.Blocked = blocked != 0
		results = append(results, result)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate downstream hook friction: %w", rowsErr)
	}

	return results, nil
}

func downstreamPolicyBlockers(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamPolicyBlocker, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT
			COALESCE(policy_id, ''),
			COALESCE(decision, ''),
			COALESCE(severity, ''),
			diagnostic_count,
			COUNT(*) AS count
		FROM hook_decisions
		WHERE decision = 'block' OR severity = 'block'
		GROUP BY policy_id, decision, severity, diagnostic_count
		ORDER BY count DESC, diagnostic_count DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream policy blockers: %w", err)
	}
	defer rows.Close()

	results := []DownstreamPolicyBlocker{}

	for rows.Next() {
		var result DownstreamPolicyBlocker

		scanErr := rows.Scan(
			&result.PolicyID,
			&result.Decision,
			&result.Severity,
			&result.DiagnosticCount,
			&result.Count,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan downstream policy blocker: %w", scanErr)
		}

		results = append(results, result)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate downstream policy blockers: %w", rowsErr)
	}

	return results, nil
}

func downstreamFindingHotspots(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamFindingHotspot, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT
			COALESCE(f.path, fo.path, ''),
			COALESCE(f.tool, ''),
			COALESCE(f.code, ''),
			COALESCE(f.policy_id, fo.policy_id, ''),
			COUNT(*) AS count
		FROM finding_occurrences fo
		JOIN findings f ON f.finding_id = fo.finding_id
		GROUP BY f.path, fo.path, f.tool, f.code, f.policy_id, fo.policy_id
		ORDER BY count DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream finding hotspots: %w", err)
	}
	defer rows.Close()

	results := []DownstreamFindingHotspot{}

	for rows.Next() {
		var result DownstreamFindingHotspot

		scanErr := rows.Scan(
			&result.Path,
			&result.Tool,
			&result.Code,
			&result.PolicyID,
			&result.Count,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan downstream finding hotspot: %w", scanErr)
		}

		results = append(results, result)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate downstream finding hotspots: %w", rowsErr)
	}

	return results, nil
}

func downstreamFilePressure(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamFilePressure, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT path, language, line_count, size_bytes, COALESCE(stale_reason, '')
		FROM code_files
		WHERE COALESCE(deleted_at_utc, '') = ''
		ORDER BY line_count DESC, size_bytes DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream file pressure: %w", err)
	}
	defer rows.Close()

	results := []DownstreamFilePressure{}

	for rows.Next() {
		var result DownstreamFilePressure

		scanErr := rows.Scan(
			&result.Path,
			&result.Language,
			&result.LineCount,
			&result.SizeBytes,
			&result.StaleReason,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan downstream file pressure: %w", scanErr)
		}

		results = append(results, result)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate downstream file pressure: %w", rowsErr)
	}

	return results, nil
}

func downstreamToolchainFailures(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamToolchainFailure, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT
			COALESCE(tool, ''),
			COALESCE(code, ''),
			COALESCE(policy_id, ''),
			COALESCE(message, ''),
			COUNT(*) AS count
		FROM findings
		WHERE policy_id LIKE 'runtime.%'
			OR policy_id LIKE 'tool.%'
			OR message LIKE '%sandbox%'
			OR message LIKE '%managed tool%'
		GROUP BY tool, code, policy_id, message
		ORDER BY count DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream toolchain failures: %w", err)
	}
	defer rows.Close()

	results := []DownstreamToolchainFailure{}

	for rows.Next() {
		var result DownstreamToolchainFailure

		scanErr := rows.Scan(
			&result.Tool,
			&result.Code,
			&result.PolicyID,
			&result.Message,
			&result.Count,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan downstream toolchain failure: %w", scanErr)
		}

		results = append(results, result)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate downstream toolchain failures: %w", rowsErr)
	}

	return results, nil
}

func scanDownstreamHookLogs(root string) (DownstreamLogSignals, error) {
	signals := DownstreamLogSignals{}
	hookRuns := filepath.Join(root, downstreamHookRunsSubpath)

	err := filepath.WalkDir(
		hookRuns,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			return updateDownstreamLogSignals(hookRuns, path, entry, &signals)
		},
	)
	if err != nil && !os.IsNotExist(err) {
		return DownstreamLogSignals{}, fmt.Errorf("scan hook run logs: %w", err)
	}

	return signals, nil
}

func updateDownstreamLogSignals(
	hookRuns string,
	path string,
	entry fs.DirEntry,
	signals *DownstreamLogSignals,
) error {
	if entry.IsDir() {
		if path != hookRuns && filepath.Dir(path) == hookRuns {
			signals.HookRunCount++
		}

		return nil
	}

	name := entry.Name()
	if name == "event.json" {
		signals.EventJSONCount++
	}

	if name != "stdout.log" && name != "stderr.log" {
		return nil
	}

	info, err := entry.Info()
	if err != nil {
		return fmt.Errorf("stat hook log %q: %w", path, err)
	}

	if info.Size() == 0 {
		return nil
	}

	if info.Size() > downstreamLargeLogBytes {
		signals.LargeLogCount++
	}

	if name == "stdout.log" {
		signals.NonEmptyStdoutLogs++
	} else {
		signals.NonEmptyStderrLogs++
	}

	return scanDownstreamLogFile(path, signals)
}

func scanDownstreamLogFile(path string, signals *DownstreamLogSignals) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open hook log %q: %w", path, err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			recordDownstreamLogLine(line, signals)
		}

		if readErr == nil {
			continue
		}

		if readErr == io.EOF {
			return nil
		}

		return fmt.Errorf("read hook log %q: %w", path, readErr)
	}
}

func recordDownstreamLogLine(line string, signals *DownstreamLogSignals) {
	lower := strings.ToLower(line)

	recordSQLiteBusySignal(lower, signals)
	recordStaleRepoMapSignal(lower, signals)
	recordSandboxMissingSignal(lower, signals)
	recordDirectGitSignal(lower, signals)
	recordProtectedBranchSignal(lower, signals)
	recordProviderRequiredSignal(lower, signals)
	recordInlineEnvSignal(lower, signals)
	recordLineLimitSignal(lower, signals)
	recordUnparsedDiagnosticSignal(lower, signals)
}

func recordSQLiteBusySignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "sqlite_busy") ||
		strings.Contains(lower, "database is locked") {
		signals.SQLiteBusyCount++
	}
}

func recordStaleRepoMapSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "stale code context") ||
		strings.Contains(lower, "repo map query failed") {
		signals.StaleRepoMapCount++
	}
}

func recordSandboxMissingSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "coding-ethos-sandbox") &&
		(strings.Contains(lower, "no such file") ||
			strings.Contains(lower, "sandbox_denied")) {
		signals.SandboxMissingCount++
	}
}

func recordDirectGitSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "direct git execution reached coding-ethos hooks") {
		signals.DirectGitHookCount++
	}
}

func recordProtectedBranchSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "protected branch") {
		signals.ProtectedBranchCount++
	}
}

func recordProviderRequiredSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "hook.provider_required") ||
		strings.Contains(lower, "provider_required") {
		signals.ProviderRequiredCount++
	}
}

func recordInlineEnvSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "shell.inline_env") ||
		strings.Contains(lower, "inline command environment") {
		signals.InlineEnvCount++
	}
}

func recordLineLimitSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "filesystem.line_limits") ||
		strings.Contains(lower, "large source files must not keep growing") {
		signals.LineLimitCount++
	}
}

func recordUnparsedDiagnosticSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "no parseable diagnostics") ||
		strings.Contains(lower, "unparsed diagnostic") {
		signals.UnparsedDiagnosticLogs++
	}
}
