// SPDX-FileCopyrightText: 2026 Blackcat Informatics Inc. <paudley@blackcat.ca>
// SPDX-License-Identifier: AGPL-3.0-only

package codeintel

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	downstreamDefaultLimit   = 10
	downstreamLargeLogBytes  = 100_000
	downstreamLogScanLimit   = 500
	downstreamScannerBuffer  = 64 * 1024
	downstreamCommandFanout  = 5
	downstreamStateDir       = ".coding-ethos"
	downstreamHookRunsDir    = "hook-runs"
	downstreamLintRunsDir    = "lint-runs"
	downstreamEventJSONFile  = "event.json"
	downstreamRebuildIndex   = "rebuild_index"
	downstreamHealthy        = "healthy"
	downstreamAppendOnlyHint = "observed SQLITE_BUSY; consider append-only " +
		"per-run traces with later ingestion"
)

type DownstreamAnalysis struct {
	IssueSummary      DownstreamIssueSummary       `json:"issue_summary,omitzero"`
	RemediationLoops  []DownstreamRemediationLoop  `json:"remediation_loops,omitempty"`
	AffectedCommands  []DownstreamAffectedCommand  `json:"affected_commands,omitempty"`
	HookFriction      []DownstreamHookFriction     `json:"hook_friction,omitempty"`
	FindingHotspots   []DownstreamFindingHotspot   `json:"finding_hotspots,omitempty"`
	FilePressure      []DownstreamFilePressure     `json:"file_pressure,omitempty"`
	ToolchainFailures []DownstreamToolchainFailure `json:"toolchain_failures,omitempty"`
	ToolchainHealth   []DownstreamToolchainHealth  `json:"toolchain_health,omitempty"`
	EvidenceGaps      []DownstreamEvidenceGap      `json:"evidence_gaps,omitempty"`
	PolicyBlockers    []DownstreamPolicyBlocker    `json:"policy_blockers,omitempty"`
	StorageStrategy   DownstreamStorageStrategy    `json:"storage_strategy"`
	StorageHealth     DownstreamStorageHealth      `json:"storage_health"`
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
	PolicyID         string                      `json:"policy_id,omitempty"`
	Decision         string                      `json:"decision,omitempty"`
	Severity         string                      `json:"severity,omitempty"`
	AffectedCommands []DownstreamAffectedCommand `json:"affected_commands,omitempty"`
	DiagnosticCount  int                         `json:"diagnostic_count,omitempty"`
	Count            int                         `json:"count"`
}

type DownstreamAffectedCommand struct {
	Tool               string `json:"tool,omitempty"`
	OperationKind      string `json:"operation_kind,omitempty"`
	TargetKind         string `json:"target_kind,omitempty"`
	RiskCategory       string `json:"risk_category,omitempty"`
	Status             string `json:"status,omitempty"`
	CommandShapeSHA256 string `json:"command_shape_sha256,omitempty"`
	Count              int    `json:"count"`
}

type DownstreamRemediationLoop struct {
	RemediationID   string `json:"remediation_id,omitempty"`
	PolicyID        string `json:"policy_id,omitempty"`
	SkillID         string `json:"skill_id,omitempty"`
	File            string `json:"file,omitempty"`
	Path            string `json:"path,omitempty"`
	LastSeenUTC     string `json:"last_seen_utc,omitempty"`
	TraceCount      int    `json:"trace_count"`
	AttemptedCount  int    `json:"attempted_count"`
	RepeatedCount   int    `json:"repeated_count"`
	OccurrenceCount int    `json:"occurrence_count"`
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

type DownstreamToolchainHealth struct {
	RootCause string `json:"root_cause"`
	Message   string `json:"message"`
	Count     int    `json:"count"`
}

type DownstreamEvidenceGap struct {
	Signal         string `json:"signal"`
	Source         string `json:"source"`
	QueryIndex     string `json:"query_index"`
	Recommendation string `json:"recommendation"`
	Count          int    `json:"count"`
}

type DownstreamIssueSummary struct {
	Title           string   `json:"title,omitempty"`
	StorageDecision string   `json:"storage_decision,omitempty"`
	TopFindings     []string `json:"top_findings,omitempty"`
	NextActions     []string `json:"next_actions,omitempty"`
}

type DownstreamLogSignals struct {
	HookRunCount           int `json:"hook_run_count"`
	LintRunCount           int `json:"lint_run_count"`
	EventJSONCount         int `json:"event_json_count"`
	NonEmptyStdoutLogs     int `json:"non_empty_stdout_logs"`
	NonEmptyStderrLogs     int `json:"non_empty_stderr_logs"`
	NonEmptyLintRunLogs    int `json:"non_empty_lint_run_logs"`
	LargeLogCount          int `json:"large_log_count"`
	StorageBusyCount       int `json:"storage_busy_count"`
	StaleRepoMapCount      int `json:"stale_repo_map_count"`
	SandboxMissingCount    int `json:"sandbox_missing_count"`
	ToolchainFailureCount  int `json:"toolchain_failure_count"`
	DirectGitHookCount     int `json:"direct_git_hook_count"`
	ProtectedBranchCount   int `json:"protected_branch_count"`
	ProviderRequiredCount  int `json:"provider_required_count"`
	InlineEnvCount         int `json:"inline_env_count"`
	LineLimitCount         int `json:"line_limit_count"`
	UnparsedDiagnosticLogs int `json:"unparsed_diagnostic_logs"`
}

type DownstreamStorageStrategy struct {
	OpenError              string `json:"open_error,omitempty"`
	JournalMode            string `json:"journal_mode,omitempty"`
	AppendOnlyFallbackHint string `json:"append_only_fallback_hint,omitempty"`
	BusyTimeoutMS          int    `json:"busy_timeout_ms,omitempty"`
	StorageBusyLogCount    int    `json:"storage_busy_log_count"`
	StoreAvailable         bool   `json:"store_available"`
	ReadOnlyAnalysis       bool   `json:"read_only_analysis"`
	SingleConnectionPool   bool   `json:"single_connection_pool"`
}

type DownstreamStorageHealth struct {
	Backend                 string `json:"backend"`
	SourceOfTruth           string `json:"source_of_truth"`
	Path                    string `json:"path,omitempty"`
	ObsoleteStorePath       string `json:"obsolete_store_path,omitempty"`
	OpenError               string `json:"open_error,omitempty"`
	Recommendation          string `json:"recommendation"`
	EventCount              int    `json:"event_count"`
	ImportedEventCount      int    `json:"imported_event_count"`
	StoreAvailable          bool   `json:"store_available"`
	ReadOnlyAnalysis        bool   `json:"read_only_analysis"`
	ImportedObsoleteStore   bool   `json:"imported_obsolete_store"`
	LogOnlyStorageBusyCount int    `json:"log_only_storage_busy_count"`
	LogOnlyToolchainSignals int    `json:"log_only_toolchain_signals"`
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
		LogSignals:    logSignals,
		StorageHealth: downstreamStorageHealth(root, store, logSignals),
		StorageStrategy: DownstreamStorageStrategy{
			StoreAvailable:       store != nil,
			StorageBusyLogCount:  logSignals.StorageBusyCount,
			ReadOnlyAnalysis:     true,
			SingleConnectionPool: downstreamSingleConnectionPool(store),
		},
	}

	if logSignals.StorageBusyCount > 0 {
		analysis.StorageStrategy.AppendOnlyFallbackHint = downstreamAppendOnlyHint
	}

	if store == nil {
		analysis.ToolchainHealth = downstreamToolchainHealth(logSignals)
		analysis.EvidenceGaps = downstreamEvidenceGaps(analysis)
		analysis.IssueSummary = downstreamIssueSummary(analysis)

		return analysis, nil
	}

	return populateDownstreamAnalysisFromStore(ctx, store, limit, analysis)
}

func AnalyzeDownstreamDuckDB(
	ctx context.Context,
	root string,
	store *DuckDBStore,
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
		StorageHealth: DownstreamStorageHealth{
			Backend:                 "duckdb",
			SourceOfTruth:           "event_log",
			Path:                    downstreamDuckDBStorePath(root, store),
			ObsoleteStorePath:       downstreamObsoleteStorePath(root),
			EventCount:              downstreamEventCount(root),
			ImportedEventCount:      downstreamImportedEventCount(ctx, store),
			StoreAvailable:          store != nil,
			ReadOnlyAnalysis:        true,
			ImportedObsoleteStore:   false,
			LogOnlyStorageBusyCount: logSignals.StorageBusyCount,
			LogOnlyToolchainSignals: logSignals.ToolchainFailureCount,
		},
		StorageStrategy: DownstreamStorageStrategy{
			StoreAvailable:      false,
			StorageBusyLogCount: logSignals.StorageBusyCount,
			ReadOnlyAnalysis:    true,
		},
	}
	analysis.StorageHealth.Recommendation = downstreamDuckDBStorageRecommendation(
		analysis.StorageHealth,
		logSignals,
	)

	if store == nil {
		analysis.ToolchainHealth = downstreamToolchainHealth(logSignals)
		analysis.EvidenceGaps = downstreamEvidenceGaps(analysis)
		analysis.IssueSummary = downstreamIssueSummary(analysis)

		return analysis, nil
	}

	stats, err := store.Stats(ctx)
	if err != nil {
		return DownstreamAnalysis{}, fmt.Errorf("read downstream DuckDB stats: %w", err)
	}

	analysis.Stats = stats

	analysis, err = populateDownstreamAnalysisFromDatabase(
		ctx,
		store.database,
		limit,
		analysis,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.ToolchainHealth = downstreamToolchainHealth(logSignals)
	analysis.EvidenceGaps = downstreamEvidenceGaps(analysis)
	analysis.IssueSummary = downstreamIssueSummary(analysis)

	return analysis, nil
}

func downstreamDuckDBStorePath(root string, store *DuckDBStore) string {
	if store == nil || strings.TrimSpace(store.path) == "" {
		return DefaultDuckDBPath(ResolveStateRoot(root))
	}

	return store.path
}

func downstreamSingleConnectionPool(store *Store) bool {
	if store == nil {
		return false
	}

	return store.database.Stats().MaxOpenConnections == 1
}

func downstreamStorageHealth(
	root string,
	store *Store,
	logSignals DownstreamLogSignals,
) DownstreamStorageHealth {
	return DownstreamStorageHealth{
		Backend:                 "obsolete_store",
		SourceOfTruth:           "obsolete_store",
		Path:                    downstreamObsoleteStorePath(root),
		ObsoleteStorePath:       downstreamObsoleteStorePath(root),
		Recommendation:          downstreamStorageRecommendation(store != nil, logSignals),
		EventCount:              downstreamEventCount(root),
		ImportedEventCount:      0,
		StoreAvailable:          store != nil,
		ReadOnlyAnalysis:        true,
		ImportedObsoleteStore:   false,
		LogOnlyStorageBusyCount: logSignals.StorageBusyCount,
		LogOnlyToolchainSignals: logSignals.ToolchainFailureCount,
	}
}

func downstreamObsoleteStorePath(root string) string {
	paths := ObsoleteCodeIntelArtifactPaths(root)
	if len(paths) == 0 {
		return ""
	}

	return paths[0]
}

func downstreamStorageRecommendation(
	storeAvailable bool,
	logSignals DownstreamLogSignals,
) string {
	if !storeAvailable {
		return downstreamRebuildIndex
	}

	if logSignals.StorageBusyCount > 0 ||
		logSignals.ToolchainFailureCount > 0 ||
		logSignals.UnparsedDiagnosticLogs > 0 {
		return downstreamRebuildIndex
	}

	return downstreamHealthy
}

func downstreamDuckDBStorageRecommendation(
	health DownstreamStorageHealth,
	logSignals DownstreamLogSignals,
) string {
	if !health.StoreAvailable {
		return downstreamRebuildIndex
	}

	if health.EventCount > health.ImportedEventCount {
		return "duckdb_index_stale"
	}

	if health.EventCount == 0 && logSignals.EventJSONCount > 0 {
		return "event_log_missing"
	}

	if logSignals.StorageBusyCount > 0 ||
		logSignals.ToolchainFailureCount > 0 ||
		logSignals.UnparsedDiagnosticLogs > 0 {
		return "log_only_evidence_present"
	}

	return downstreamHealthy
}

func downstreamAffectedCommands(
	blockers []DownstreamPolicyBlocker,
	limit int,
) []DownstreamAffectedCommand {
	counts := map[string]DownstreamAffectedCommand{}

	for _, blocker := range blockers {
		for _, command := range blocker.AffectedCommands {
			key := strings.Join([]string{
				command.Tool,
				command.OperationKind,
				command.TargetKind,
				command.RiskCategory,
				command.Status,
				command.CommandShapeSHA256,
			}, "\x00")

			current := counts[key]
			if current.Count == 0 {
				current = command
			}

			current.Count += command.Count
			counts[key] = current
		}
	}

	results := make([]DownstreamAffectedCommand, 0, len(counts))
	for _, command := range counts {
		results = append(results, command)
	}

	sort.SliceStable(results, func(left, right int) bool {
		return results[left].Count > results[right].Count
	})

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	return results
}

func downstreamToolchainHealth(
	signals DownstreamLogSignals,
) []DownstreamToolchainHealth {
	results := []DownstreamToolchainHealth{}
	if signals.SandboxMissingCount > 0 {
		results = append(results, DownstreamToolchainHealth{
			RootCause: "missing_sandbox_binary",
			Message:   "Managed tool sandbox executable was missing or unavailable.",
			Count:     signals.SandboxMissingCount,
		})
	}

	if signals.ToolchainFailureCount > 0 {
		results = append(results, DownstreamToolchainHealth{
			RootCause: "managed_toolchain_failure",
			Message:   "Managed lint/toolchain logs contain sandbox, cgroup, or tool failures.",
			Count:     signals.ToolchainFailureCount,
		})
	}

	if signals.UnparsedDiagnosticLogs > 0 {
		results = append(results, DownstreamToolchainHealth{
			RootCause: "unparsed_diagnostics",
			Message: "Tool output contained diagnostics that were not " +
				"normalized into the query index.",
			Count: signals.UnparsedDiagnosticLogs,
		})
	}

	return results
}

func downstreamEvidenceGaps(analysis DownstreamAnalysis) []DownstreamEvidenceGap {
	gaps := []DownstreamEvidenceGap{}
	if analysis.LogSignals.StorageBusyCount > 0 {
		gaps = append(gaps, DownstreamEvidenceGap{
			Signal:         "storage_busy",
			Source:         "hook_or_lint_logs",
			QueryIndex:     analysis.StorageHealth.Backend,
			Count:          analysis.LogSignals.StorageBusyCount,
			Recommendation: "rebuild DuckDB index from append-only events and legacy logs",
		})
	}

	if analysis.LogSignals.ToolchainFailureCount > 0 &&
		len(analysis.ToolchainFailures) == 0 {
		gaps = append(gaps, DownstreamEvidenceGap{
			Signal:         "toolchain_failure",
			Source:         "lint_logs",
			QueryIndex:     analysis.StorageHealth.Backend,
			Count:          analysis.LogSignals.ToolchainFailureCount,
			Recommendation: "ingest lint-run logs into structured event records",
		})
	}

	if analysis.LogSignals.EventJSONCount > 0 && analysis.StorageHealth.EventCount == 0 {
		gaps = append(gaps, DownstreamEvidenceGap{
			Signal:         "legacy_hook_events",
			Source:         "hook-runs/event.json",
			QueryIndex:     analysis.StorageHealth.Backend,
			Count:          analysis.LogSignals.EventJSONCount,
			Recommendation: "run code-intel rebuild-index to materialize durable events",
		})
	}

	return gaps
}

func downstreamIssueSummary(analysis DownstreamAnalysis) DownstreamIssueSummary {
	summary := DownstreamIssueSummary{
		Title: "Downstream coding-ethos diagnostics summary",
		StorageDecision: fmt.Sprintf(
			"%s backed by %s",
			analysis.StorageHealth.Backend,
			analysis.StorageHealth.SourceOfTruth,
		),
	}

	if len(analysis.PolicyBlockers) > 0 {
		summary.TopFindings = append(
			summary.TopFindings,
			fmt.Sprintf(
				"top blocker %s occurred %d times",
				analysis.PolicyBlockers[0].PolicyID,
				analysis.PolicyBlockers[0].Count,
			),
		)
	}

	if len(analysis.FilePressure) > 0 {
		summary.TopFindings = append(
			summary.TopFindings,
			fmt.Sprintf(
				"largest file pressure is %s at %d lines",
				analysis.FilePressure[0].Path,
				analysis.FilePressure[0].LineCount,
			),
		)
	}

	if len(analysis.ToolchainHealth) > 0 {
		summary.TopFindings = append(
			summary.TopFindings,
			fmt.Sprintf(
				"toolchain health root cause %s appeared %d times",
				analysis.ToolchainHealth[0].RootCause,
				analysis.ToolchainHealth[0].Count,
			),
		)
	}

	if analysis.StorageHealth.Recommendation != downstreamHealthy {
		summary.NextActions = append(
			summary.NextActions,
			"coding-ethos-run code-intel rebuild-index --root <repo>",
		)
	}

	if len(analysis.RemediationLoops) > 0 {
		summary.NextActions = append(
			summary.NextActions,
			"review remediation_loops and fix the highest trace_count policy first",
		)
	}

	if len(analysis.AffectedCommands) > 0 {
		summary.NextActions = append(
			summary.NextActions,
			"review affected_commands and route repeated blocked command "+
				"shapes through managed workflows",
		)
	}

	return summary
}

func downstreamEventCount(root string) int {
	count, err := EventLogStats(root)
	if err != nil {
		return 0
	}

	return count
}

func downstreamImportedEventCount(ctx context.Context, store *DuckDBStore) int {
	if store == nil {
		return 0
	}

	var count int

	err := store.database.QueryRowContext(
		ctx,
		"SELECT COUNT(*) FROM code_intel_events",
	).Scan(&count)
	if err != nil {
		return 0
	}

	return count
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

	analysis, err = populateDownstreamAnalysisFromDatabase(
		ctx,
		store.database,
		limit,
		analysis,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.ToolchainHealth = downstreamToolchainHealth(analysis.LogSignals)
	analysis.EvidenceGaps = downstreamEvidenceGaps(analysis)
	analysis.IssueSummary = downstreamIssueSummary(analysis)

	return analysis, nil
}

func populateDownstreamAnalysisFromDatabase(
	ctx context.Context,
	database *sql.DB,
	limit int,
	analysis DownstreamAnalysis,
) (DownstreamAnalysis, error) {
	var err error

	analysis.HookFriction, err = downstreamHookFriction(ctx, database, limit)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.PolicyBlockers, err = downstreamPolicyBlockers(
		ctx,
		database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.AffectedCommands = downstreamAffectedCommands(analysis.PolicyBlockers, limit)

	analysis.RemediationLoops, err = downstreamRemediationLoops(
		ctx,
		database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.FindingHotspots, err = downstreamFindingHotspots(
		ctx,
		database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.FilePressure, err = downstreamFilePressure(ctx, database, limit)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	analysis.ToolchainFailures, err = downstreamToolchainFailures(
		ctx,
		database,
		limit,
	)
	if err != nil {
		return DownstreamAnalysis{}, err
	}

	return analysis, nil
}

func downstreamHookFriction(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamHookFriction, error) {
	rows, err := database.QueryContext(
		ctx,
		`SELECT
			COALESCE(operation_kind, '') AS operation,
			COALESCE(target_kind, '') AS target,
			COALESCE(risk_category, '') AS risk,
			COALESCE(status, '') AS event_status,
			blocked,
			COUNT(*) AS count
		FROM hook_events
		GROUP BY operation, target, risk, event_status, blocked
		ORDER BY blocked DESC, count DESC
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
	rows, err := queryDownstreamPolicyBlockerRows(ctx, database, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	accumulator := downstreamPolicyBlockerAccumulator{
		results:    []DownstreamPolicyBlocker{},
		indexByKey: map[downstreamPolicyBlockerKey]int{},
		limit:      limit,
	}

	err = scanDownstreamPolicyBlockerRows(rows, &accumulator)
	if err != nil {
		return nil, err
	}

	return accumulator.results, nil
}

func queryDownstreamPolicyBlockerRows(
	ctx context.Context,
	database *sql.DB,
	limit int,
) (*sql.Rows, error) {
	rows, err := database.QueryContext(
		ctx,
		downstreamPolicyBlockersSQL,
		limit*downstreamCommandFanout,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream policy blockers: %w", err)
	}

	return rows, nil
}

const downstreamPolicyBlockersSQL = `SELECT
	COALESCE(hd.policy_id, '') AS policy,
	COALESCE(hd.decision, '') AS decision_result,
	COALESCE(hd.severity, '') AS severity_result,
	hd.diagnostic_count,
	COALESCE(he.tool, '') AS command_tool,
	COALESCE(he.operation_kind, '') AS operation,
	COALESCE(he.target_kind, '') AS target,
	COALESCE(he.risk_category, '') AS risk,
	COALESCE(he.status, '') AS event_status,
	COALESCE(he.command_shape_sha256, '') AS command_shape,
	COUNT(*) AS count
FROM hook_decisions hd
LEFT JOIN hook_events he ON he.trace_id = hd.trace_id
WHERE hd.decision = 'block' OR hd.severity = 'block'
GROUP BY
	policy,
	decision_result,
	severity_result,
	hd.diagnostic_count,
	command_tool,
	operation,
	target,
	risk,
	event_status,
	command_shape
ORDER BY count DESC, diagnostic_count DESC
LIMIT ?`

type downstreamPolicyBlockerAccumulator struct {
	indexByKey map[downstreamPolicyBlockerKey]int
	results    []DownstreamPolicyBlocker
	limit      int
}

func scanDownstreamPolicyBlockerRows(
	rows *sql.Rows,
	accumulator *downstreamPolicyBlockerAccumulator,
) error {
	for rows.Next() {
		row, err := scanDownstreamPolicyBlockerRow(rows)
		if err != nil {
			return err
		}

		accumulator.add(row)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return fmt.Errorf("iterate downstream policy blockers: %w", rowsErr)
	}

	return nil
}

type downstreamPolicyBlockerRow struct {
	key     downstreamPolicyBlockerKey
	command DownstreamAffectedCommand
	count   int
}

func scanDownstreamPolicyBlockerRow(
	rows *sql.Rows,
) (downstreamPolicyBlockerRow, error) {
	var row downstreamPolicyBlockerRow

	scanErr := rows.Scan(
		&row.key.policyID,
		&row.key.decision,
		&row.key.severity,
		&row.key.diagnosticCount,
		&row.command.Tool,
		&row.command.OperationKind,
		&row.command.TargetKind,
		&row.command.RiskCategory,
		&row.command.Status,
		&row.command.CommandShapeSHA256,
		&row.count,
	)
	if scanErr != nil {
		return downstreamPolicyBlockerRow{}, fmt.Errorf(
			"scan downstream policy blocker: %w",
			scanErr,
		)
	}

	row.command.Count = row.count

	return row, nil
}

func (accumulator *downstreamPolicyBlockerAccumulator) add(
	row downstreamPolicyBlockerRow,
) {
	if index, found := accumulator.indexByKey[row.key]; found {
		accumulator.results[index].Count += row.count
		accumulator.results[index].AffectedCommands = append(
			accumulator.results[index].AffectedCommands,
			row.command,
		)

		return
	}

	if len(accumulator.results) >= accumulator.limit {
		return
	}

	accumulator.indexByKey[row.key] = len(accumulator.results)
	accumulator.results = append(accumulator.results, DownstreamPolicyBlocker{
		PolicyID:         row.key.policyID,
		Decision:         row.key.decision,
		Severity:         row.key.severity,
		DiagnosticCount:  row.key.diagnosticCount,
		Count:            row.count,
		AffectedCommands: []DownstreamAffectedCommand{row.command},
	})
}

type downstreamPolicyBlockerKey struct {
	policyID        string
	decision        string
	severity        string
	diagnosticCount int
}

func downstreamRemediationLoops(
	ctx context.Context,
	database *sql.DB,
	limit int,
) ([]DownstreamRemediationLoop, error) {
	rows, err := database.QueryContext(
		ctx,
		`WITH outcome_counts AS (
			SELECT
				COALESCE(remediation_id, '') AS remediation_id,
				COALESCE(policy_id, '') AS policy_id,
				COALESCE(skill_id, '') AS skill_id,
				COALESCE(file, '') AS file,
				COALESCE(path, '') AS path,
				SUM(CASE WHEN outcome = 'attempted' THEN 1 ELSE 0 END) AS attempted,
				SUM(CASE WHEN outcome = 'repeated' THEN 1 ELSE 0 END) AS repeated,
				MAX(COALESCE(recorded_at_utc, '')) AS last_seen_utc
			FROM remediation_outcomes
			GROUP BY remediation_id, policy_id, skill_id, file, path
		)
		SELECT
			COALESCE(ro.remediation_id, ''),
			COALESCE(NULLIF(oc.policy_id, ''), ro.policy_id, ''),
			COALESCE(NULLIF(oc.skill_id, ''), ro.skill_id, ''),
			COALESCE(NULLIF(oc.file, ''), ro.file, ''),
			COALESCE(NULLIF(oc.path, ''), ro.path, ''),
			COUNT(DISTINCT ro.trace_id) AS trace_count,
			COUNT(*) AS occurrence_count,
			COALESCE(oc.attempted, 0) AS attempted_count,
			COALESCE(oc.repeated, 0) AS repeated_count,
			MAX(COALESCE(NULLIF(oc.last_seen_utc, ''), ro.recorded_at_utc, '')) AS last_seen_utc
		FROM remediation_occurrences ro
		LEFT JOIN outcome_counts oc ON oc.remediation_id = ro.remediation_id
		GROUP BY
			ro.remediation_id,
			COALESCE(NULLIF(oc.policy_id, ''), ro.policy_id, ''),
			COALESCE(NULLIF(oc.skill_id, ''), ro.skill_id, ''),
			COALESCE(NULLIF(oc.file, ''), ro.file, ''),
			COALESCE(NULLIF(oc.path, ''), ro.path, ''),
			oc.attempted,
			oc.repeated
		HAVING occurrence_count > 1 OR attempted_count > 0 OR repeated_count > 0
		ORDER BY repeated_count DESC, occurrence_count DESC, attempted_count DESC
		LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query downstream remediation loops: %w", err)
	}
	defer rows.Close()

	results := []DownstreamRemediationLoop{}

	for rows.Next() {
		var result DownstreamRemediationLoop

		scanErr := rows.Scan(
			&result.RemediationID,
			&result.PolicyID,
			&result.SkillID,
			&result.File,
			&result.Path,
			&result.TraceCount,
			&result.OccurrenceCount,
			&result.AttemptedCount,
			&result.RepeatedCount,
			&result.LastSeenUTC,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scan downstream remediation loop: %w", scanErr)
		}

		results = append(results, result)
	}

	rowsErr := rows.Err()
	if rowsErr != nil {
		return nil, fmt.Errorf("iterate downstream remediation loops: %w", rowsErr)
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
			COALESCE(f.path, fo.path, '') AS display_path,
			COALESCE(f.tool, '') AS display_tool,
			COALESCE(f.code, '') AS display_code,
			COALESCE(f.policy_id, fo.policy_id, '') AS display_policy,
			COUNT(*) AS count
		FROM finding_occurrences fo
		JOIN findings f ON f.finding_id = fo.finding_id
		GROUP BY display_path, display_tool, display_code, display_policy
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
			COALESCE(tool, '') AS tool_name,
			COALESCE(code, '') AS rule_code,
			COALESCE(policy_id, '') AS policy,
			COALESCE(message, '') AS finding_message,
			COUNT(*) AS count
		FROM findings
		WHERE policy_id LIKE 'runtime.%'
			OR policy_id LIKE 'tool.%'
			OR message LIKE '%sandbox%'
			OR message LIKE '%managed tool%'
		GROUP BY tool_name, rule_code, policy, finding_message
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

	err := scanDownstreamLogTree(
		filepath.Join(root, downstreamStateDir, downstreamHookRunsDir),
		downstreamLogTreeHook,
		&signals,
	)
	if err != nil {
		return DownstreamLogSignals{}, err
	}

	err = scanDownstreamLogTree(
		filepath.Join(root, downstreamStateDir, downstreamLintRunsDir),
		downstreamLogTreeLint,
		&signals,
	)
	if err != nil {
		return DownstreamLogSignals{}, err
	}

	return signals, nil
}

type downstreamLogTreeKind string

const (
	downstreamLogTreeHook downstreamLogTreeKind = "hook"
	downstreamLogTreeLint downstreamLogTreeKind = "lint"
)

func scanDownstreamLogTree(
	runRoot string,
	kind downstreamLogTreeKind,
	signals *DownstreamLogSignals,
) error {
	candidates := []downstreamLogCandidate{}

	err := filepath.WalkDir(
		runRoot,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			candidate, shouldScan, updateErr := updateDownstreamLogSignals(
				runRoot,
				kind,
				path,
				entry,
				signals,
			)
			if updateErr != nil {
				return updateErr
			}

			if shouldScan {
				candidates = append(candidates, candidate)
			}

			return nil
		},
	)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("scan %s run logs: %w", kind, err)
	}

	return scanDownstreamLogCandidates(candidates, signals)
}

type downstreamLogCandidate struct {
	path    string
	modTime int64
}

func updateDownstreamLogSignals(
	runRoot string,
	kind downstreamLogTreeKind,
	path string,
	entry fs.DirEntry,
	signals *DownstreamLogSignals,
) (downstreamLogCandidate, bool, error) {
	if entry.IsDir() {
		recordDownstreamRunDirectory(runRoot, kind, path, signals)

		return downstreamLogCandidate{}, false, nil
	}

	name := entry.Name()
	recordDownstreamRunFile(kind, name, signals)

	if !isDownstreamScannableLog(kind, name) {
		return downstreamLogCandidate{}, false, nil
	}

	info, err := entry.Info()
	if err != nil {
		return downstreamLogCandidate{}, false, fmt.Errorf(
			"stat downstream log %q: %w",
			path,
			err,
		)
	}

	if info.Size() == 0 {
		return downstreamLogCandidate{}, false, nil
	}

	recordDownstreamNonEmptyLog(kind, name, signals)

	if info.Size() > downstreamLargeLogBytes {
		signals.LargeLogCount++

		return downstreamLogCandidate{}, false, nil
	}

	return downstreamLogCandidate{
		path:    path,
		modTime: info.ModTime().UnixNano(),
	}, true, nil
}

func scanDownstreamLogCandidates(
	candidates []downstreamLogCandidate,
	signals *DownstreamLogSignals,
) error {
	sort.SliceStable(candidates, func(left, right int) bool {
		return candidates[left].modTime > candidates[right].modTime
	})

	if len(candidates) > downstreamLogScanLimit {
		candidates = candidates[:downstreamLogScanLimit]
	}

	for _, candidate := range candidates {
		err := scanDownstreamLogFile(candidate.path, signals)
		if err != nil {
			return err
		}
	}

	return nil
}

func recordDownstreamRunDirectory(
	runRoot string,
	kind downstreamLogTreeKind,
	path string,
	signals *DownstreamLogSignals,
) {
	if kind == downstreamLogTreeHook && path != runRoot && filepath.Dir(path) == runRoot {
		signals.HookRunCount++
	}
}

func recordDownstreamRunFile(
	kind downstreamLogTreeKind,
	name string,
	signals *DownstreamLogSignals,
) {
	if name == downstreamEventJSONFile {
		signals.EventJSONCount++
	}

	if kind == downstreamLogTreeLint {
		signals.LintRunCount++
	}
}

func recordDownstreamNonEmptyLog(
	kind downstreamLogTreeKind,
	name string,
	signals *DownstreamLogSignals,
) {
	switch name {
	case "stdout.log":
		signals.NonEmptyStdoutLogs++
	case "stderr.log":
		signals.NonEmptyStderrLogs++
	}

	if kind == downstreamLogTreeLint {
		signals.NonEmptyLintRunLogs++
	}
}

func isDownstreamScannableLog(kind downstreamLogTreeKind, name string) bool {
	if name == "stdout.log" || name == "stderr.log" {
		return true
	}

	return kind == downstreamLogTreeLint && strings.HasSuffix(name, ".json")
}

func scanDownstreamLogFile(path string, signals *DownstreamLogSignals) error {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("open downstream log %q: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, downstreamScannerBuffer), downstreamLargeLogBytes)

	for scanner.Scan() {
		recordDownstreamLogLine(scanner.Text(), signals)
	}

	err = scanner.Err()
	if err != nil {
		return fmt.Errorf("read downstream log %q: %w", path, err)
	}

	return nil
}

func recordDownstreamLogLine(line string, signals *DownstreamLogSignals) {
	lower := strings.ToLower(line)

	recordStorageBusySignal(lower, signals)
	recordStaleRepoMapSignal(lower, signals)
	recordSandboxMissingSignal(lower, signals)
	recordToolchainFailureSignal(lower, signals)
	recordDirectGitSignal(lower, signals)
	recordProtectedBranchSignal(lower, signals)
	recordProviderRequiredSignal(lower, signals)
	recordInlineEnvSignal(lower, signals)
	recordLineLimitSignal(lower, signals)
	recordUnparsedDiagnosticSignal(lower, signals)
}

func recordStorageBusySignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "storage_busy") ||
		strings.Contains(lower, "database is locked") {
		signals.StorageBusyCount++
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

func recordToolchainFailureSignal(lower string, signals *DownstreamLogSignals) {
	if strings.Contains(lower, "managed-toolchain") ||
		strings.Contains(lower, "managed tool") ||
		strings.Contains(lower, "pyupgrade") ||
		strings.Contains(lower, "python-complexity") ||
		strings.Contains(lower, "python-vulture") ||
		strings.Contains(lower, "vulture") ||
		strings.Contains(lower, "toolchain") ||
		strings.Contains(lower, "sandbox backend unavailable") ||
		strings.Contains(lower, "cgroup memory limit could not be applied") {
		signals.ToolchainFailureCount++
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
